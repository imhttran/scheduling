package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
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
		Email    string `json:"email"` // email or UID
		Password string `json:"password"`
		DeviceID string `json:"deviceId"`
	}
	decodeJSON(r, &body)
	identifier := strings.TrimSpace(body.Email)

	// Resolve the account (email or UID) so the lockout is keyed on the account
	// rather than the identifier string — otherwise alternating email/UID would
	// reset the attempt counter and bypass the lockout.
	var id int
	var email, storedHash string
	var verified bool
	err := db.QueryRow(r.Context(),
		`SELECT id, email, password, email_verified FROM users WHERE email = $1 OR uid = $1`, identifier).
		Scan(&id, &email, &storedHash, &verified)
	notFound := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !notFound {
		respond500(w, "Login Error", err, false)
		return
	}
	// Lockout key: the account id when it exists, else the raw identifier (so
	// unknown identifiers are still rate-limited). The "acct:" prefix keeps
	// account keys from colliding with identifier keys.
	key := identifier
	if !notFound {
		key = "acct:" + strconv.Itoa(id)
	}

	// Rate limit: reject if this key is currently locked out.
	var lockedUntil *time.Time
	_ = db.QueryRow(r.Context(),
		`SELECT locked_until FROM login_attempts WHERE identifier = $1`, key).Scan(&lockedUntil)
	if lockedUntil != nil && time.Now().Before(*lockedUntil) {
		respond(w, http.StatusTooManyRequests, fail(http.StatusTooManyRequests,
			"Too many failed attempts. Try again later."))
		return
	}

	if notFound || !verifyPassword(body.Password, storedHash) {
		// Record the failure; lock the key after the max attempts.
		_, _ = db.Exec(r.Context(), `
			INSERT INTO login_attempts (identifier, attempts, locked_until)
			VALUES ($1, 1, NULL)
			ON CONFLICT (identifier) DO UPDATE SET
				attempts = login_attempts.attempts + 1,
				locked_until = CASE
					WHEN login_attempts.attempts + 1 >= $2 THEN now() + make_interval(mins => $3)
					ELSE login_attempts.locked_until END,
				updated_at = now()`,
			key, cfg.LoginMaxAttempts, cfg.LoginLockMinutes)
		respond(w, http.StatusUnauthorized, fail(http.StatusUnauthorized, "Invalid email or password"))
		return
	}
	// Successful password — clear any prior failures.
	_, _ = db.Exec(r.Context(),
		`DELETE FROM login_attempts WHERE identifier = $1`, key)
	if verificationRequired() && !verified {
		respond(w, http.StatusForbidden, fail(http.StatusForbidden, "Please verify your email before logging in."))
		return
	}
	// Trusted device? Skip 2FA.
	if body.DeviceID != "" {
		var known bool
		if err := db.QueryRow(r.Context(),
			`SELECT EXISTS (SELECT 1 FROM user_devices WHERE user_id = $1 AND device_id = $2)`, id, body.DeviceID).Scan(&known); err == nil && known {
			respond(w, http.StatusOK, map[string]any{
				"success": true, "message": "Login successful!",
				"token": issueToken(email), "user": map[string]any{"email": email},
			})
			return
		}
	}
	// New device — require 2FA.
	token := randomToken()
	code := randomCode()
	if _, err := db.Exec(r.Context(), `
		INSERT INTO login_codes (user_id, token, code, expires_at)
		VALUES ($1, $2, $3, now() + interval '10 minutes')`, id, token, code); err != nil {
		respond500(w, "Login Error", err, false)
		return
	}
	sendLoginCode(r.Context(), id, email, code)
	respond(w, http.StatusOK, map[string]any{
		"success": true, "twoFactorRequired": true, "token": token,
		"message": "Enter the code sent to your device",
	})
}

