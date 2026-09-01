package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Runtime configuration, read from the environment (see .env.example).
type Config struct {
	Port        string
	DatabaseURL string
	JWTSecret   string
	Env         string // development | qa | production

	EmailVerificationRequired bool
	FrontendURL               string

	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string
	MailFrom string

	MaxAttempts int

	// Login rate limiting: max failed attempts before a temporary lockout.
	LoginMaxAttempts int
	// Lockout window in minutes after exceeding the attempt limit.
	LoginLockMinutes int
}

// Globals, mirroring the Node app's module-level prisma client and
// process.env reads. Tests overwrite them directly.
var (
	cfg Config
	db  *pgxpool.Pool
)

// Bypass email verification when EMAIL_VERIFICATION_REQUIRED=false (dev / local setups).
func verificationRequired() bool { return cfg.EmailVerificationRequired }

func loadConfig() {
	cfg = Config{
		Port:        envOr("PORT", "8080"),
		DatabaseURL: envOr("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/go_template?sslmode=disable"),
		Env:         envOr("NODE_ENV", "development"),
		FrontendURL: envOr("FRONTEND_URL", "http://localhost:3000"),
		SMTPHost:    os.Getenv("SMTP_HOST"),
		SMTPPort:    intOr(os.Getenv("SMTP_PORT"), 587),
		SMTPUser:    os.Getenv("SMTP_USER"),
		SMTPPass:    os.Getenv("SMTP_PASS"),
		MailFrom:    envOr("MAIL_FROM", "no-reply@example.edu"),
		MaxAttempts: intOr(os.Getenv("MAX_ATTEMPTS"), 3),
		// Bypass email verification when EMAIL_VERIFICATION_REQUIRED=false (dev / local setups).
		EmailVerificationRequired: os.Getenv("EMAIL_VERIFICATION_REQUIRED") != "false",
		// Failed logins before a temporary lockout (default 5).
		LoginMaxAttempts: intOr(os.Getenv("LOGIN_MAX_ATTEMPTS"), 5),
		// Lockout window in minutes (default 15).
		LoginLockMinutes: intOr(os.Getenv("LOGIN_LOCK_MINUTES"), 15),
	}
	if cfg.JWTSecret = os.Getenv("JWT_SECRET"); cfg.JWTSecret == "" {
		if cfg.Env == "production" {
			log.Fatal("JWT_SECRET must be set in production")
		}
		cfg.JWTSecret = "dev-insecure-jwt-secret"
		log.Println("[config] JWT_SECRET not set — using insecure dev fallback")
	}
}

// Prisma's P2002 (unique constraint) as a Postgres error code check.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// JS number coercion semantics: Number("" | garbage) yields 0/NaN → fallback.
func intOr(v string, fallback int) int {
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return n
	}
	return fallback
}

// loadEnvFiles ports loadEnv.js: a personal root .env always wins (existing
// env vars are never overwritten); the committed dev profile only fills in
// when NODE_ENV=development. Resolved from cwd, then the parent dir — running
// from backend/ hits the same repo-root files the Node app loaded.
func loadEnvFiles() {
	applyEnv := func(path string) {
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimPrefix(line, "export ")
			key, value, found := strings.Cut(line, "=")
			if !found {
				continue
			}
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' ||
				value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
			if _, set := os.LookupEnv(key); !set {
				os.Setenv(key, value)
			}
		}
	}
	for _, dir := range []string{".", ".."} {
		if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
			applyEnv(filepath.Join(dir, ".env"))
			break
		}
	}
	// Unset counts as development (the backend's default) — otherwise this flag
	// could never be seen, since .env.dev is itself what sets NODE_ENV.
	if os.Getenv("NODE_ENV") == "" || os.Getenv("NODE_ENV") == "development" {
		for _, dir := range []string{".", ".."} {
			if _, err := os.Stat(filepath.Join(dir, ".env.dev")); err == nil {
				applyEnv(filepath.Join(dir, ".env.dev"))
				break
			}
		}
	}
}

