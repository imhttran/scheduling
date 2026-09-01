package main

import (
	"crypto/rand"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

func listUsers(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	// Staff sees everyone except admin accounts — admin accounts aren't theirs
	// to manage. Admin sees everyone.
	query := `
		SELECT u.id, u.email, u.uid, u.role, u.email_verified, u.disabled, u.created_at,
		       p.first_name, p.last_name, p.address, p.address2, p.city, p.state,
		       p.zip, p.country, p.phone, p.communication_preference
		FROM users u
		LEFT JOIN user_profiles p ON p.user_id = u.id`
	if !hasRole(u.Role, "admin") {
		query += ` WHERE u.role <> 'admin'`
	}
	query += ` ORDER BY u.created_at ASC`
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
		var verified, disabled bool
		var createdAt time.Time
		var firstName, lastName, address, city, state, zip, country, phone, commPref *string
		var address2 *string
		var uid *string
		if err := rows.Scan(&id, &email, &uid, &role, &verified, &disabled, &createdAt,
			&firstName, &lastName, &address, &address2, &city, &state,
			&zip, &country, &phone, &commPref); err != nil {
			respond500(w, "List Users Error", err, false)
			return
		}
		users = append(users, map[string]any{
			"id":                      id,
			"email":                   email,
			"role":                    role,
			"emailVerified":           verified,
			"disabled":                disabled,
			"createdAt":               createdAt,
			"uid":                     uid,
			"firstName":               firstName,
			"lastName":                lastName,
			"address":                 address,
			"address2":                address2,
			"city":                    city,
			"state":                   state,
			"zip":                     zip,
			"country":                 country,
			"phone":                   phone,
			"communicationPreference": commPref,
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
		UID      string `json:"uid"`
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
	if len(body.UID) > 20 {
		respond(w, http.StatusBadRequest, msg("uid must be 20 characters or fewer"))
		return
	}
	var id int
	var email, role string
	var verified bool
	err := db.QueryRow(r.Context(), `
		INSERT INTO users (email, uid, password, email_verified, must_change_password)
		VALUES ($1, NULLIF($2, ''), $3, true, true)
		RETURNING id, email, role, email_verified`,
		body.Email, body.UID, hashPassword(body.Password)).
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

// Manager-only: creates a worker (student or staff) in the manager's location.
// The worker is verified (the manager vouches) and forced to change their
// password on first login.
func createWorker(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email     string `json:"email"`
		Password  string `json:"password"`
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
		Role      string `json:"role"`
		JobID     int    `json:"jobId"`
		MinHours  int    `json:"minHours"`
		MaxHours  int    `json:"maxHours"`
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
	if body.Role != "student" && body.Role != "staff" {
		respond(w, http.StatusBadRequest, msg("role must be student or staff"))
		return
	}
	if body.FirstName == "" || body.LastName == "" {
		respond(w, http.StatusBadRequest, msg("firstName and lastName are required"))
		return
	}
	// The job must be in the caller's location (non-admin).
	if !hasRole(currentUser(r).Role, "admin") {
		locID, err := managerLocationID(r.Context(), currentUser(r).ID)
		if err != nil {
			respond500(w, "Create Worker Error", err, false)
			return
		}
		var ok bool
		if err := db.QueryRow(r.Context(),
			`SELECT EXISTS (SELECT 1 FROM jobs j JOIN departments d ON d.id = j.department_id WHERE j.id = $1 AND d.location_id = $2)`,
			body.JobID, locID).Scan(&ok); err != nil || !ok {
			respond(w, http.StatusForbidden, msg("Job not in your location"))
			return
		}
	}
	var id int
	var email, role string
	err := db.QueryRow(r.Context(), `
		INSERT INTO users (email, password, role, email_verified, must_change_password)
		VALUES ($1, $2, $3, true, true)
		RETURNING id, email, role`,
		body.Email, hashPassword(body.Password), body.Role).
		Scan(&id, &email, &role)
	if err != nil {
		if isUniqueViolation(err) {
			respond(w, http.StatusBadRequest, fail(http.StatusBadRequest, "Email is already registered"))
			return
		}
		respond500(w, "Create Worker Error", err, true)
		return
	}
	// Create the profile (placeholders the worker can update later).
	if _, err := db.Exec(r.Context(), `
		INSERT INTO user_profiles (user_id, first_name, last_name, address, state, zip, phone)
		VALUES ($1, $2, $3, 'N/A', 'N/A', '00000', 'N/A')`,
		id, body.FirstName, body.LastName); err != nil {
		respond500(w, "Create Worker Error", err, true)
		return
	}
	// Assign the job so the worker sees the workqueue.
	if _, err := db.Exec(r.Context(), `
		INSERT INTO student_jobs (user_id, job_id, min_hours, max_hours)
		VALUES ($1, $2, $3, $4)`,
		id, body.JobID, body.MinHours, body.MaxHours); err != nil {
		respond500(w, "Create Worker Error", err, true)
		return
	}
	respond(w, http.StatusCreated, map[string]any{
		"success": true, "message": "Worker created",
		"user": map[string]any{"id": id, "email": email, "role": role},
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
		UID  string `json:"uid"`
	}
	decodeJSON(r, &body)
	if !isValidRole(body.Role) {
		respond(w, http.StatusBadRequest, msg("role must be one of: student, staff, manager, scheduler, admin"))
		return
	}
	if len(body.UID) > 20 {
		respond(w, http.StatusBadRequest, msg("uid must be 20 characters or fewer"))
		return
	}
	if targetID(r) == currentUser(r).ID {
		respond(w, http.StatusBadRequest, msg("Cannot change your own role"))
		return
	}
	var id int
	var email, role string
	err := db.QueryRow(r.Context(), `
		UPDATE users SET role = $1, uid = NULLIF($3, '') WHERE id = $2
		RETURNING id, email, role`, body.Role, targetID(r), body.UID).
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

// A 15-character password that always satisfies validatePassword (uppercase,
// digit, special char). Uses crypto/rand.
func generatePassword() string {
	const upper = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	const digits = "23456789"
	const special = "!@#$%^&*"
	const all = upper + "abcdefghijkmnpqrstuvwxyz" + digits + special
	randInt := func(n int) int {
		var buf [1]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return 0
		}
		return int(buf[0]) % n
	}
	b := make([]byte, 15)
	// Guarantee one of each required class, then fill the rest.
	classes := []string{upper, digits, special}
	for i := 0; i < 3; i++ {
		b[i] = classes[i][randInt(len(classes[i]))]
	}
	for i := 3; i < 15; i++ {
		b[i] = all[randInt(len(all))]
	}
	// Shuffle so the guaranteed chars aren't always at the front.
	for i := 14; i > 0; i-- {
		j := randInt(i + 1)
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

// Admin-only: generates a new 15-char password, sets it (forcing a change on
// next login), and emails it. When EXPOSE_GENERATED_PASSWORD=true, the
// plaintext is returned so the admin can copy it for testing.
func generateUserPassword(w http.ResponseWriter, r *http.Request) {
	var email string
	err := db.QueryRow(r.Context(),
		`SELECT email FROM users WHERE id = $1`, targetID(r)).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		respond(w, http.StatusNotFound, msg("User not found"))
		return
	}
	if err != nil {
		respond500(w, "Generate Password Error", err, false)
		return
	}
	password := generatePassword()
	if _, err := db.Exec(r.Context(),
		`UPDATE users SET password = $1, must_change_password = true WHERE id = $2`,
		hashPassword(password), targetID(r)); err != nil {
		respond500(w, "Generate Password Error", err, true)
		return
	}
	row := generatedPasswordEmail(email, password)
	if _, err := db.Exec(r.Context(),
		`INSERT INTO email_queue ("to", subject, body) VALUES ($1, $2, $3)`,
		row.To, row.Subject, row.Body); err != nil {
		respond500(w, "Generate Password Error", err, true)
		return
	}
	resp := map[string]any{"success": true, "message": "New password generated and emailed"}
	respond(w, http.StatusOK, resp)
}
