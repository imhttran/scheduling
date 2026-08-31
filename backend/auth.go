package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/scrypt"
)

// Node scrypt defaults: N=16384, r=8, p=1, salt hex-encoded as a string.
const (
	scryptN = 16384
	scryptR = 8
	scryptP = 1
	// 16-byte salt, 64-byte key, stored as `salt:hash` hex.
	saltLen = 16
	keyLen  = 64
)

func hashPassword(password string) string {
	salt := make([]byte, saltLen)
	_, _ = rand.Read(salt)
	saltHex := hex.EncodeToString(salt)
	key, err := scrypt.Key([]byte(password), []byte(saltHex), scryptN, scryptR, scryptP, keyLen)
	if err != nil {
		panic(err) // not reachable with these cost params
	}
	return saltHex + ":" + hex.EncodeToString(key)
}

func verifyPassword(password, stored string) bool {
	saltHex, hashHex, found := strings.Cut(stored, ":")
	if !found {
		return false
	}
	key, err := scrypt.Key([]byte(password), []byte(saltHex), scryptN, scryptR, scryptP, keyLen)
	if err != nil {
		return false
	}
	hash, _ := hex.DecodeString(hashHex)
	return len(hash) == len(key) && subtle.ConstantTimeCompare(hash, key) == 1
}

// HS256, claim {email}, 1h expiry.
func issueToken(email string) string {
	now := time.Now()
	claims := jwt.MapClaims{
		"email": email,
		"exp":   now.Add(time.Hour).Unix(),
		"iat":   now.Unix(),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		panic(err) // signing with a string secret can't fail
	}
	return token
}

func verifyToken(tokenStr string) (string, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		return []byte(cfg.JWTSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !token.Valid {
		return "", err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	// Node: a signed token without a usable email claim still fails the
	// Prisma lookup and reads as "Invalid or expired token".
	email, ok2 := claims["email"].(string)
	if !ok || !ok2 || email == "" {
		return "", jwt.ErrTokenInvalidClaims
	}
	return email, nil
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- handlers ----

func me(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	respond(w, http.StatusOK, map[string]any{
		"message": "Welcome to the secret area!",
		"user": map[string]any{
			"id":                 u.ID,
			"email":              u.Email,
			"role":               u.Role,
			"emailVerified":      u.EmailVerified,
			"mustChangePassword": u.MustChangePassword,
			"hasProfile":         u.HasProfile,
		},
	})
}

func signup(w http.ResponseWriter, r *http.Request) {
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

	// Atomic: user + welcome-email row together, so a failed email never
	// leaves an orphaned account and a rolled-back signup leaves no queued
	// email. Actual send is deferred to the worker — signup is never blocked
	// on mail delivery.
	token := randomToken()
	ctx := r.Context()
	tx, err := db.Begin(ctx)
	if err != nil {
		respond500(w, "Signup Error", err, true)
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`INSERT INTO users (email, password, email_verified, verification_token) VALUES ($1, $2, false, $3)`,
		body.Email, hashPassword(body.Password), token); err != nil {
		if isUniqueViolation(err) {
			// Keep the user-facing message generic so the API doesn't reveal
			// whether an email is already registered (prevents user enumeration).
			// The real reason is logged server-side for debugging.
			log.Printf("[signup] rejected: email already registered (email=%s)", body.Email)
			respond(w, http.StatusBadRequest, fail(http.StatusBadRequest, "Unable to sign up. Please try again later."))
			return
		}
		respond500(w, "Signup Error", err, true)
		return
	}
	row := welcomeEmail(body.Email)
	if _, err := tx.Exec(ctx,
		`INSERT INTO email_queue ("to", subject, body) VALUES ($1, $2, $3)`,
		row.To, row.Subject, row.Body); err != nil {
		respond500(w, "Signup Error", err, true)
		return
	}
	vrow := verificationEmail(body.Email, tokenLink("verify", token))
	if _, err := tx.Exec(ctx,
		`INSERT INTO email_queue ("to", subject, body) VALUES ($1, $2, $3)`,
		vrow.To, vrow.Subject, vrow.Body); err != nil {
		respond500(w, "Signup Error", err, true)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		respond500(w, "Signup Error", err, true)
		return
	}
	respond(w, http.StatusCreated, map[string]any{
		"success": true,
		"message": "User created successfully!",
		"user":    map[string]any{"email": body.Email},
	})
}

func verify(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		respond(w, http.StatusBadRequest, fail(http.StatusBadRequest, "Missing verification token"))
		return
	}
	var id int
	err := db.QueryRow(r.Context(), `SELECT id FROM users WHERE verification_token = $1`, token).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		respond(w, http.StatusBadRequest, fail(http.StatusBadRequest, "Invalid or expired verification link"))
		return
	}
	if err != nil {
		respond500(w, "Verify Error", err, true)
		return
	}
	if _, err := db.Exec(r.Context(),
		`UPDATE users SET email_verified = true, verification_token = NULL WHERE id = $1`, id); err != nil {
		respond500(w, "Verify Error", err, true)
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Email verified successfully!"})
}

func resendVerification(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	decodeJSON(r, &body)
	if !validateEmail(body.Email) {
		respond(w, http.StatusBadRequest, fail(http.StatusBadRequest, "Invalid email address"))
		return
	}
	var id int
	var verified bool
	err := db.QueryRow(r.Context(),
		`SELECT id, email_verified FROM users WHERE email = $1`, body.Email).Scan(&id, &verified)
	// Same response regardless of account existence/verified state, so this
	// endpoint can't be used to enumerate registered emails.
	if err == nil && !verified {
		if err := queueVerificationEmail(r.Context(), id, body.Email); err != nil {
			respond500(w, "Resend Verification Error", err, true)
			return
		}
	}
	respond(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "If that email is registered and unverified, a verification link has been sent.",
	})
}

func forgotPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	decodeJSON(r, &body)
	if !validateEmail(body.Email) {
		respond(w, http.StatusBadRequest, fail(http.StatusBadRequest, "Invalid email address"))
		return
	}
	// No such user: fall through to the generic response (P2025 equivalent).
	err := queuePasswordReset(r.Context(), "email", body.Email)
	if err != nil && !errors.Is(err, errNotFound) {
		respond500(w, "Forgot Password Error", err, true)
		return
	}
	// Same response whether or not the account exists, so this endpoint
	// can't be used to enumerate registered emails.
	respond(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "If that email is registered, a reset link has been sent.",
	})
}

func resetPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	decodeJSON(r, &body)
	if body.Token == "" {
		respond(w, http.StatusBadRequest, fail(http.StatusBadRequest, "Missing reset token"))
		return
	}
	if passwordError := validatePassword(body.Password); passwordError != "" {
		respond(w, http.StatusBadRequest, fail(http.StatusBadRequest, passwordError))
		return
	}
	var id int
	var email string
	var expiry *time.Time
	err := db.QueryRow(r.Context(),
		`SELECT id, email, reset_token_expiry FROM users WHERE reset_token = $1`, body.Token).
		Scan(&id, &email, &expiry)
	if err != nil || expiry == nil || expiry.Before(time.Now()) {
		respond(w, http.StatusBadRequest, fail(http.StatusBadRequest, "Invalid or expired reset link"))
		return
	}
	if _, err := db.Exec(r.Context(), `
		UPDATE users
		SET password = $1, reset_token = NULL, reset_token_expiry = NULL, must_change_password = false
		WHERE id = $2`, hashPassword(body.Password), id); err != nil {
		respond500(w, "Reset Password Error", err, true)
		return
	}
	respond(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Password reset successfully!",
		"token":   issueToken(email),
		"user":    map[string]any{"email": email},
	})
}

func login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	decodeJSON(r, &body)
	var id int
	var storedHash string
	var verified bool
	err := db.QueryRow(r.Context(),
		`SELECT id, password, email_verified FROM users WHERE email = $1`, body.Email).
		Scan(&id, &storedHash, &verified)
	if err != nil || !verifyPassword(body.Password, storedHash) {
		respond(w, http.StatusUnauthorized, fail(http.StatusUnauthorized, "Invalid email or password"))
		return
	}
	if verificationRequired() && !verified {
		respond(w, http.StatusForbidden, fail(http.StatusForbidden, "Please verify your email before logging in."))
		return
	}
	respond(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Login successful!",
		"token":   issueToken(body.Email),
		"user":    map[string]any{"email": body.Email},
	})
}

// Authenticated self-service password change. Used both for the general
// "change my password" case and to clear the forced-change flag an
// admin-created account starts with.
func changePassword(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	decodeJSON(r, &body)
	if !verifyPassword(body.CurrentPassword, u.Password) {
		respond(w, http.StatusUnauthorized, fail(http.StatusUnauthorized, "Current password is incorrect"))
		return
	}
	if passwordError := validatePassword(body.NewPassword); passwordError != "" {
		respond(w, http.StatusBadRequest, fail(http.StatusBadRequest, passwordError))
		return
	}
	if _, err := db.Exec(r.Context(),
		`UPDATE users SET password = $1, must_change_password = false WHERE id = $2`,
		hashPassword(body.NewPassword), u.ID); err != nil {
		respond500(w, "Change Password Error", err, false)
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Password changed successfully!"})
}