func newRouter() http.Handler {
	r := chi.NewRouter()

	// No CORS middleware: browsers only talk to the Next.js server, which
	// proxies /api/* to this API (see frontend/next.config.ts) — same-origin.
	// If you split domains, re-add github.com/go-chi/cors here.

	r.Route("/api", func(r chi.Router) {
		r.Post("/signup", signup)
		r.Get("/verify", verify)
		r.Post("/resend-verification", resendVerification)
		r.Post("/forgot-password", forgotPassword)
		r.Post("/reset-password", resetPassword)
		r.Post("/login", login)
		r.Post("/login/verify", verifyLogin)
		r.Post("/login/resend", resendLoginCode)

		r.Group(func(r chi.Router) {
			r.Use(requireAuth)
			r.Get("/me", me)
			r.Get("/profile", getProfile)
			r.Post("/profile", saveProfile)
			r.Post("/change-password", changePassword)

			r.With(requireRole("staff")).Get("/users", listUsers)
			r.With(requireRole("admin")).Post("/users", adminCreateUser)
			r.With(requireRole("admin"), parseID).Delete("/users/{id}", deleteUser)
			r.With(requireRole("admin"), parseID).Patch("/users/{id}/verification", patchVerification)
			r.With(requireRole("admin"), parseID).Patch("/users/{id}/role", patchRole)
			r.With(requireRole("admin"), parseID).Patch("/users/{id}/profile", adminUpdateProfile)
			r.With(requireRole("admin"), parseID).Post("/users/{id}/generate-password", generateUserPassword)
			r.With(requireRole("staff"), parseID).Post("/users/{id}/resend-verification", staffResendVerification)
			r.With(requireRole("admin"), parseID).Post("/users/{id}/reset-password", adminResetPassword)

			// ---- Student work-schedule system ----
			r.With(requireRole("admin")).Get("/locations", listLocations)
			r.With(requireRole("admin")).Post("/locations", createLocation)
			r.With(requireRole("admin"), parseID).Patch("/locations/{id}", updateLocation)
			r.With(requireRole("manager")).Get("/departments", listDepartments)
			r.With(requireRole("admin")).Post("/departments", createDepartment)
			r.With(requireRole("admin"), parseID).Patch("/departments/{id}", updateDepartment)
			r.With(requireRole("manager")).Get("/jobs", listJobs)
			r.With(requireRoleIn("manager", "admin")).Post("/jobs", createJob)
			r.With(requireRoleIn("manager", "admin"), parseID).Patch("/jobs/{id}", updateJob)
			r.With(requireRole("admin"), parseID).Post("/users/{id}/disable", setDisabled(true))
			r.With(requireRole("admin"), parseID).Post("/users/{id}/enable", setDisabled(false))
			r.With(requireRole("admin"), parseID).Post("/managers/{id}/assign", assignManager)

			r.With(requireRole("manager")).Get("/students", listStudents)
			r.With(requireRoleIn("manager", "admin")).Post("/workers", createWorker)
			r.With(requireRole("manager"), parseID).Get("/workers/{id}/calendar", workerCalendar)
			r.With(requireRole("manager"), parseID).Get("/workers/{id}/preferences", workerPreferences)
			r.With(requireRole("manager"), parseID).Post("/students/{id}/jobs", assignStudentJob)
			r.With(requireRole("manager"), parseID, parseJobID).Delete("/students/{id}/jobs/{jobId}", removeStudentJob)
			r.With(requireRole("manager"), parseID).Post("/students/{id}/schedule", addWeeklySchedule)
			r.With(requireRole("manager"), parseID).Patch("/users/{id}/worker", setWorkerDetails)
			r.With(requireRole("manager")).Post("/workqueue", createWorkqueueShift)
			r.With(requireRole("manager")).Get("/staff/workqueue", staffWorkqueue)
			r.With(requireRole("scheduler"), parseID).Post("/staff/workqueue/{id}/assign", assignWorkqueueShift)
			r.With(requireRole("manager")).Get("/requests", listRequests)
			r.With(requireRole("manager"), parseID).Post("/requests/{id}/approve", approveRequest)
			r.With(requireRole("manager"), parseID).Post("/requests/{id}/deny", denyRequest)

			r.Get("/me/calendar", myCalendar)
			r.Get("/workqueue", myWorkqueue)
			r.With(parseID).Post("/workqueue/{id}/pick", pickShift)
			r.Get("/me/requests", myRequests)
			r.Post("/me/requests", createRequest)
			r.With(parseID).Post("/me/requests/{id}/cancel", cancelRequest)
			r.Get("/me/preferences", myPreferences)
			r.Post("/me/preferences", addPreference)
		})
	})
	return r
}

type authUser struct {
	ID                 int    `json:"id"`
	Email              string `json:"email"`
	Role               string `json:"role"`
	EmailVerified      bool   `json:"emailVerified"`
	MustChangePassword bool   `json:"mustChangePassword"`
	HasProfile         bool   `json:"hasProfile"`
	Disabled           bool   `json:"disabled"`
	Password           string `json:"-"` // stored hash, for /api/change-password
}

type ctxKey int

const (
	userCtxKey ctxKey = iota
	targetIDKey
	targetJobIDKey
)

// A logged-in user can be mid-onboarding — temp password not yet changed,
// registration details not yet filled in, possibly both at once. Each gate
// owns its own clearing route(s), always exempt from every gate (not just its
// own) so a user working through one gate can still reach the other's route —
// enforced here so the frontend redirect isn't the only thing stopping a temp
// password or empty profile from driving the API.
var onboardingGates = []struct {
	blocked func(u *authUser) bool
	message string
}{
	{func(u *authUser) bool { return u.MustChangePassword }, "Password change required"},
	{func(u *authUser) bool { return !u.HasProfile }, "Profile information required"},
}

