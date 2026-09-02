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
	"io"
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

// Same migrations main() runs, applied for tests too.
func ensureTestSchema(pool *pgxpool.Pool) {
	ctx := context.Background()
	// Objects live in the `scheduling` schema — create it before anything else
	// (schema_migrations itself lives there).
	_, _ = pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS scheduling`)
	_, _ = pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`)
	migrations := []struct {
		version int
		sql     string
	}{
		{1, schemaSQL},
	}
	for _, m := range migrations {
		var applied bool
		_ = pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, m.version).Scan(&applied)
		if applied {
			continue
		}
		for _, stmt := range splitStatements(m.sql) {
			if _, err := pool.Exec(ctx, stmt); err != nil {
				panic(err)
			}
		}
		_, _ = pool.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, m.version)
	}
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
		Env:                       "development", // 2FA code is always 1234
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
	deviceID := "test-device-" + email
	status, body := doJSON(t, server, http.MethodPost, "/api/login", "", map[string]string{
		"email": email, "password": password, "deviceId": deviceID,
	})
	if status != http.StatusOK {
		t.Fatalf("login for %s failed (status %d)", email, status)
	}
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("login for %s returned no token", email)
	}
	// New device: complete 2FA (dev code is always 1234) to get the real JWT.
	if twoFactor, _ := body["twoFactorRequired"].(bool); twoFactor {
		status, body = doJSON(t, server, http.MethodPost, "/api/login/verify", "", map[string]string{
			"token": token, "code": "1234", "deviceId": deviceID,
		})
		if status != http.StatusOK {
			t.Fatalf("2FA verify for %s failed (status %d, body %v)", email, status, body)
		}
		token, _ = body["token"].(string)
		if token == "" {
			t.Fatalf("2FA verify for %s returned no token", email)
		}
	}
	return token
}