// Completes a 2FA login: validates the code, issues the real JWT, and
// registers the device so future logins from it skip 2FA.
func verifyLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token    string `json:"token"`
		Code     string `json:"code"`
		DeviceID string `json:"deviceId"`
	}
	decodeJSON(r, &body)
	var userID int
	var code, email string
	var expiresAt time.Time
	var used bool
	var attempts int
	err := db.QueryRow(r.Context(), `
		SELECT lc.user_id, lc.code, lc.expires_at, lc.used, lc.attempts, u.email
		FROM login_codes lc JOIN users u ON u.id = lc.user_id
		WHERE lc.token = $1`, body.Token).
		Scan(&userID, &code, &expiresAt, &used, &attempts, &email)
	if errors.Is(err, pgx.ErrNoRows) {
		respond(w, http.StatusBadRequest, msg("Invalid or expired code"))
		return
	}
	if err != nil {
		respond500(w, "Verify Login Error", err, false)
		return
	}
	// Lock the code after a handful of failed tries so a 4-digit code can't be
	// brute-forced within its 10-minute window.
	if used || time.Now().After(expiresAt) || attempts >= 5 {
		respond(w, http.StatusBadRequest, msg("Invalid or expired code"))
		return
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(body.Code)) != 1 {
		_, _ = db.Exec(r.Context(),
			`UPDATE login_codes SET attempts = attempts + 1 WHERE token = $1`, body.Token)
		respond(w, http.StatusBadRequest, msg("Invalid or expired code"))
		return
	}
	if _, err := db.Exec(r.Context(),
		`UPDATE login_codes SET used = true WHERE token = $1`, body.Token); err != nil {
		respond500(w, "Verify Login Error", err, false)
		return
	}
	if body.DeviceID != "" {
		_, _ = db.Exec(r.Context(),
			`INSERT INTO user_devices (user_id, device_id) VALUES ($1, $2) ON CONFLICT (device_id) DO NOTHING`,
			userID, body.DeviceID)
	}
	respond(w, http.StatusOK, map[string]any{
		"success": true, "message": "Login successful!",
		"token": issueToken(email), "user": map[string]any{"email": email},
	})
}

// Resends the 2FA code for a pending login, up to 3 times.
func resendLoginCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	decodeJSON(r, &body)
	var userID, resends int
	var email string
	var expiresAt time.Time
	var used bool
	err := db.QueryRow(r.Context(), `
		SELECT lc.user_id, lc.resends, lc.expires_at, lc.used, u.email
		FROM login_codes lc JOIN users u ON u.id = lc.user_id
		WHERE lc.token = $1`, body.Token).
		Scan(&userID, &resends, &expiresAt, &used, &email)
	if errors.Is(err, pgx.ErrNoRows) {
		respond(w, http.StatusBadRequest, msg("Invalid or expired code"))
		return
	}
	if err != nil {
		respond500(w, "Resend Code Error", err, false)
		return
	}
	if used || time.Now().After(expiresAt) {
		respond(w, http.StatusBadRequest, msg("Invalid or expired code"))
		return
	}
	if resends >= 3 {
		respond(w, http.StatusTooManyRequests, msg("Too many resend attempts"))
		return
	}
	code := randomCode()
	if _, err := db.Exec(r.Context(), `
		UPDATE login_codes SET code = $1, resends = resends + 1, expires_at = now() + interval '10 minutes'
		WHERE token = $2`, code, body.Token); err != nil {
		respond500(w, "Resend Code Error", err, false)
		return
	}
	sendLoginCode(r.Context(), userID, email, code)
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Code resent"})
}

// A 4-digit login code. In development it's always 1234 so testing doesn't
// need a mail server; otherwise a random code.
func randomCode() string {
	if cfg.Env == "development" {
		return "1234"
	}
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	n := int(b[0])<<8 | int(b[1])
	return fmt.Sprintf("%04d", n%10000)
}

// Sends the 2FA code via the user's preferred channel (text/phone → SMS,
// else email). SMS is logged — no provider is wired up.
func sendLoginCode(ctx context.Context, userID int, email, code string) {
	var pref string
	_ = db.QueryRow(ctx,
		`SELECT communication_preference FROM user_profiles WHERE user_id = $1`, userID).Scan(&pref)
	if pref == "text" || pref == "phone" {
		log.Printf("[2fa] SMS to %s: your code is %s", email, code)
		return
	}
	row := loginCodeEmail(email, code)
	_, _ = db.Exec(ctx,
		`INSERT INTO email_queue ("to", subject, body) VALUES ($1, $2, $3)`,
		row.To, row.Subject, row.Body)
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
