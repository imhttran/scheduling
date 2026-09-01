package main

// httptest integration tests against the real router. They run against a real
// Postgres; without TEST_DATABASE_URL they skip, so `go test` passes with no
// database. Set e.g.:
//
//	TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/go_template_test?sslmode=disable go test ./...
//
// Every test creates its own uniquely-addressed fixtures (unique email per
// run), so reruns against a dirty database still pass.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	testPool *pgxpool.Pool
	testRun  int64 // sequence for unique emails within one run
)

func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping database-backed tests")
	}
	if testPool == nil {
		pool, err := pgxpool.New(context.Background(), dsn)
		if err != nil {
			t.Fatalf("connect TEST_DATABASE_URL: %v", err)
		}
		ensureTestSchema(pool)
		testPool = pool
	}
	db = testPool
	return testPool
}

// Same migration main() runs, applied for tests too.
func ensureTestSchema(pool *pgxpool.Pool) {
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`)
	var applied bool
	_ = pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = 1)`).Scan(&applied)
	if applied {
		return
	}
	for _, stmt := range splitStatements(schemaSQL) {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			panic(err)
		}
	}
	_, _ = pool.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES (1)`)
}

// Boots a fresh router + httptest server with dev-like config, and seeds a
// unique test user via the real signup path. Returns helper closures.
func newTestEnv(t *testing.T) (*httptest.Server, string, string) {
	t.Helper()
	testDB(t)

	originalConfig := cfg
	cfg = Config{
		JWTSecret:                 "test-secret",
		EmailVerificationRequired: false, // same bypass the Node tests use
		FrontendURL:               "http://localhost:3000",
		MailFrom:                  "no-reply@example.edu",
		MaxAttempts:               3,
	}
	t.Cleanup(func() { cfg = originalConfig })

	server := httptest.NewServer(newRouter())
	t.Cleanup(server.Close)

	testRun++
	email := fmt.Sprintf("gotest-%d-%d@mail.edu", time.Now().UnixNano(), testRun)
	password := "Valid123!"
	return server, email, password
}

func cleanupUser(t *testing.T, email string) {
	t.Helper()
	ctx := context.Background()
	_, _ = testPool.Exec(ctx, `DELETE FROM users WHERE email = $1`, email) // cascades profile
	_, _ = testPool.Exec(ctx, `DELETE FROM email_queue WHERE "to" = $1`, email)
}

func doJSON(t *testing.T, server *httptest.Server, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader([]byte{})
	}
	req, err := http.NewRequest(method, server.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func loginAs(t *testing.T, server *httptest.Server, email, password string) string {
	t.Helper()
	status, body := doJSON(t, server, http.MethodPost, "/api/login", "", map[string]string{"email": email, "password": password})
	if status != http.StatusOK {
		t.Fatalf("login for %s failed (status %d)", email, status)
	}
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("login for %s returned no token", email)
	}
	return token
}

// Sets a user's role directly in the store, like the Node tests do.
func setRole(t *testing.T, email, role string) {
	t.Helper()
	_, err := testPool.Exec(context.Background(),
		`UPDATE users SET role = $1 WHERE email = $2`, role, email)
	if err != nil {
		t.Fatalf("set role: %v", err)
	}
}

// Fills in the profile so onboarding gates don't mask the behavior under test.
func fillProfile(t *testing.T, server *httptest.Server, token string) {
	t.Helper()
	status, body := doJSON(t, server, http.MethodPost, "/api/profile", token, map[string]any{
		"firstName": "Test", "lastName": "User", "address": "1 Test St",
		"state": "CA", "zip": "94043", "phone": "555-123-4567",
		"communicationPreference": "email",
	})
	if status != http.StatusCreated {
		t.Fatalf("profile setup failed: status %d (%v)", status, body)
	}
}

func ownUserID(t *testing.T, server *httptest.Server, token string) int {
	t.Helper()
	status, body := doJSON(t, server, http.MethodGet, "/api/me", token, nil)
	if status != http.StatusOK {
		t.Fatalf("/api/me failed: status %d (%v)", status, body)
	}
	user, _ := body["user"].(map[string]any)
	id, _ := user["id"].(float64)
	return int(id)
}

// ---- tests ----

func TestSignupWeakPassword(t *testing.T) {
	server, email, _ := newTestEnv(t)
	defer cleanupUser(t, email)

	status, body := doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{
		"email": email, "password": "S1!",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %v)", status, body)
	}
	if msg := fmt.Sprint(body["message"]); !strings.Contains(msg, "at least 8 characters") {
		t.Fatalf("message = %q, want it to mention at least 8 characters", msg)
	}
}

func TestSignupAndLoginHappyPath(t *testing.T) {
	server, email, password := newTestEnv(t)
	defer cleanupUser(t, email)

	status, body := doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{
		"email": email, "password": password,
	})
	if status != http.StatusCreated || body["success"] != true {
		t.Fatalf("signup: status %d, body %v", status, body)
	}

	status, body = doJSON(t, server, http.MethodPost, "/api/login", "", map[string]string{
		"email": email, "password": password,
	})
	if status != http.StatusOK {
		t.Fatalf("login: status %d, body %v", status, body)
	}
	if token, _ := body["token"].(string); token == "" {
		t.Fatalf("login returned no token: %v", body)
	}
}

func TestMeRequiresToken(t *testing.T) {
	server, email, password := newTestEnv(t)
	defer cleanupUser(t, email)

	if status, _ := doJSON(t, server, http.MethodGet, "/api/me", "", nil); status != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401", status)
	}

	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": email, "password": password})
	token := loginAs(t, server, email, password)

	status, body := doJSON(t, server, http.MethodGet, "/api/me", token, nil)
	if status != http.StatusOK {
		t.Fatalf("with token: status = %d, want 200 (body %v)", status, body)
	}
	user, _ := body["user"].(map[string]any)
	if user["email"] != email {
		t.Fatalf("me.user.email = %v, want %v", user["email"], email)
	}
}

func TestUsersRBAC(t *testing.T) {
	server, email, password := newTestEnv(t)
	defer cleanupUser(t, email)

	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": email, "password": password})
	token := loginAs(t, server, email, password)
	fillProfile(t, server, token) // lift the profile gate so RBAC is what's under test

	// Student (base role) is rejected.
	if status, _ := doJSON(t, server, http.MethodGet, "/api/users", token, nil); status != http.StatusForbidden {
		t.Fatalf("student: status = %d, want 403", status)
	}
	// Staff is allowed.
	setRole(t, email, "staff")
	if status, body := doJSON(t, server, http.MethodGet, "/api/users", token, nil); status != http.StatusOK {
		t.Fatalf("staff: status = %d, want 200 (body %v)", status, body)
	} else if _, ok := body["users"]; !ok {
		t.Fatalf("staff: response missing users key: %v", body)
	}
	// Admin is allowed.
	setRole(t, email, "admin")
	if status, _ := doJSON(t, server, http.MethodGet, "/api/users", token, nil); status != http.StatusOK {
		t.Fatalf("admin: status = %d, want 200", status)
	}
}

func TestDeleteOwnAccount(t *testing.T) {
	server, email, password := newTestEnv(t)
	defer cleanupUser(t, email)

	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": email, "password": password})
	token := loginAs(t, server, email, password)
	fillProfile(t, server, token)
	setRole(t, email, "admin")

	id := ownUserID(t, server, token)
	status, body := doJSON(t, server, http.MethodDelete, fmt.Sprintf("/api/users/%d", id), token, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %v)", status, body)
	}
	if body["message"] != "Cannot delete your own account" {
		t.Fatalf("message = %v", body["message"])
	}
}

func TestPatchRoleInvalidValue(t *testing.T) {
	server, email, password := newTestEnv(t)
	defer cleanupUser(t, email)

	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": email, "password": password})
	token := loginAs(t, server, email, password)
	fillProfile(t, server, token)
	setRole(t, email, "admin")

	id := ownUserID(t, server, token)
	status, body := doJSON(t, server, http.MethodPatch, fmt.Sprintf("/api/users/%d/role", id), token, map[string]string{"role": "wizard"})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %v)", status, body)
	}
	if body["message"] != "role must be one of: student, staff, manager, scheduler, admin" {
		t.Fatalf("message = %v", body["message"])
	}
}