var onboardingExemptRoutes = map[string]bool{
	"GET /api/me":               true,
	"POST /api/change-password": true,
	"GET /api/profile":          true,
	"POST /api/profile":         true,
}

// Shared by every route that requires a logged-in user (attaches the user).
func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var token string
		if parts := strings.Split(r.Header.Get("Authorization"), " "); len(parts) > 1 {
			token = parts[1]
		}
		if token == "" {
			respond(w, http.StatusUnauthorized, msg("No token provided"))
			return
		}
		email, err := verifyToken(token)
		if err != nil {
			respond(w, http.StatusForbidden, msg("Invalid or expired token"))
			return
		}
		var u authUser
		err = db.QueryRow(r.Context(), `
			SELECT id, email, role, email_verified, must_change_password, password,
			       disabled,
			       EXISTS (SELECT 1 FROM user_profiles WHERE user_id = users.id)
			FROM users WHERE email = $1`, email).
			Scan(&u.ID, &u.Email, &u.Role, &u.EmailVerified, &u.MustChangePassword, &u.Password, &u.Disabled, &u.HasProfile)
		if errors.Is(err, pgx.ErrNoRows) {
			respond(w, http.StatusNotFound, msg("User not found"))
			return
		}
		if err != nil {
			// Node wraps the user lookup in the same try/catch as the JWT
			// check, so any failure here reads as a bad token.
			respond(w, http.StatusForbidden, msg("Invalid or expired token"))
			return
		}
		if verificationRequired() && !u.EmailVerified {
			respond(w, http.StatusForbidden, msg("Please verify your email"))
			return
		}
		if u.Disabled {
			respond(w, http.StatusForbidden, msg("Access disabled"))
			return
		}
		// Gates only need to know a profile exists, not its contents — routes
		// that need the full row (GET /api/profile) fetch it themselves.
		route := r.Method + " " + r.URL.Path
		if !onboardingExemptRoutes[route] {
			for _, gate := range onboardingGates {
				if gate.blocked(&u) {
					respond(w, http.StatusForbidden, msg(gate.message))
					return
				}
			}
		}
		ctx := context.WithValue(r.Context(), userCtxKey, &u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func currentUser(r *http.Request) *authUser {
	return r.Context().Value(userCtxKey).(*authUser)
}

// Chain after requireAuth: 403s unless the user's role is minRole or higher.
func requireRole(minRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !hasRole(currentUser(r).Role, minRole) {
				respond(w, http.StatusForbidden, msg("Insufficient permissions"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Allows only the given roles (exact match) — for cases where the linear
// hierarchy doesn't fit, e.g. job management is manager/admin but not scheduler.
func requireRoleIn(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, role := range roles {
				if currentUser(r).Role == role {
					next.ServeHTTP(w, r)
					return
				}
			}
			respond(w, http.StatusForbidden, msg("Insufficient permissions"))
		})
	}
}

// Shared by every route taking a :id param — rejects non-numeric ids before
// they hit the database.
func parseID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(chi.URLParam(r, "id"))
		if err != nil {
			respond(w, http.StatusBadRequest, msg("Invalid user id"))
			return
		}
		ctx := context.WithValue(r.Context(), targetIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func targetID(r *http.Request) int {
	return r.Context().Value(targetIDKey).(int)
}

// Shared by routes taking a :jobId param — rejects non-numeric ids before they
// hit the database.
func parseJobID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(chi.URLParam(r, "jobId"))
		if err != nil {
			respond(w, http.StatusBadRequest, msg("Invalid job id"))
			return
		}
		ctx := context.WithValue(r.Context(), targetJobIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func targetJobID(r *http.Request) int {
	return r.Context().Value(targetJobIDKey).(int)
}

// ---- response helpers ----

func respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func msg(message string) map[string]any {
	return map[string]any{"message": message}
}

func fail(status int, message string) map[string]any {
	return map[string]any{"success": false, "message": message}
}

// express treats a missing/unparsable body as an empty object and lets
// route-level validation produce the 400s, so decode errors are ignored.
func decodeJSON(r *http.Request, v any) {
	_ = json.NewDecoder(r.Body).Decode(v)
}

// Node logs the server-side reason, then answers with the same shape whether
// or not the response carries the success field.
func respond500(w http.ResponseWriter, context string, err error, withSuccess bool) {
	log.Printf("%s: %v", context, err)
	if withSuccess {
		respond(w, http.StatusInternalServerError, fail(http.StatusInternalServerError, "Internal server error"))
		return
	}
	respond(w, http.StatusInternalServerError, msg("Internal server error"))
}
