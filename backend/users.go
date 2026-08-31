package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

func listUsers(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	// Staff sees clients and other staff — admin accounts aren't theirs to
	// manage. Admin sees everyone.
	query := `SELECT id, email, role, email_verified, created_at FROM users`
	if !hasRole(u.Role, "admin") {
		query += ` WHERE role IN ('client', 'staff')`
	}
	query += ` ORDER BY created_at ASC`
	rows, err := db.Query(r.Context(), query)
	if err != nil {
		respond500(w, "List Users Error", err, false)
		return
	}
	defer rows.Close()
	users := []map[string]any{}
	for rows.Next() {
		var id int
		var email, role string
		var verified bool
		var createdAt time.Time
		if err := rows.Scan(&id, &email, &role, &verified, &createdAt); err != nil {
			respond500(w, "List Users Error", err, false)
			return
		}
		users = append(users, map[string]any{
			"id":            id,
			"email":         email,
			"role":          role,
			"emailVerified": verified,
			"createdAt":     createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		respond500(w, "List Users Error", err, false)
		return
	}
	respond(w, http.StatusOK, map[string]any{"users": users})
}

// Admin-only: creates a user with an admin-chosen password, already verified
// (the admin vouches for the email) and flagged to force a password change
// on first login — the admin never needs to share the real password twice.
func adminCreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	decodeJSON(r, &body)
	if !validateEmail(body.Email) {
		respond(w, http.StatusBadRequest, fail(http.StatusBadRequest, "Invalid email address"))
		return
	}
	if passwordError := validatePassword(body.Password); passwordError != "" {
		respond(w, http.StatusBadRequest, fail(http.StatusBadRequest, passwordError))
		return
	}
	var id int
	var email, role string
	var verified bool
	err := db.QueryRow(r.Context(), `
		INSERT INTO users (email, password, email_verified, must_change_password)
		VALUES ($1, $2, true, true)
		RETURNING id, email, role, email_verified`,
		body.Email, hashPassword(body.Password)).
		Scan(&id, &email, &role, &verified)
	if err != nil {
		if isUniqueViolation(err) {
			respond(w, http.StatusBadRequest, fail(http.StatusBadRequest, "Email is already registered"))
			return
		}
		respond500(w, "Admin Create User Error", err, true)
		return
	}
	respond(w, http.StatusCreated, map[string]any{
		"success": true,
		"message": "User created successfully!",
		"user": map[string]any{
			"id":            id,
			"email":         email,
			"role":          role,
			"emailVerified": verified,
		},
	})
}

// Staff can nudge a not-yet-verified user's verification email along.
func staffResendVerification(w http.ResponseWriter, r *http.Request) {
	var id int
	var email string
	var verified bool
	err := db.QueryRow(r.Context(),
		`SELECT id, email, email_verified FROM users WHERE id = $1`, targetID(r)).
		Scan(&id, &email, &verified)
	if errors.Is(err, pgx.ErrNoRows) {
		respond(w, http.StatusNotFound, msg("User not found"))
		return
	}
	if err != nil {
		respond500(w, "Resend Verification Error", err, false)
		return
	}
	if verified {
		respond(w, http.StatusBadRequest, msg("User is already verified"))
		return
	}
	if err := queueVerificationEmail(r.Context(), id, email); err != nil {
		respond500(w, "Resend Verification Error", err, false)
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Verification email sent"})
}

// Admin-only: flip a user's verified flag directly, no email round-trip.
func patchVerification(w http.ResponseWriter, r *http.Request) {
	// Decoded loosely so a present-but-non-boolean value (string, number)
	// reads as not-a-boolean, exactly like `typeof emailVerified !== "boolean"`.
	var body map[string]any
	decodeJSON(r, &body)
	verified, ok := body["emailVerified"].(bool)
	if !ok {
		respond(w, http.StatusBadRequest, msg("emailVerified must be a boolean"))
		return
	}
	var id int
	var email string
	err := db.QueryRow(r.Context(), `
		UPDATE users SET email_verified = $1, verification_token = NULL
		WHERE id = $2
		RETURNING id, email, email_verified`, verified, targetID(r)).
		Scan(&id, &email, &verified)
	if errors.Is(err, pgx.ErrNoRows) {
		respond(w, http.StatusNotFound, msg("User not found"))
		return
	}
	if err != nil {
		respond500(w, "Update Verification Error", err, false)
		return
	}
	responseMessage := "User marked as unverified"
	if verified {
		responseMessage = "User marked as verified"
	}
	respond(w, http.StatusOK, map[string]any{
		"success": true,
		"message": responseMessage,
		"user": map[string]any{
			"id":            id,
			"email":         email,
			"emailVerified": verified,
		},
	})
}

// Admin-only: changes a user's role. Blocks self-demotion so an admin can't
// lock themselves (and potentially every other admin) out of admin routes.
func patchRole(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Role string `json:"role"`
	}
	decodeJSON(r, &body)
	if !isValidRole(body.Role) {
		respond(w, http.StatusBadRequest, msg("role must be one of: client, staff, admin"))
		return
	}
	if targetID(r) == currentUser(r).ID {
		respond(w, http.StatusBadRequest, msg("Cannot change your own role"))
		return
	}
	var id int
	var email, role string
	err := db.QueryRow(r.Context(), `
		UPDATE users SET role = $1 WHERE id = $2
		RETURNING id, email, role`, body.Role, targetID(r)).
		Scan(&id, &email, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		respond(w, http.StatusNotFound, msg("User not found"))
		return
	}
	if err != nil {
		respond500(w, "Update Role Error", err, false)
		return
	}
	respond(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "User role updated",
		"user":    map[string]any{"id": id, "email": email, "role": role},
	})
}

// Admin-only: sends the same reset-password email a user would trigger themselves,
// so an admin never has to see or set anyone's plaintext password.
func adminResetPassword(w http.ResponseWriter, r *http.Request) {
	err := queuePasswordReset(r.Context(), "id", targetID(r))
	if errors.Is(err, errNotFound) {
		respond(w, http.StatusNotFound, msg("User not found"))
		return
	}
	if err != nil {
		respond500(w, "Admin Reset Password Error", err, false)
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Password reset email sent"})
}

func deleteUser(w http.ResponseWriter, r *http.Request) {
	if targetID(r) == currentUser(r).ID {
		respond(w, http.StatusBadRequest, msg("Cannot delete your own account"))
		return
	}
	tag, err := db.Exec(r.Context(), `DELETE FROM users WHERE id = $1`, targetID(r))
	if err != nil {
		respond500(w, "Delete User Error", err, false)
		return
	}
	if tag.RowsAffected() == 0 {
		respond(w, http.StatusNotFound, msg("User not found"))
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "User deleted"})
}

func isValidRole(role string) bool { return roleIndex(role) >= 0 }