// Sets a user's roles directly in the store, like the Node tests do.
func setRole(t *testing.T, email, role string) {
	t.Helper()
	_, err := testPool.Exec(context.Background(),
		`UPDATE users SET roles = ARRAY[$1] WHERE email = $2`, role, email)
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
	// Manager is allowed.
	setRole(t, email, "manager")
	if status, _ := doJSON(t, server, http.MethodGet, "/api/users", token, nil); status != http.StatusOK {
		t.Fatalf("manager: status = %d, want 200", status)
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
	if body["message"] != "role must be one of: student, staff, manager, admin" {
		t.Fatalf("message = %v", body["message"])
	}
}

// A manager scoped to a team sees only that team's shifts, teams, and can only
// assign shifts within it.
func TestTeamScoping(t *testing.T) {
	server, email, password := newTestEnv(t)
	defer cleanupUser(t, email)
	ctx := context.Background()

	// One department, two teams, one job + one open shift each.
	var deptID int
	if err := testPool.QueryRow(ctx, `INSERT INTO departments (name) VALUES ($1) RETURNING id`, "Test Dept "+email).Scan(&deptID); err != nil {
		t.Fatalf("insert department: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM departments WHERE id = $1`, deptID) })
	var teamA, teamB int
	if err := testPool.QueryRow(ctx, `INSERT INTO teams (name, department_id) VALUES ($1, $2) RETURNING id`, "Team A "+email, deptID).Scan(&teamA); err != nil {
		t.Fatalf("insert team A: %v", err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO teams (name, department_id) VALUES ($1, $2) RETURNING id`, "Team B "+email, deptID).Scan(&teamB); err != nil {
		t.Fatalf("insert team B: %v", err)
	}
	for _, tm := range []struct {
		name string
		id   int
	}{{"Job A " + email, teamA}, {"Job B " + email, teamB}} {
		if _, err := testPool.Exec(ctx, `INSERT INTO jobs (name, team_id) VALUES ($1, $2)`, tm.name, tm.id); err != nil {
			t.Fatalf("insert job %s: %v", tm.name, err)
		}
	}
	date := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	var shiftA, shiftB int
	if err := testPool.QueryRow(ctx, `INSERT INTO workqueue (team_id, date, start_time, end_time, status) VALUES ($1, $2, '09:00', '17:00', 'open') RETURNING id`, teamA, date).Scan(&shiftA); err != nil {
		t.Fatalf("insert shift A: %v", err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO workqueue (team_id, date, start_time, end_time, status) VALUES ($1, $2, '09:00', '17:00', 'open') RETURNING id`, teamB, date).Scan(&shiftB); err != nil {
		t.Fatalf("insert shift B: %v", err)
	}

	// Manager user scoped to team A.
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": email, "password": password})
	token := loginAs(t, server, email, password)
	fillProfile(t, server, token)
	setRole(t, email, "manager")
	var userID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID); err != nil {
		t.Fatalf("get user id: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE teams SET manager_id = $1 WHERE id = $2`, userID, teamA); err != nil {
		t.Fatalf("set team manager: %v", err)
	}

	// Workqueue shows only team A's shift.
	status, body := doJSON(t, server, http.MethodGet, "/api/staff/workqueue", token, nil)
	if status != http.StatusOK {
		t.Fatalf("staff/workqueue status = %d, want 200 (body %v)", status, body)
	}
	shifts, _ := body["shifts"].([]any)
	if len(shifts) != 1 {
		t.Fatalf("staff/workqueue returned %d shifts, want 1 (team A only)", len(shifts))
	}
	if first, _ := shifts[0].(map[string]any); first["teamName"] != "Team A "+email {
		t.Fatalf("shift team = %v, want team A", first["teamName"])
	}

	// Teams list shows only team A.
	status, body = doJSON(t, server, http.MethodGet, "/api/teams", token, nil)
	if status != http.StatusOK {
		t.Fatalf("teams status = %d, want 200 (body %v)", status, body)
	}
	if teams, _ := body["teams"].([]any); len(teams) != 1 {
		t.Fatalf("teams returned %d, want 1 (team A only)", len(teams))
	}

	// Assigning a shift in team B is forbidden.
	status, body = doJSON(t, server, http.MethodPost, fmt.Sprintf("/api/staff/workqueue/%d/assign", shiftB), token, map[string]any{"userId": 0})
	if status != http.StatusForbidden {
		t.Fatalf("assign team B shift status = %d, want 403 (body %v)", status, body)
	}

	// Assigning (unassigning) a shift in team A succeeds.
	status, body = doJSON(t, server, http.MethodPost, fmt.Sprintf("/api/staff/workqueue/%d/assign", shiftA), token, map[string]any{"userId": 0})
	if status != http.StatusOK {
		t.Fatalf("assign dept A shift status = %d, want 200 (body %v)", status, body)
	}
}

// The coverage calendar is scoped to the caller's scope and includes the
// assigned worker's name.
func TestCalendarScoping(t *testing.T) {
	server, email, password := newTestEnv(t)
	defer cleanupUser(t, email)
	ctx := context.Background()

	var deptID int
	if err := testPool.QueryRow(ctx, `INSERT INTO departments (name) VALUES ($1) RETURNING id`, "Cal Dept "+email).Scan(&deptID); err != nil {
		t.Fatalf("insert department: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM departments WHERE id = $1`, deptID) })
	var teamA, teamB int
	if err := testPool.QueryRow(ctx, `INSERT INTO teams (name, department_id) VALUES ($1, $2) RETURNING id`, "Cal Team A "+email, deptID).Scan(&teamA); err != nil {
		t.Fatalf("insert team A: %v", err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO teams (name, department_id) VALUES ($1, $2) RETURNING id`, "Cal Team B "+email, deptID).Scan(&teamB); err != nil {
		t.Fatalf("insert team B: %v", err)
	}
	date := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	var shiftA, shiftB int
	if err := testPool.QueryRow(ctx, `INSERT INTO workqueue (team_id, date, start_time, end_time, status) VALUES ($1, $2, '09:00', '17:00', 'open') RETURNING id`, teamA, date).Scan(&shiftA); err != nil {
		t.Fatalf("insert shift A: %v", err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO workqueue (team_id, date, start_time, end_time, status) VALUES ($1, $2, '09:00', '17:00', 'open') RETURNING id`, teamB, date).Scan(&shiftB); err != nil {
		t.Fatalf("insert shift B: %v", err)
	}

	// Manager scoped to team A.
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": email, "password": password})
	token := loginAs(t, server, email, password)
	fillProfile(t, server, token)
	setRole(t, email, "manager")
	var userID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID); err != nil {
		t.Fatalf("get user id: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE teams SET manager_id = $1 WHERE id = $2`, userID, teamA); err != nil {
		t.Fatalf("set team manager: %v", err)
	}

	// Calendar shows only team A's shift, with an empty assigned name.
	status, body := doJSON(t, server, http.MethodGet, "/api/calendar", token, nil)
	if status != http.StatusOK {
		t.Fatalf("calendar status = %d, want 200 (body %v)", status, body)
	}
	shifts, _ := body["shifts"].([]any)
	if len(shifts) != 1 {
		t.Fatalf("calendar returned %d shifts, want 1 (team A only)", len(shifts))
	}
	first, _ := shifts[0].(map[string]any)
	if first["teamName"] != "Cal Team A "+email {
		t.Fatalf("shift team = %v, want team A", first["teamName"])
	}
	if first["assignedUserId"] != float64(0) {
		t.Fatalf("open shift assignedUserId = %v, want 0", first["assignedUserId"])
	}
	if first["assignedName"] != "" {
		t.Fatalf("open shift assignedName = %v, want empty", first["assignedName"])
	}

	// A manager scoped to the department sees both teams' shifts.
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": "mgr-" + email, "password": password})
	mgrToken := loginAs(t, server, "mgr-"+email, password)
	fillProfile(t, server, mgrToken)
	setRole(t, "mgr-"+email, "manager")
	var mgrID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, "mgr-"+email).Scan(&mgrID); err != nil {
		t.Fatalf("get manager id: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE departments SET manager_id = $1 WHERE id = $2`, mgrID, deptID); err != nil {
		t.Fatalf("set department manager: %v", err)
	}
	status, body = doJSON(t, server, http.MethodGet, "/api/calendar", mgrToken, nil)
	if status != http.StatusOK {
		t.Fatalf("department manager calendar status = %d, want 200 (body %v)", status, body)
	}
	if shifts, _ := body["shifts"].([]any); len(shifts) != 2 {
		t.Fatalf("department manager calendar returned %d shifts, want 2", len(shifts))
	}
}

// Assigning an hourly worker a block of hours splits the shift: the worker
// covers the first block and the remainder stays open.
func TestAssignHourlyBlockSplitsShift(t *testing.T) {
	server, email, password := newTestEnv(t)
	defer cleanupUser(t, email)
	ctx := context.Background()

	var deptID int
	if err := testPool.QueryRow(ctx, `INSERT INTO departments (name) VALUES ($1) RETURNING id`, "Block Dept "+email).Scan(&deptID); err != nil {
		t.Fatalf("insert department: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM departments WHERE id = $1`, deptID) })
	var teamID int
	if err := testPool.QueryRow(ctx, `INSERT INTO teams (name, department_id) VALUES ($1, $2) RETURNING id`, "Block Team "+email, deptID).Scan(&teamID); err != nil {
		t.Fatalf("insert team: %v", err)
	}
	var jobID int
	if err := testPool.QueryRow(ctx, `INSERT INTO jobs (name, team_id) VALUES ($1, $2) RETURNING id`, "Block Job "+email, teamID).Scan(&jobID); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	date := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	var shiftID int
	if err := testPool.QueryRow(ctx, `INSERT INTO workqueue (team_id, date, start_time, end_time, status) VALUES ($1, $2, '09:00', '17:00', 'open') RETURNING id`, teamID, date).Scan(&shiftID); err != nil {
		t.Fatalf("insert shift: %v", err)
	}

	// Hourly worker with a job in the team.
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": email, "password": password})
	token := loginAs(t, server, email, password)
	fillProfile(t, server, token)
	setRole(t, email, "staff")
	var workerID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&workerID); err != nil {
		t.Fatalf("get worker id: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE users SET worker_type = 'hourly', hourly_limit = 20 WHERE id = $1`, workerID); err != nil {
		t.Fatalf("set hourly: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO student_jobs (user_id, job_id, min_hours, max_hours, active) VALUES ($1, $2, 0, 20, true)`, workerID, jobID); err != nil {
		t.Fatalf("insert student job: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO worker_teams (user_id, team_id, active) VALUES ($1, $2, true)`, workerID, teamID); err != nil {
		t.Fatalf("insert team membership: %v", err)
	}

	// Manager scoped to the team assigns the worker a 2-hour block.
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": "mgr-" + email, "password": password})
	mgrToken := loginAs(t, server, "mgr-"+email, password)
	fillProfile(t, server, mgrToken)
	setRole(t, "mgr-"+email, "manager")
	var mgrID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, "mgr-"+email).Scan(&mgrID); err != nil {
		t.Fatalf("get manager id: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE teams SET manager_id = $1 WHERE id = $2`, mgrID, teamID); err != nil {
		t.Fatalf("set team manager: %v", err)
	}

	status, body := doJSON(t, server, http.MethodPost, fmt.Sprintf("/api/staff/workqueue/%d/assign", shiftID), mgrToken, map[string]any{"userId": workerID, "hours": 2})
	if status != http.StatusOK {
		t.Fatalf("assign status = %d, want 200 (body %v)", status, body)
	}

	// Original shift is now assigned to the worker and ends at 11:00.
	var assignedID int
	var end string
	if err := testPool.QueryRow(ctx, `SELECT assigned_user_id, end_time FROM workqueue WHERE id = $1`, shiftID).Scan(&assignedID, &end); err != nil {
		t.Fatalf("read shift: %v", err)
	}
	if assignedID != workerID {
		t.Fatalf("shift assigned to %d, want %d", assignedID, workerID)
	}
	if end[:5] != "11:00" {
		t.Fatalf("shift end = %s, want 11:00", end)
	}

	// A new open shift covers the remainder [11:00, 17:00].
	var remainderStart, remainderEnd, remainderStatus string
	if err := testPool.QueryRow(ctx, `SELECT start_time, end_time, status FROM workqueue WHERE team_id = $1 AND id != $2`, teamID, shiftID).Scan(&remainderStart, &remainderEnd, &remainderStatus); err != nil {
		t.Fatalf("read remainder: %v", err)
	}
	if remainderStart[:5] != "11:00" || remainderEnd[:5] != "17:00" || remainderStatus != "open" {
		t.Fatalf("remainder = %s-%s %s, want 11:00-17:00 open", remainderStart, remainderEnd, remainderStatus)
	}

	// Both rows share the same parent_shift_id so the calendar can reconstruct
	// the full shift and show taken blocks.
	var parentA, parentB int
	if err := testPool.QueryRow(ctx, `SELECT parent_shift_id FROM workqueue WHERE id = $1`, shiftID).Scan(&parentA); err != nil {
		t.Fatalf("read parent of assigned: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT parent_shift_id FROM workqueue WHERE team_id = $1 AND id != $2`, teamID, shiftID).Scan(&parentB); err != nil {
		t.Fatalf("read parent of remainder: %v", err)
	}
	if parentA != shiftID || parentB != shiftID {
		t.Fatalf("parent_shift_id = %d/%d, want %d for both", parentA, parentB, shiftID)
	}
}

// A manager can place a block in the middle of a shift by passing a startTime:
// the worker covers [startTime, startTime+hours] and the slot stays open on
// both sides.
func TestAssignHourlyBlockMidSlot(t *testing.T) {
	server, email, password := newTestEnv(t)
	defer cleanupUser(t, email)
	ctx := context.Background()

	var deptID int
	if err := testPool.QueryRow(ctx, `INSERT INTO departments (name) VALUES ($1) RETURNING id`, "Mid Dept "+email).Scan(&deptID); err != nil {
		t.Fatalf("insert department: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM departments WHERE id = $1`, deptID) })
	var teamID int
	if err := testPool.QueryRow(ctx, `INSERT INTO teams (name, department_id) VALUES ($1, $2) RETURNING id`, "Mid Team "+email, deptID).Scan(&teamID); err != nil {
		t.Fatalf("insert team: %v", err)
	}
	var jobID int
	if err := testPool.QueryRow(ctx, `INSERT INTO jobs (name, team_id) VALUES ($1, $2) RETURNING id`, "Mid Job "+email, teamID).Scan(&jobID); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	date := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	var shiftID int
	if err := testPool.QueryRow(ctx, `INSERT INTO workqueue (team_id, date, start_time, end_time, status) VALUES ($1, $2, '09:00', '17:00', 'open') RETURNING id`, teamID, date).Scan(&shiftID); err != nil {
		t.Fatalf("insert shift: %v", err)
	}

	// Hourly worker with a job in the team.
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": email, "password": password})
	token := loginAs(t, server, email, password)
	fillProfile(t, server, token)
	setRole(t, email, "staff")
	var workerID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&workerID); err != nil {
		t.Fatalf("get worker id: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE users SET worker_type = 'hourly', hourly_limit = 20 WHERE id = $1`, workerID); err != nil {
		t.Fatalf("set hourly: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO student_jobs (user_id, job_id, min_hours, max_hours, active) VALUES ($1, $2, 0, 20, true)`, workerID, jobID); err != nil {
		t.Fatalf("insert student job: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO worker_teams (user_id, team_id, active) VALUES ($1, $2, true)`, workerID, teamID); err != nil {
		t.Fatalf("insert team membership: %v", err)
	}

	// Manager scoped to the team assigns a 2-hour block starting at 12:00.
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": "mgr-" + email, "password": password})
	mgrToken := loginAs(t, server, "mgr-"+email, password)
	fillProfile(t, server, mgrToken)
	setRole(t, "mgr-"+email, "manager")
	var mgrID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, "mgr-"+email).Scan(&mgrID); err != nil {
		t.Fatalf("get manager id: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE teams SET manager_id = $1 WHERE id = $2`, mgrID, teamID); err != nil {
		t.Fatalf("set team manager: %v", err)
	}

	status, body := doJSON(t, server, http.MethodPost, fmt.Sprintf("/api/staff/workqueue/%d/assign", shiftID), mgrToken, map[string]any{"userId": workerID, "hours": 2, "startTime": "12:00"})
	if status != http.StatusOK {
		t.Fatalf("assign status = %d, want 200 (body %v)", status, body)
	}

	// Original shift is now assigned to the worker and covers [12:00, 14:00].
	var assignedID int
	var start, end string
	if err := testPool.QueryRow(ctx, `SELECT assigned_user_id, start_time, end_time FROM workqueue WHERE id = $1`, shiftID).Scan(&assignedID, &start, &end); err != nil {
		t.Fatalf("read shift: %v", err)
	}
	if assignedID != workerID {
		t.Fatalf("shift assigned to %d, want %d", assignedID, workerID)
	}
	if start[:5] != "12:00" || end[:5] != "14:00" {
		t.Fatalf("assigned block = %s-%s, want 12:00-14:00", start, end)
	}

	// Two open shifts cover the parts before and after the block.
	var beforeStart, beforeEnd, afterStart, afterEnd string
	if err := testPool.QueryRow(ctx, `SELECT start_time, end_time FROM workqueue WHERE team_id = $1 AND id != $2 AND start_time < '12:00'`, teamID, shiftID).Scan(&beforeStart, &beforeEnd); err != nil {
		t.Fatalf("read before block: %v", err)
	}
	if beforeStart[:5] != "09:00" || beforeEnd[:5] != "12:00" {
		t.Fatalf("before block = %s-%s, want 09:00-12:00", beforeStart, beforeEnd)
	}
	if err := testPool.QueryRow(ctx, `SELECT start_time, end_time FROM workqueue WHERE team_id = $1 AND id != $2 AND start_time >= '12:00'`, teamID, shiftID).Scan(&afterStart, &afterEnd); err != nil {
		t.Fatalf("read after block: %v", err)
	}
	if afterStart[:5] != "14:00" || afterEnd[:5] != "17:00" {
		t.Fatalf("after block = %s-%s, want 14:00-17:00", afterStart, afterEnd)
	}
}

// A manager can assign several 2-hour blocks at once: the shift is split so
// the worker covers each selected block and the gaps stay open.
func TestAssignHourlyMultiBlock(t *testing.T) {
	server, email, password := newTestEnv(t)
	defer cleanupUser(t, email)
	ctx := context.Background()

	var deptID int
	if err := testPool.QueryRow(ctx, `INSERT INTO departments (name) VALUES ($1) RETURNING id`, "Multi Dept "+email).Scan(&deptID); err != nil {
		t.Fatalf("insert department: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM departments WHERE id = $1`, deptID) })
	var teamID int
	if err := testPool.QueryRow(ctx, `INSERT INTO teams (name, department_id) VALUES ($1, $2) RETURNING id`, "Multi Team "+email, deptID).Scan(&teamID); err != nil {
		t.Fatalf("insert team: %v", err)
	}
	var jobID int
	if err := testPool.QueryRow(ctx, `INSERT INTO jobs (name, team_id) VALUES ($1, $2) RETURNING id`, "Multi Job "+email, teamID).Scan(&jobID); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	date := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	var shiftID int
	if err := testPool.QueryRow(ctx, `INSERT INTO workqueue (team_id, date, start_time, end_time, status) VALUES ($1, $2, '09:00', '17:00', 'open') RETURNING id`, teamID, date).Scan(&shiftID); err != nil {
		t.Fatalf("insert shift: %v", err)
	}

	// Hourly worker with a job in the team.
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": email, "password": password})
	token := loginAs(t, server, email, password)
	fillProfile(t, server, token)
	setRole(t, email, "staff")
	var workerID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&workerID); err != nil {
		t.Fatalf("get worker id: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE users SET worker_type = 'hourly', hourly_limit = 20 WHERE id = $1`, workerID); err != nil {
		t.Fatalf("set hourly: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO student_jobs (user_id, job_id, min_hours, max_hours, active) VALUES ($1, $2, 0, 20, true)`, workerID, jobID); err != nil {
		t.Fatalf("insert student job: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO worker_teams (user_id, team_id, active) VALUES ($1, $2, true)`, workerID, teamID); err != nil {
		t.Fatalf("insert team membership: %v", err)
	}

	// Manager scoped to the team assigns two non-contiguous blocks.
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": "mgr-" + email, "password": password})
	mgrToken := loginAs(t, server, "mgr-"+email, password)
	fillProfile(t, server, mgrToken)
	setRole(t, "mgr-"+email, "manager")
	var mgrID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, "mgr-"+email).Scan(&mgrID); err != nil {
		t.Fatalf("get manager id: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE teams SET manager_id = $1 WHERE id = $2`, mgrID, teamID); err != nil {
		t.Fatalf("set team manager: %v", err)
	}

	status, body := doJSON(t, server, http.MethodPost, fmt.Sprintf("/api/staff/workqueue/%d/assign", shiftID), mgrToken, map[string]any{
		"userId": workerID,
		"blocks": []map[string]string{
			{"startTime": "09:00", "endTime": "11:00"},
			{"startTime": "13:00", "endTime": "15:00"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("assign status = %d, want 200 (body %v)", status, body)
	}

	// The shift is split into assigned blocks and open gaps.
	rows, err := testPool.Query(ctx, `SELECT start_time, end_time, status, COALESCE(assigned_user_id, 0) FROM workqueue WHERE team_id = $1 ORDER BY start_time`, teamID)
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	defer rows.Close()
	type row struct {
		start, end, status string
		assigned           int
	}
	got := []row{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.start, &r.end, &r.status, &r.assigned); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		got = append(got, r)
	}
	want := []row{
		{"09:00", "11:00", "assigned", workerID},
		{"11:00", "13:00", "open", 0},
		{"13:00", "15:00", "assigned", workerID},
		{"15:00", "17:00", "open", 0},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].start[:5] != want[i].start || got[i].end[:5] != want[i].end || got[i].status != want[i].status || got[i].assigned != want[i].assigned {
			t.Fatalf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// Every request/assignment/approval action lands an audit_log row with the
// right actor, and the admin-only GET /api/audit view exposes them.
func TestAuditTrail(t *testing.T) {
	server, email, password := newTestEnv(t)
	defer cleanupUser(t, email)
	defer cleanupUser(t, "admin-"+email)
	ctx := context.Background()

	// One department/team/job and three open shifts.
	var deptID int
	if err := testPool.QueryRow(ctx, `INSERT INTO departments (name) VALUES ($1) RETURNING id`, "Audit Dept "+email).Scan(&deptID); err != nil {
		t.Fatalf("insert department: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM departments WHERE id = $1`, deptID) })
	var teamID int
	if err := testPool.QueryRow(ctx, `INSERT INTO teams (name, department_id) VALUES ($1, $2) RETURNING id`, "Audit Team "+email, deptID).Scan(&teamID); err != nil {
		t.Fatalf("insert team: %v", err)
	}
	var jobID int
	if err := testPool.QueryRow(ctx, `INSERT INTO jobs (name, team_id) VALUES ($1, $2) RETURNING id`, "Audit Job "+email, teamID).Scan(&jobID); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	date := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	shiftIDs := make([]int, 3)
	for i := range shiftIDs {
		if err := testPool.QueryRow(ctx, `INSERT INTO workqueue (team_id, date, start_time, end_time, status) VALUES ($1, $2, '09:00', '17:00', 'open') RETURNING id`, teamID, date).Scan(&shiftIDs[i]); err != nil {
			t.Fatalf("insert shift: %v", err)
		}
	}

	// Student with a job in the department.
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": email, "password": password})
	token := loginAs(t, server, email, password)
	fillProfile(t, server, token)
	var studentID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&studentID); err != nil {
		t.Fatalf("get student id: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO student_jobs (user_id, job_id, min_hours, max_hours, active) VALUES ($1, $2, 0, 20, true)`, studentID, jobID); err != nil {
		t.Fatalf("insert student job: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO worker_teams (user_id, team_id, active) VALUES ($1, $2, true)`, studentID, teamID); err != nil {
		t.Fatalf("insert worker team: %v", err)
	}

	// Admin does the approving/denying.
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": "admin-" + email, "password": password})
	adminToken := loginAs(t, server, "admin-"+email, password)
	fillProfile(t, server, adminToken)
	setRole(t, "admin-"+email, "admin")
	adminID := ownUserID(t, server, adminToken)

	assertAudit := func(action, entityType string, entityID, wantActor int) {
		t.Helper()
		var actorID int
		err := testPool.QueryRow(ctx,
			`SELECT actor_id FROM audit_log WHERE action = $1 AND entity_type = $2 AND entity_id = $3`,
			action, entityType, entityID).Scan(&actorID)
		if err != nil {
			t.Fatalf("no audit row for %s on %s %d: %v", action, entityType, entityID, err)
		}
		if actorID != wantActor {
			t.Fatalf("audit %s actor = %d, want %d", action, actorID, wantActor)
		}
	}

	// Student picks a shift (self-assignment).
	status, body := doJSON(t, server, http.MethodPost, fmt.Sprintf("/api/workqueue/%d/pick", shiftIDs[0]), token, nil)
	if status != http.StatusOK {
		t.Fatalf("pick status = %d, want 200 (body %v)", status, body)
	}
	assertAudit("shift.picked", "workqueue", shiftIDs[0], studentID)
	var pickedDetails map[string]any
	if err := testPool.QueryRow(ctx,
		`SELECT details FROM audit_log WHERE action = 'shift.picked' AND entity_id = $1`, shiftIDs[0]).Scan(&pickedDetails); err != nil {
		t.Fatalf("read pick details: %v", err)
	}
	if pickedDetails["startTime"] == nil || pickedDetails["hours"] == nil {
		t.Fatalf("pick audit details incomplete: %v", pickedDetails)
	}

	// Student requests an overflow shift; admin approves it.
	status, body = doJSON(t, server, http.MethodPost, "/api/me/requests", token, map[string]any{
		"workqueueId": shiftIDs[1], "type": "overflow", "reason": "extra hours",
	})
	if status != http.StatusOK {
		t.Fatalf("create request status = %d, want 200 (body %v)", status, body)
	}
	var reqID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM schedule_requests WHERE user_id = $1 AND workqueue_id = $2 ORDER BY id DESC LIMIT 1`, studentID, shiftIDs[1]).Scan(&reqID); err != nil {
		t.Fatalf("get request id: %v", err)
	}
	assertAudit("request.created", "schedule_request", reqID, studentID)

	status, body = doJSON(t, server, http.MethodPost, fmt.Sprintf("/api/requests/%d/approve", reqID), adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("approve status = %d, want 200 (body %v)", status, body)
	}
	assertAudit("request.approved", "schedule_request", reqID, adminID)
	var wqStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM workqueue WHERE id = $1`, shiftIDs[1]).Scan(&wqStatus); err != nil || wqStatus != "assigned" {
		t.Fatalf("shift status after approve = %q (err %v), want assigned", wqStatus, err)
	}

	// Second request gets denied.
	doJSON(t, server, http.MethodPost, "/api/me/requests", token, map[string]any{
		"workqueueId": shiftIDs[2], "type": "overflow", "reason": "more hours",
	})
	var denyID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM schedule_requests WHERE user_id = $1 AND workqueue_id = $2 ORDER BY id DESC LIMIT 1`, studentID, shiftIDs[2]).Scan(&denyID); err != nil {
		t.Fatalf("get deny request id: %v", err)
	}
	status, body = doJSON(t, server, http.MethodPost, fmt.Sprintf("/api/requests/%d/deny", denyID), adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("deny status = %d, want 200 (body %v)", status, body)
	}
	assertAudit("request.denied", "schedule_request", denyID, adminID)
	var deniedDetails map[string]any
	if err := testPool.QueryRow(ctx,
		`SELECT details FROM audit_log WHERE action = 'request.denied' AND entity_id = $1`, denyID).Scan(&deniedDetails); err != nil {
		t.Fatalf("read deny details: %v", err)
	}
	if deniedDetails["type"] == nil || deniedDetails["workqueueId"] == nil {
		t.Fatalf("deny audit details incomplete: %v", deniedDetails)
	}

	// The audit view is manager+ only and filters by action prefix.
	if status, _ = doJSON(t, server, http.MethodGet, "/api/audit", token, nil); status != http.StatusForbidden {
		t.Fatalf("student /api/audit status = %d, want 403", status)
	}
	status, body = doJSON(t, server, http.MethodGet, "/api/audit?action=request", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("admin /api/audit status = %d, want 200 (body %v)", status, body)
	}
	entries, _ := body["entries"].([]any)
	if len(entries) < 3 {
		t.Fatalf("audit entries = %d, want at least 3", len(entries))
	}
	for _, e := range entries {
		action, _ := e.(map[string]any)["action"].(string)
		if !strings.HasPrefix(action, "request.") {
			t.Fatalf("filter returned non-request action %q", action)
		}
	}

	// A manager sees their department's teams' entries; a manager of another
	// department sees none of them.
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": "mgr-here-" + email, "password": password})
	hereToken := loginAs(t, server, "mgr-here-"+email, password)
	fillProfile(t, server, hereToken)
	setRole(t, "mgr-here-"+email, "manager")
	defer cleanupUser(t, "mgr-here-"+email)
	var hereID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, "mgr-here-"+email).Scan(&hereID); err != nil {
		t.Fatalf("get manager id: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE departments SET manager_id = $1 WHERE id = $2`, hereID, deptID); err != nil {
		t.Fatalf("assign department manager: %v", err)
	}
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": "mgr-away-" + email, "password": password})
	awayToken := loginAs(t, server, "mgr-away-"+email, password)
	fillProfile(t, server, awayToken)
	setRole(t, "mgr-away-"+email, "manager")
	defer cleanupUser(t, "mgr-away-"+email)
	var awayID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, "mgr-away-"+email).Scan(&awayID); err != nil {
		t.Fatalf("get away manager id: %v", err)
	}
	var otherDeptID int
	if err := testPool.QueryRow(ctx, `INSERT INTO departments (name) VALUES ($1) RETURNING id`, "Audit Other Dept "+email).Scan(&otherDeptID); err != nil {
		t.Fatalf("insert other department: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM departments WHERE id = $1`, otherDeptID) })
	if _, err := testPool.Exec(ctx, `UPDATE departments SET manager_id = $1 WHERE id = $2`, awayID, otherDeptID); err != nil {
		t.Fatalf("assign away manager: %v", err)
	}

	status, body = doJSON(t, server, http.MethodGet, "/api/audit", hereToken, nil)
	if status != http.StatusOK {
		t.Fatalf("location manager /api/audit status = %d, want 200 (body %v)", status, body)
	}
	here, _ := body["entries"].([]any)
	if len(here) == 0 {
		t.Fatal("location manager sees no audit entries, want the request/shift rows")
	}
	status, body = doJSON(t, server, http.MethodGet, "/api/audit", awayToken, nil)
	if status != http.StatusOK {
		t.Fatalf("other-location manager /api/audit status = %d, want 200 (body %v)", status, body)
	}
	away, _ := body["entries"].([]any)
	for _, e := range away {
		entry := e.(map[string]any)
		if int(entry["entityId"].(float64)) == reqID {
			t.Fatalf("other-location manager saw entity %d (email %v)", reqID, entry["actorEmail"])
		}
	}

	// The CSV export honors the same scoping; the per-entity filter returns
	// just the picked shift's audit rows.
	status, body = doJSON(t, server, http.MethodGet,
		fmt.Sprintf("/api/audit?entityType=workqueue&entityId=%d", shiftIDs[0]), hereToken, nil)
	if status != http.StatusOK {
		t.Fatalf("entity-filtered /api/audit status = %d, want 200 (body %v)", status, body)
	}
	filtered, _ := body["entries"].([]any)
	if len(filtered) != 1 {
		t.Fatalf("entity-filtered entries = %d, want 1", len(filtered))
	}
	getRaw := func(path, authToken string) (int, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+authToken)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		return resp.StatusCode, string(raw)
	}
	awayStatus, awayCSV := getRaw("/api/audit/export", awayToken)
	if awayStatus != http.StatusOK || strings.Contains(awayCSV, "request.") {
		t.Fatalf("away manager export status = %d, should contain none of this location's rows (body %.200q)", awayStatus, awayCSV)
	}
	hereStatus, hereCSV := getRaw("/api/audit/export", hereToken)
	if hereStatus != http.StatusOK || !strings.Contains(hereCSV, "request.created") {
		t.Fatalf("location manager export status = %d, want 200 with csv rows (body %.200q)", hereStatus, hereCSV)
	}
}
