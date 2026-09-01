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

// Same migrations main() runs, applied for tests too.
func ensureTestSchema(pool *pgxpool.Pool) {
	ctx := context.Background()
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

// A scheduler is scoped to a single department: they only see that department's
// workqueue/departments and can only assign shifts within it.
func TestSchedulerDepartmentScoping(t *testing.T) {
	server, email, password := newTestEnv(t)
	defer cleanupUser(t, email)
	ctx := context.Background()

	// One location, two departments, one job + one open shift each.
	var locID int
	if err := testPool.QueryRow(ctx, `INSERT INTO locations (name) VALUES ($1) RETURNING id`, "Test Loc "+email).Scan(&locID); err != nil {
		t.Fatalf("insert location: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locID) })
	var deptA, deptB int
	if err := testPool.QueryRow(ctx, `INSERT INTO departments (name, location_id) VALUES ($1, $2) RETURNING id`, "Dept A "+email, locID).Scan(&deptA); err != nil {
		t.Fatalf("insert dept A: %v", err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO departments (name, location_id) VALUES ($1, $2) RETURNING id`, "Dept B "+email, locID).Scan(&deptB); err != nil {
		t.Fatalf("insert dept B: %v", err)
	}
	for _, d := range []struct {
		name string
		id   int
	}{{"Job A " + email, deptA}, {"Job B " + email, deptB}} {
		if _, err := testPool.Exec(ctx, `INSERT INTO jobs (name, department_id) VALUES ($1, $2)`, d.name, d.id); err != nil {
			t.Fatalf("insert job %s: %v", d.name, err)
		}
	}
	date := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	var shiftA, shiftB int
	if err := testPool.QueryRow(ctx, `INSERT INTO workqueue (department_id, date, start_time, end_time, status) VALUES ($1, $2, '09:00', '17:00', 'open') RETURNING id`, deptA, date).Scan(&shiftA); err != nil {
		t.Fatalf("insert shift A: %v", err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO workqueue (department_id, date, start_time, end_time, status) VALUES ($1, $2, '09:00', '17:00', 'open') RETURNING id`, deptB, date).Scan(&shiftB); err != nil {
		t.Fatalf("insert shift B: %v", err)
	}

	// Scheduler user scoped to dept A (plus a location assignment, like seeding).
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": email, "password": password})
	token := loginAs(t, server, email, password)
	fillProfile(t, server, token)
	setRole(t, email, "scheduler")
	var userID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID); err != nil {
		t.Fatalf("get user id: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO manager_assignments (user_id, location_id) VALUES ($1, $2)`, userID, locID); err != nil {
		t.Fatalf("insert manager assignment: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO scheduler_assignments (user_id, department_id) VALUES ($1, $2)`, userID, deptA); err != nil {
		t.Fatalf("insert scheduler assignment: %v", err)
	}

	// Workqueue shows only dept A's shift.
	status, body := doJSON(t, server, http.MethodGet, "/api/staff/workqueue", token, nil)
	if status != http.StatusOK {
		t.Fatalf("staff/workqueue status = %d, want 200 (body %v)", status, body)
	}
	shifts, _ := body["shifts"].([]any)
	if len(shifts) != 1 {
		t.Fatalf("staff/workqueue returned %d shifts, want 1 (dept A only)", len(shifts))
	}
	if first, _ := shifts[0].(map[string]any); first["departmentName"] != "Dept A "+email {
		t.Fatalf("shift department = %v, want dept A", first["departmentName"])
	}

	// Departments list shows only dept A.
	status, body = doJSON(t, server, http.MethodGet, "/api/departments", token, nil)
	if status != http.StatusOK {
		t.Fatalf("departments status = %d, want 200 (body %v)", status, body)
	}
	if depts, _ := body["departments"].([]any); len(depts) != 1 {
		t.Fatalf("departments returned %d, want 1 (dept A only)", len(depts))
	}

	// Assigning a shift in dept B is forbidden.
	status, body = doJSON(t, server, http.MethodPost, fmt.Sprintf("/api/staff/workqueue/%d/assign", shiftB), token, map[string]any{"userId": 0})
	if status != http.StatusForbidden {
		t.Fatalf("assign dept B shift status = %d, want 403 (body %v)", status, body)
	}

	// Assigning (unassigning) a shift in dept A succeeds.
	status, body = doJSON(t, server, http.MethodPost, fmt.Sprintf("/api/staff/workqueue/%d/assign", shiftA), token, map[string]any{"userId": 0})
	if status != http.StatusOK {
		t.Fatalf("assign dept A shift status = %d, want 200 (body %v)", status, body)
	}
}

// The scheduler coverage calendar is scoped to the scheduler's department and
// includes the assigned worker's name.
func TestSchedulerCalendarScoping(t *testing.T) {
	server, email, password := newTestEnv(t)
	defer cleanupUser(t, email)
	ctx := context.Background()

	var locID int
	if err := testPool.QueryRow(ctx, `INSERT INTO locations (name) VALUES ($1) RETURNING id`, "Cal Loc "+email).Scan(&locID); err != nil {
		t.Fatalf("insert location: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locID) })
	var deptA, deptB int
	if err := testPool.QueryRow(ctx, `INSERT INTO departments (name, location_id) VALUES ($1, $2) RETURNING id`, "Cal Dept A "+email, locID).Scan(&deptA); err != nil {
		t.Fatalf("insert dept A: %v", err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO departments (name, location_id) VALUES ($1, $2) RETURNING id`, "Cal Dept B "+email, locID).Scan(&deptB); err != nil {
		t.Fatalf("insert dept B: %v", err)
	}
	date := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	var shiftA, shiftB int
	if err := testPool.QueryRow(ctx, `INSERT INTO workqueue (department_id, date, start_time, end_time, status) VALUES ($1, $2, '09:00', '17:00', 'open') RETURNING id`, deptA, date).Scan(&shiftA); err != nil {
		t.Fatalf("insert shift A: %v", err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO workqueue (department_id, date, start_time, end_time, status) VALUES ($1, $2, '09:00', '17:00', 'open') RETURNING id`, deptB, date).Scan(&shiftB); err != nil {
		t.Fatalf("insert shift B: %v", err)
	}

	// Scheduler scoped to dept A.
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": email, "password": password})
	token := loginAs(t, server, email, password)
	fillProfile(t, server, token)
	setRole(t, email, "scheduler")
	var userID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID); err != nil {
		t.Fatalf("get user id: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO scheduler_assignments (user_id, department_id) VALUES ($1, $2)`, userID, deptA); err != nil {
		t.Fatalf("insert scheduler assignment: %v", err)
	}

	// Calendar shows only dept A's shift, with an empty assigned name.
	status, body := doJSON(t, server, http.MethodGet, "/api/scheduler/calendar", token, nil)
	if status != http.StatusOK {
		t.Fatalf("scheduler/calendar status = %d, want 200 (body %v)", status, body)
	}
	shifts, _ := body["shifts"].([]any)
	if len(shifts) != 1 {
		t.Fatalf("scheduler/calendar returned %d shifts, want 1 (dept A only)", len(shifts))
	}
	first, _ := shifts[0].(map[string]any)
	if first["departmentName"] != "Cal Dept A "+email {
		t.Fatalf("shift department = %v, want dept A", first["departmentName"])
	}
	if first["assignedUserId"] != float64(0) {
		t.Fatalf("open shift assignedUserId = %v, want 0", first["assignedUserId"])
	}
	if first["assignedName"] != "" {
		t.Fatalf("open shift assignedName = %v, want empty", first["assignedName"])
	}

	// A manager scoped to the location sees both departments' shifts.
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": "mgr-" + email, "password": password})
	mgrToken := loginAs(t, server, "mgr-"+email, password)
	fillProfile(t, server, mgrToken)
	setRole(t, "mgr-"+email, "manager")
	var mgrID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, "mgr-"+email).Scan(&mgrID); err != nil {
		t.Fatalf("get manager id: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO manager_assignments (user_id, location_id) VALUES ($1, $2)`, mgrID, locID); err != nil {
		t.Fatalf("insert manager assignment: %v", err)
	}
	status, body = doJSON(t, server, http.MethodGet, "/api/scheduler/calendar", mgrToken, nil)
	if status != http.StatusOK {
		t.Fatalf("manager scheduler/calendar status = %d, want 200 (body %v)", status, body)
	}
	if shifts, _ := body["shifts"].([]any); len(shifts) != 2 {
		t.Fatalf("manager scheduler/calendar returned %d shifts, want 2", len(shifts))
	}
}

// Assigning an hourly worker a block of hours splits the shift: the worker
// covers the first block and the remainder stays open.
func TestAssignHourlyBlockSplitsShift(t *testing.T) {
	server, email, password := newTestEnv(t)
	defer cleanupUser(t, email)
	ctx := context.Background()

	var locID int
	if err := testPool.QueryRow(ctx, `INSERT INTO locations (name) VALUES ($1) RETURNING id`, "Block Loc "+email).Scan(&locID); err != nil {
		t.Fatalf("insert location: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locID) })
	var deptID int
	if err := testPool.QueryRow(ctx, `INSERT INTO departments (name, location_id) VALUES ($1, $2) RETURNING id`, "Block Dept "+email, locID).Scan(&deptID); err != nil {
		t.Fatalf("insert dept: %v", err)
	}
	var jobID int
	if err := testPool.QueryRow(ctx, `INSERT INTO jobs (name, department_id) VALUES ($1, $2) RETURNING id`, "Block Job "+email, deptID).Scan(&jobID); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	date := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	var shiftID int
	if err := testPool.QueryRow(ctx, `INSERT INTO workqueue (department_id, date, start_time, end_time, status) VALUES ($1, $2, '09:00', '17:00', 'open') RETURNING id`, deptID, date).Scan(&shiftID); err != nil {
		t.Fatalf("insert shift: %v", err)
	}

	// Hourly worker with a job in the department.
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

	// Scheduler scoped to the department assigns the worker a 2-hour block.
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": "sched-" + email, "password": password})
	schedToken := loginAs(t, server, "sched-"+email, password)
	fillProfile(t, server, schedToken)
	setRole(t, "sched-"+email, "scheduler")
	var schedID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, "sched-"+email).Scan(&schedID); err != nil {
		t.Fatalf("get scheduler id: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO scheduler_assignments (user_id, department_id) VALUES ($1, $2)`, schedID, deptID); err != nil {
		t.Fatalf("insert scheduler assignment: %v", err)
	}

	status, body := doJSON(t, server, http.MethodPost, fmt.Sprintf("/api/staff/workqueue/%d/assign", shiftID), schedToken, map[string]any{"userId": workerID, "hours": 2})
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
	if err := testPool.QueryRow(ctx, `SELECT start_time, end_time, status FROM workqueue WHERE department_id = $1 AND id != $2`, deptID, shiftID).Scan(&remainderStart, &remainderEnd, &remainderStatus); err != nil {
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
	if err := testPool.QueryRow(ctx, `SELECT parent_shift_id FROM workqueue WHERE department_id = $1 AND id != $2`, deptID, shiftID).Scan(&parentB); err != nil {
		t.Fatalf("read parent of remainder: %v", err)
	}
	if parentA != shiftID || parentB != shiftID {
		t.Fatalf("parent_shift_id = %d/%d, want %d for both", parentA, parentB, shiftID)
	}
}

// A scheduler can place a block in the middle of a shift by passing a startTime:
// the worker covers [startTime, startTime+hours] and the slot stays open on
// both sides.
func TestAssignHourlyBlockMidSlot(t *testing.T) {
	server, email, password := newTestEnv(t)
	defer cleanupUser(t, email)
	ctx := context.Background()

	var locID int
	if err := testPool.QueryRow(ctx, `INSERT INTO locations (name) VALUES ($1) RETURNING id`, "Mid Loc "+email).Scan(&locID); err != nil {
		t.Fatalf("insert location: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locID) })
	var deptID int
	if err := testPool.QueryRow(ctx, `INSERT INTO departments (name, location_id) VALUES ($1, $2) RETURNING id`, "Mid Dept "+email, locID).Scan(&deptID); err != nil {
		t.Fatalf("insert dept: %v", err)
	}
	var jobID int
	if err := testPool.QueryRow(ctx, `INSERT INTO jobs (name, department_id) VALUES ($1, $2) RETURNING id`, "Mid Job "+email, deptID).Scan(&jobID); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	date := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	var shiftID int
	if err := testPool.QueryRow(ctx, `INSERT INTO workqueue (department_id, date, start_time, end_time, status) VALUES ($1, $2, '09:00', '17:00', 'open') RETURNING id`, deptID, date).Scan(&shiftID); err != nil {
		t.Fatalf("insert shift: %v", err)
	}

	// Hourly worker with a job in the department.
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

	// Scheduler scoped to the department assigns a 2-hour block starting at 12:00.
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": "sched-" + email, "password": password})
	schedToken := loginAs(t, server, "sched-"+email, password)
	fillProfile(t, server, schedToken)
	setRole(t, "sched-"+email, "scheduler")
	var schedID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, "sched-"+email).Scan(&schedID); err != nil {
		t.Fatalf("get scheduler id: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO scheduler_assignments (user_id, department_id) VALUES ($1, $2)`, schedID, deptID); err != nil {
		t.Fatalf("insert scheduler assignment: %v", err)
	}

	status, body := doJSON(t, server, http.MethodPost, fmt.Sprintf("/api/staff/workqueue/%d/assign", shiftID), schedToken, map[string]any{"userId": workerID, "hours": 2, "startTime": "12:00"})
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
	if err := testPool.QueryRow(ctx, `SELECT start_time, end_time FROM workqueue WHERE department_id = $1 AND id != $2 AND start_time < '12:00'`, deptID, shiftID).Scan(&beforeStart, &beforeEnd); err != nil {
		t.Fatalf("read before block: %v", err)
	}
	if beforeStart[:5] != "09:00" || beforeEnd[:5] != "12:00" {
		t.Fatalf("before block = %s-%s, want 09:00-12:00", beforeStart, beforeEnd)
	}
	if err := testPool.QueryRow(ctx, `SELECT start_time, end_time FROM workqueue WHERE department_id = $1 AND id != $2 AND start_time >= '12:00'`, deptID, shiftID).Scan(&afterStart, &afterEnd); err != nil {
		t.Fatalf("read after block: %v", err)
	}
	if afterStart[:5] != "14:00" || afterEnd[:5] != "17:00" {
		t.Fatalf("after block = %s-%s, want 14:00-17:00", afterStart, afterEnd)
	}
}

// A scheduler can assign several 2-hour blocks at once: the shift is split so
// the worker covers each selected block and the gaps stay open.
func TestAssignHourlyMultiBlock(t *testing.T) {
	server, email, password := newTestEnv(t)
	defer cleanupUser(t, email)
	ctx := context.Background()

	var locID int
	if err := testPool.QueryRow(ctx, `INSERT INTO locations (name) VALUES ($1) RETURNING id`, "Multi Loc "+email).Scan(&locID); err != nil {
		t.Fatalf("insert location: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locID) })
	var deptID int
	if err := testPool.QueryRow(ctx, `INSERT INTO departments (name, location_id) VALUES ($1, $2) RETURNING id`, "Multi Dept "+email, locID).Scan(&deptID); err != nil {
		t.Fatalf("insert dept: %v", err)
	}
	var jobID int
	if err := testPool.QueryRow(ctx, `INSERT INTO jobs (name, department_id) VALUES ($1, $2) RETURNING id`, "Multi Job "+email, deptID).Scan(&jobID); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	date := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	var shiftID int
	if err := testPool.QueryRow(ctx, `INSERT INTO workqueue (department_id, date, start_time, end_time, status) VALUES ($1, $2, '09:00', '17:00', 'open') RETURNING id`, deptID, date).Scan(&shiftID); err != nil {
		t.Fatalf("insert shift: %v", err)
	}

	// Hourly worker with a job in the department.
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

	// Scheduler scoped to the department assigns two non-contiguous blocks.
	doJSON(t, server, http.MethodPost, "/api/signup", "", map[string]string{"email": "sched-" + email, "password": password})
	schedToken := loginAs(t, server, "sched-"+email, password)
	fillProfile(t, server, schedToken)
	setRole(t, "sched-"+email, "scheduler")
	var schedID int
	if err := testPool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, "sched-"+email).Scan(&schedID); err != nil {
		t.Fatalf("get scheduler id: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO scheduler_assignments (user_id, department_id) VALUES ($1, $2)`, schedID, deptID); err != nil {
		t.Fatalf("insert scheduler assignment: %v", err)
	}

	status, body := doJSON(t, server, http.MethodPost, fmt.Sprintf("/api/staff/workqueue/%d/assign", shiftID), schedToken, map[string]any{
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
	rows, err := testPool.Query(ctx, `SELECT start_time, end_time, status, COALESCE(assigned_user_id, 0) FROM workqueue WHERE department_id = $1 ORDER BY start_time`, deptID)
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
