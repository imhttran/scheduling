package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- helpers ----

// Department IDs the student is qualified for via their active jobs.
func studentDepartmentIDs(ctx context.Context, userID int) ([]int, error) {
	rows, err := db.Query(ctx, `
		SELECT j.department_id FROM student_jobs sj
		JOIN jobs j ON j.id = sj.job_id
		WHERE sj.user_id = $1 AND sj.active`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// The manager's location scope (0 if unassigned). Admin bypasses the check.
func managerLocationID(ctx context.Context, userID int) (int, error) {
	var locID int
	err := db.QueryRow(ctx,
		`SELECT location_id FROM manager_assignments WHERE user_id = $1`, userID).Scan(&locID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return locID, err
}

// schedulerDepartmentID returns the department a scheduler is scoped to (0 if
// none). Schedulers manage the schedule for a single department.
func schedulerDepartmentID(ctx context.Context, userID int) (int, error) {
	var deptID int
	err := db.QueryRow(ctx,
		`SELECT department_id FROM scheduler_assignments WHERE user_id = $1`, userID).Scan(&deptID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return deptID, err
}

// Date for a day_of_week (0=Sun..6=Sat) in the current week.
func weekDate(monday time.Time, dow int) time.Time {
	return monday.AddDate(0, 0, (dow+6)%7)
}

// Validates "HH:MM" and returns it normalized, or "" if invalid.
func parseTime(s string) string {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return ""
	}
	return t.Format("15:04")
}

func hoursBetween(start, end string) float64 {
	// DB TIME columns scan as "HH:MM:SS.ffffff"; parse just the HH:MM part.
	// time.Parse returns year 0000 for valid times, so normalize to year 2000
	// for consistent math. "24:00" (end-of-day) isn't a valid 15:04 time, so
	// treat it as 00:00 the next day.
	parse := func(s string) time.Time {
		if len(s) >= 5 {
			s = s[:5]
		}
		if s == "24:00" {
			return time.Date(2000, 1, 2, 0, 0, 0, 0, time.UTC)
		}
		t, _ := time.Parse("15:04", s)
		return t.AddDate(2000, 0, 0)
	}
	return parse(end).Sub(parse(start)).Hours()
}

// ---- admin: locations & departments ----

func listLocations(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(r.Context(), `
		SELECT l.id, l.name, l.abbreviation, l.address, l.address2, l.city, l.state, l.zip, l.country,
		       ma.user_id, u.email
		FROM locations l
		LEFT JOIN LATERAL (
			SELECT ma.user_id FROM manager_assignments ma
			WHERE ma.location_id = l.id LIMIT 1
		) ma ON true
		LEFT JOIN users u ON u.id = ma.user_id
		ORDER BY l.name`)
	if err != nil {
		respond500(w, "List Locations Error", err, false)
		return
	}
	defer rows.Close()
	locations := []map[string]any{}
	for rows.Next() {
		var id int
		var name, abbreviation, address, address2, city, state, zip, country *string
		var managerID *int
		var managerEmail *string
		if err := rows.Scan(&id, &name, &abbreviation, &address, &address2, &city, &state, &zip, &country, &managerID, &managerEmail); err != nil {
			respond500(w, "List Locations Error", err, false)
			return
		}
		locations = append(locations, map[string]any{
			"id": id, "name": name, "abbreviation": abbreviation,
			"address": address, "address2": address2, "city": city,
			"state": state, "zip": zip, "country": country,
			"managerId": managerID, "managerEmail": managerEmail,
		})
	}
	respond(w, http.StatusOK, map[string]any{"locations": locations})
}

func createLocation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name         string `json:"name"`
		Abbreviation string `json:"abbreviation"`
		Address      string `json:"address"`
		Address2     string `json:"address2"`
		City         string `json:"city"`
		State        string `json:"state"`
		Zip          string `json:"zip"`
		Country      string `json:"country"`
	}
	decodeJSON(r, &body)
	if body.Name == "" {
		respond(w, http.StatusBadRequest, msg("name is required"))
		return
	}
	var id int
	err := db.QueryRow(r.Context(), `
		INSERT INTO locations
			(name, abbreviation, address, address2, city, state, zip, country)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''))
		RETURNING id`,
		body.Name, body.Abbreviation, body.Address, body.Address2,
		body.City, body.State, body.Zip, body.Country).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			respond(w, http.StatusBadRequest, msg("Location already exists"))
			return
		}
		respond500(w, "Create Location Error", err, true)
		return
	}
	respond(w, http.StatusCreated, map[string]any{"success": true, "location": map[string]any{"id": id, "name": body.Name}})
}

func updateLocation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name         string `json:"name"`
		Abbreviation string `json:"abbreviation"`
		Address      string `json:"address"`
		Address2     string `json:"address2"`
		City         string `json:"city"`
		State        string `json:"state"`
		Zip          string `json:"zip"`
		Country      string `json:"country"`
	}
	decodeJSON(r, &body)
	if body.Name == "" {
		respond(w, http.StatusBadRequest, msg("name is required"))
		return
	}
	tag, err := db.Exec(r.Context(), `
		UPDATE locations SET
			name = $1, abbreviation = NULLIF($2, ''), address = NULLIF($3, ''),
			address2 = NULLIF($4, ''), city = NULLIF($5, ''), state = NULLIF($6, ''),
			zip = NULLIF($7, ''), country = NULLIF($8, '')
		WHERE id = $9`,
		body.Name, body.Abbreviation, body.Address, body.Address2,
		body.City, body.State, body.Zip, body.Country, targetID(r))
	if err != nil {
		if isUniqueViolation(err) {
			respond(w, http.StatusBadRequest, msg("Location already exists"))
			return
		}
		respond500(w, "Update Location Error", err, true)
		return
	}
	if tag.RowsAffected() == 0 {
		respond(w, http.StatusNotFound, msg("Location not found"))
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Location updated"})
}

func listDepartments(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	query := `
		SELECT d.id, d.name, d.department_code, d.location_id, l.name
		FROM departments d JOIN locations l ON l.id = d.location_id`
	args := []any{}
	if !hasRole(u.Role, "admin") {
		if u.Role == "scheduler" {
			deptID, err := schedulerDepartmentID(r.Context(), u.ID)
			if err != nil {
				respond500(w, "List Departments Error", err, false)
				return
			}
			query += ` WHERE d.id = $1`
			args = append(args, deptID)
		} else {
			locID, err := managerLocationID(r.Context(), u.ID)
			if err != nil {
				respond500(w, "List Departments Error", err, false)
				return
			}
			query += ` WHERE d.location_id = $1`
			args = append(args, locID)
		}
	}
	query += ` ORDER BY d.name`
	rows, err := db.Query(r.Context(), query, args...)
	if err != nil {
		respond500(w, "List Departments Error", err, false)
		return
	}
	defer rows.Close()
	departments := []map[string]any{}
	for rows.Next() {
		var id, locID int
		var name, locName string
		var code *string
		if err := rows.Scan(&id, &name, &code, &locID, &locName); err != nil {
			respond500(w, "List Departments Error", err, false)
			return
		}
		departments = append(departments, map[string]any{
			"id": id, "name": name, "departmentCode": code, "locationId": locID, "locationName": locName,
		})
	}
	respond(w, http.StatusOK, map[string]any{"departments": departments})
}

func createDepartment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name           string `json:"name"`
		DepartmentCode string `json:"departmentCode"`
		LocationID     int    `json:"locationId"`
	}
	decodeJSON(r, &body)
	if body.Name == "" || body.LocationID == 0 {
		respond(w, http.StatusBadRequest, msg("name and locationId are required"))
		return
	}
	if len(body.DepartmentCode) > 20 {
		respond(w, http.StatusBadRequest, msg("departmentCode must be 20 characters or fewer"))
		return
	}
	var id int
	err := db.QueryRow(r.Context(), `
		INSERT INTO departments (name, department_code, location_id) VALUES ($1, NULLIF($2, ''), $3)
		RETURNING id`,
		body.Name, body.DepartmentCode, body.LocationID).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			respond(w, http.StatusBadRequest, msg("Department already exists"))
			return
		}
		respond500(w, "Create Department Error", err, true)
		return
	}
	respond(w, http.StatusCreated, map[string]any{"success": true, "department": map[string]any{"id": id, "name": body.Name, "departmentCode": body.DepartmentCode, "locationId": body.LocationID}})
}

func updateDepartment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name           string `json:"name"`
		DepartmentCode string `json:"departmentCode"`
		LocationID     int    `json:"locationId"`
	}
	decodeJSON(r, &body)
	if body.Name == "" || body.LocationID == 0 {
		respond(w, http.StatusBadRequest, msg("name and locationId are required"))
		return
	}
	if len(body.DepartmentCode) > 20 {
		respond(w, http.StatusBadRequest, msg("departmentCode must be 20 characters or fewer"))
		return
	}
	tag, err := db.Exec(r.Context(), `
		UPDATE departments SET name = $1, department_code = NULLIF($2, ''), location_id = $3
		WHERE id = $4`,
		body.Name, body.DepartmentCode, body.LocationID, targetID(r))
	if err != nil {
		if isUniqueViolation(err) {
			respond(w, http.StatusBadRequest, msg("Department already exists"))
			return
		}
		respond500(w, "Update Department Error", err, true)
		return
	}
	if tag.RowsAffected() == 0 {
		respond(w, http.StatusNotFound, msg("Department not found"))
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Department updated"})
}

// ---- admin: jobs ----

type jobScheduleInput struct {
	DayOfWeek int    `json:"dayOfWeek"`
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}

type jobHolidayInput struct {
	Date   string `json:"date"`
	Reason string `json:"reason"`
}

func listJobs(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	query := `
		SELECT j.id, j.name, j.department_id, d.name, d.location_id, l.name, j.optimal_workers,
		       (SELECT COUNT(*) FROM student_jobs sj WHERE sj.job_id = j.id AND sj.active) AS current_workers
		FROM jobs j
		JOIN departments d ON d.id = j.department_id
		JOIN locations l ON l.id = d.location_id`
	args := []any{}
	if !hasRole(u.Role, "admin") {
		locID, err := managerLocationID(r.Context(), u.ID)
		if err != nil {
			respond500(w, "List Jobs Error", err, false)
			return
		}
		query += ` WHERE d.location_id = $1`
		args = append(args, locID)
	}
	query += ` ORDER BY d.name, j.name`
	rows, err := db.Query(r.Context(), query, args...)
	if err != nil {
		respond500(w, "List Jobs Error", err, false)
		return
	}
	defer rows.Close()
	jobs := []map[string]any{}
	for rows.Next() {
		var id, deptID, locID, optimalWorkers, currentWorkers int
		var name, deptName, locName string
		if err := rows.Scan(&id, &name, &deptID, &deptName, &locID, &locName, &optimalWorkers, &currentWorkers); err != nil {
			respond500(w, "List Jobs Error", err, false)
			return
		}
		schedules, templateWeekly, err := jobSchedules(r.Context(), id)
		if err != nil {
			respond500(w, "List Jobs Error", err, false)
			return
		}
		holidays, err := jobHolidays(r.Context(), id)
		if err != nil {
			respond500(w, "List Jobs Error", err, false)
			return
		}
		daily := map[int]float64{}
		for _, s := range schedules {
			daily[s["dayOfWeek"].(int)] = s["hours"].(float64)
		}
		closed, err := holidayHoursThisWeek(r.Context(), id, daily)
		if err != nil {
			respond500(w, "List Jobs Error", err, false)
			return
		}
		jobs = append(jobs, map[string]any{
			"id": id, "name": name, "departmentId": deptID, "departmentName": deptName,
			"locationId": locID, "locationName": locName,
			"optimalWorkers": optimalWorkers, "currentWorkers": currentWorkers,
			"weeklyHours": templateWeekly - closed, "schedules": schedules, "holidays": holidays,
		})
	}
	respond(w, http.StatusOK, map[string]any{"jobs": jobs})
}

// A job's daily operating hours (template), plus the total weekly hours across
// all template days (before holiday closures are subtracted).
func jobSchedules(ctx context.Context, jobID int) ([]map[string]any, float64, error) {
	rows, err := db.Query(ctx, `
		SELECT day_of_week, start_time, end_time
		FROM job_schedules WHERE job_id = $1 ORDER BY day_of_week`, jobID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	schedules := []map[string]any{}
	weekly := 0.0
	for rows.Next() {
		var dow int
		var start, end string
		if err := rows.Scan(&dow, &start, &end); err != nil {
			return nil, 0, err
		}
		hours := hoursBetween(start, end)
		weekly += hours
		schedules = append(schedules, map[string]any{
			"dayOfWeek": dow, "startTime": start[:5], "endTime": end[:5], "hours": hours,
		})
	}
	return schedules, weekly, rows.Err()
}

// A job's date-specific holiday closures, newest-looking first.
func jobHolidays(ctx context.Context, jobID int) ([]map[string]any, error) {
	rows, err := db.Query(ctx, `
		SELECT date, COALESCE(reason, '') FROM job_holidays WHERE job_id = $1 ORDER BY date`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	holidays := []map[string]any{}
	for rows.Next() {
		var d time.Time
		var reason string
		if err := rows.Scan(&d, &reason); err != nil {
			return nil, err
		}
		holidays = append(holidays, map[string]any{"date": d.Format("2006-01-02"), "reason": reason})
	}
	return holidays, rows.Err()
}

// Hours this job is closed this week because a holiday falls on a day it would
// normally be open. openDaily maps day_of_week -> hours that day would run.
func holidayHoursThisWeek(ctx context.Context, jobID int, openDaily map[int]float64) (float64, error) {
	monday := weekMondayOf(time.Now())
	rows, err := db.Query(ctx, `
		SELECT date FROM job_holidays
		WHERE job_id = $1 AND date >= $2 AND date < $3 ORDER BY date`,
		jobID, monday, monday.AddDate(0, 0, 7))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	closed := 0.0
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return 0, err
		}
		closed += openDaily[int(d.Weekday())]
	}
	return closed, rows.Err()
}

// Writes a 403 (and returns false) unless the department is in the caller's
// location. Admins are exempt (they manage everything).
func deptInCallerLocation(w http.ResponseWriter, r *http.Request, deptID int) bool {
	u := currentUser(r)
	if hasRole(u.Role, "admin") {
		return true
	}
	locID, err := managerLocationID(r.Context(), u.ID)
	if err != nil {
		respond500(w, "Location Error", err, false)
		return false
	}
	var ok bool
	if err := db.QueryRow(r.Context(),
		`SELECT EXISTS (SELECT 1 FROM departments WHERE id = $1 AND location_id = $2)`,
		deptID, locID).Scan(&ok); err != nil {
		respond500(w, "Location Error", err, false)
		return false
	}
	if !ok {
		respond(w, http.StatusForbidden, msg("Department not in your location"))
		return false
	}
	return true
}

// Writes a 403 (and returns false) unless the schedule request's shift is in
// the caller's location. Admins are exempt (they manage everything).
func requestInCallerLocation(w http.ResponseWriter, r *http.Request, reqID int) bool {
	u := currentUser(r)
	if hasRole(u.Role, "admin") {
		return true
	}
	if u.Role == "scheduler" {
		deptID, err := schedulerDepartmentID(r.Context(), u.ID)
		if err != nil {
			respond500(w, "Location Error", err, false)
			return false
		}
		var ok bool
		if err := db.QueryRow(r.Context(), `
			SELECT EXISTS (
				SELECT 1 FROM schedule_requests r
				JOIN workqueue w ON w.id = r.workqueue_id
				WHERE r.id = $1 AND w.department_id = $2)`, reqID, deptID).Scan(&ok); err != nil {
			respond500(w, "Location Error", err, false)
			return false
		}
		if !ok {
			respond(w, http.StatusForbidden, msg("Request not in your department"))
			return false
		}
		return true
	}
	locID, err := managerLocationID(r.Context(), u.ID)
	if err != nil {
		respond500(w, "Location Error", err, false)
		return false
	}
	var ok bool
	if err := db.QueryRow(r.Context(), `
		SELECT EXISTS (
			SELECT 1 FROM schedule_requests r
			JOIN workqueue w ON w.id = r.workqueue_id
			JOIN departments d ON d.id = w.department_id
			WHERE r.id = $1 AND d.location_id = $2)`, reqID, locID).Scan(&ok); err != nil {
		respond500(w, "Location Error", err, false)
		return false
	}
	if !ok {
		respond(w, http.StatusForbidden, msg("Request not in your location"))
		return false
	}
	return true
}

// Default operating hours for a new job: weekdays 9am-5pm (8h each, 40h/wk)
// and weekends 10h each (20h/wk). Used when a job is created without explicit
// schedules.
func defaultJobSchedules() []jobScheduleInput {
	return []jobScheduleInput{
		{DayOfWeek: 1, StartTime: "09:00", EndTime: "17:00"}, // Mon
		{DayOfWeek: 2, StartTime: "09:00", EndTime: "17:00"}, // Tue
		{DayOfWeek: 3, StartTime: "09:00", EndTime: "17:00"}, // Wed
		{DayOfWeek: 4, StartTime: "09:00", EndTime: "17:00"}, // Thu
		{DayOfWeek: 5, StartTime: "09:00", EndTime: "17:00"}, // Fri
		{DayOfWeek: 6, StartTime: "10:00", EndTime: "20:00"}, // Sat
		{DayOfWeek: 0, StartTime: "10:00", EndTime: "20:00"}, // Sun
	}
}

// Inserts the default operating hours for a job (used by the seeder).
// Idempotent — existing rows are left untouched.
func insertDefaultJobSchedules(ctx context.Context, jobID int) error {
	for _, s := range defaultJobSchedules() {
		if _, err := db.Exec(ctx, `
			INSERT INTO job_schedules (job_id, day_of_week, start_time, end_time)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (job_id, day_of_week, start_time) DO NOTHING`, jobID, s.DayOfWeek, s.StartTime, s.EndTime); err != nil {
			return err
		}
	}
	return nil
}

func createJob(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name           string             `json:"name"`
		DepartmentID   int                `json:"departmentId"`
		OptimalWorkers int                `json:"optimalWorkers"`
		Schedules      []jobScheduleInput `json:"schedules"`
		Holidays       []jobHolidayInput  `json:"holidays"`
	}
	decodeJSON(r, &body)
	if body.Name == "" || body.DepartmentID == 0 {
		respond(w, http.StatusBadRequest, msg("name and departmentId are required"))
		return
	}
	if body.OptimalWorkers < 1 {
		body.OptimalWorkers = 1
	}
	if !validSchedules(body.Schedules) {
		respond(w, http.StatusBadRequest, msg("schedules must have unique dayOfWeek (0-6) with valid startTime/endTime"))
		return
	}
	if !validHolidays(body.Holidays) {
		respond(w, http.StatusBadRequest, msg("holidays must be unique YYYY-MM-DD dates"))
		return
	}
	if !deptInCallerLocation(w, r, body.DepartmentID) {
		return
	}
	var id int
	err := db.QueryRow(r.Context(), `
		INSERT INTO jobs (name, department_id, optimal_workers) VALUES ($1, $2, $3)
		RETURNING id`,
		body.Name, body.DepartmentID, body.OptimalWorkers).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			respond(w, http.StatusBadRequest, msg("Job already exists in this department"))
			return
		}
		respond500(w, "Create Job Error", err, true)
		return
	}
	schedules := body.Schedules
	if len(schedules) == 0 {
		schedules = defaultJobSchedules()
	}
	if err := replaceJobSchedules(r.Context(), id, schedules); err != nil {
		respond500(w, "Create Job Error", err, true)
		return
	}
	if err := replaceJobHolidays(r.Context(), id, body.Holidays); err != nil {
		respond500(w, "Create Job Error", err, true)
		return
	}
	respond(w, http.StatusCreated, map[string]any{"success": true, "job": map[string]any{"id": id, "name": body.Name, "departmentId": body.DepartmentID}})
}

func updateJob(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name           string             `json:"name"`
		DepartmentID   int                `json:"departmentId"`
		OptimalWorkers int                `json:"optimalWorkers"`
		Schedules      []jobScheduleInput `json:"schedules"`
		Holidays       []jobHolidayInput  `json:"holidays"`
	}
	decodeJSON(r, &body)
	if body.Name == "" || body.DepartmentID == 0 {
		respond(w, http.StatusBadRequest, msg("name and departmentId are required"))
		return
	}
	if body.OptimalWorkers < 1 {
		body.OptimalWorkers = 1
	}
	if !validSchedules(body.Schedules) {
		respond(w, http.StatusBadRequest, msg("schedules must have unique dayOfWeek (0-6) with valid startTime/endTime"))
		return
	}
	if !validHolidays(body.Holidays) {
		respond(w, http.StatusBadRequest, msg("holidays must be unique YYYY-MM-DD dates"))
		return
	}
	if !deptInCallerLocation(w, r, body.DepartmentID) {
		return
	}
	tag, err := db.Exec(r.Context(), `
		UPDATE jobs SET name = $1, department_id = $2, optimal_workers = $3 WHERE id = $4`,
		body.Name, body.DepartmentID, body.OptimalWorkers, targetID(r))
	if err != nil {
		if isUniqueViolation(err) {
			respond(w, http.StatusBadRequest, msg("Job already exists in this department"))
			return
		}
		respond500(w, "Update Job Error", err, true)
		return
	}
	if tag.RowsAffected() == 0 {
		respond(w, http.StatusNotFound, msg("Job not found"))
		return
	}
	if err := replaceJobSchedules(r.Context(), targetID(r), body.Schedules); err != nil {
		respond500(w, "Update Job Error", err, true)
		return
	}
	if err := replaceJobHolidays(r.Context(), targetID(r), body.Holidays); err != nil {
		respond500(w, "Update Job Error", err, true)
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Job updated"})
}

// Validates a set of daily schedules: each day 0-6, valid times, end after
// start, and no duplicate days.
func validSchedules(schedules []jobScheduleInput) bool {
	seen := map[int]bool{}
	for _, s := range schedules {
		if s.DayOfWeek < 0 || s.DayOfWeek > 6 || seen[s.DayOfWeek] {
			return false
		}
		start, end := parseTime(s.StartTime), parseTime(s.EndTime)
		if start == "" || end == "" || hoursBetween(start, end) <= 0 {
			return false
		}
		seen[s.DayOfWeek] = true
	}
	return true
}

// Validates a set of holiday dates: each parses as YYYY-MM-DD, unique.
func validHolidays(holidays []jobHolidayInput) bool {
	seen := map[string]bool{}
	for _, h := range holidays {
		if _, err := time.Parse("2006-01-02", h.Date); err != nil || seen[h.Date] {
			return false
		}
		seen[h.Date] = true
	}
	return true
}

// Replaces a job's daily schedules (delete + reinsert) in one transaction.
func replaceJobSchedules(ctx context.Context, jobID int, schedules []jobScheduleInput) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM job_schedules WHERE job_id = $1`, jobID); err != nil {
		return err
	}
	for _, s := range schedules {
		if _, err := tx.Exec(ctx, `
			INSERT INTO job_schedules (job_id, day_of_week, start_time, end_time)
			VALUES ($1, $2, $3, $4)`,
			jobID, s.DayOfWeek, parseTime(s.StartTime), parseTime(s.EndTime)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Replaces a job's holiday closures (delete + reinsert) in one transaction.
func replaceJobHolidays(ctx context.Context, jobID int, holidays []jobHolidayInput) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM job_holidays WHERE job_id = $1`, jobID); err != nil {
		return err
	}
	for _, h := range holidays {
		if _, err := tx.Exec(ctx, `
			INSERT INTO job_holidays (job_id, date, reason) VALUES ($1, $2, $3)`,
			jobID, h.Date, h.Reason); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ---- admin: one-click disable/enable ----

func setDisabled(disabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if targetID(r) == currentUser(r).ID {
			respond(w, http.StatusBadRequest, msg("Cannot disable your own account"))
			return
		}
		tag, err := db.Exec(r.Context(),
			`UPDATE users SET disabled = $1 WHERE id = $2`, disabled, targetID(r))
		if err != nil {
			respond500(w, "Set Disabled Error", err, false)
			return
		}
		if tag.RowsAffected() == 0 {
			respond(w, http.StatusNotFound, msg("User not found"))
			return
		}
		action := "enabled"
		if disabled {
			action = "disabled"
		}
		respond(w, http.StatusOK, map[string]any{"success": true, "message": "User " + action})
	}
}

// Admin assigns a manager to a location — the scope of everything they see.
func assignManager(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LocationID int `json:"locationId"`
	}
	decodeJSON(r, &body)
	if body.LocationID == 0 {
		respond(w, http.StatusBadRequest, msg("locationId required"))
		return
	}
	_, err := db.Exec(r.Context(), `
		INSERT INTO manager_assignments (user_id, location_id)
		VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET location_id = $2`,
		targetID(r), body.LocationID)
	if err != nil {
		respond500(w, "Assign Manager Error", err, true)
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Manager assigned to location"})
}

// ---- manager/scheduler: workers (students + staff) ----

func listStudents(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	query := `
		SELECT u.id, u.email, u.disabled, u.worker_type, COALESCE(u.hourly_limit, 0),
		       sj.job_id, j.name, j.department_id, d.name, d.location_id,
		       sj.min_hours, sj.max_hours, sj.active
		FROM users u
		LEFT JOIN student_jobs sj ON sj.user_id = u.id
		LEFT JOIN jobs j ON j.id = sj.job_id
		LEFT JOIN departments d ON d.id = j.department_id
		WHERE u.role IN ('student','staff')`
	args := []any{}
	if !hasRole(u.Role, "admin") {
		if u.Role == "scheduler" {
			deptID, err := schedulerDepartmentID(r.Context(), u.ID)
			if err != nil {
				respond500(w, "List Students Error", err, false)
				return
			}
			query += ` AND j.department_id = $1`
			args = append(args, deptID)
		} else {
			locID, err := managerLocationID(r.Context(), u.ID)
			if err != nil {
				respond500(w, "List Students Error", err, false)
				return
			}
			query += ` AND d.location_id = $1`
			args = append(args, locID)
		}
	}
	query += ` ORDER BY u.email, j.name`
	rows, err := db.Query(r.Context(), query, args...)
	if err != nil {
		respond500(w, "List Students Error", err, false)
		return
	}
	defer rows.Close()
	// Aggregate each worker's jobs into a single row.
	byID := map[int]map[string]any{}
	order := []int{}
	for rows.Next() {
		var id, hourlyLimit int
		var email, workerType string
		var disabled bool
		var jobID *int
		var jobName, deptName *string
		var deptID, locID *int
		var minH, maxH *int
		var active *bool
		if err := rows.Scan(&id, &email, &disabled, &workerType, &hourlyLimit, &jobID, &jobName, &deptID, &deptName, &locID, &minH, &maxH, &active); err != nil {
			respond500(w, "List Students Error", err, false)
			return
		}
		row, ok := byID[id]
		if !ok {
			pol := workerPolicyFor(workerType, hourlyLimit)
			used, err := workerWeekHours(r.Context(), id, time.Now())
			if err != nil {
				respond500(w, "List Students Error", err, false)
				return
			}
			row = map[string]any{
				"id": id, "email": email, "disabled": disabled,
				"workerType": workerType, "workerTypeLabel": workerTypeLabel(workerType),
				"hourlyLimit":   pol.regular,
				"weekHoursCap":  pol.cap,
				"weekHoursUsed": used,
				"jobs":          []map[string]any{},
			}
			byID[id] = row
			order = append(order, id)
		}
		if jobID != nil {
			row["jobs"] = append(row["jobs"].([]map[string]any), map[string]any{
				"jobId": *jobID, "jobName": *jobName, "departmentId": *deptID,
				"departmentName": *deptName, "locationId": *locID,
				"minHours": *minH, "maxHours": *maxH, "active": *active,
			})
		}
	}
	if err := rows.Err(); err != nil {
		respond500(w, "List Students Error", err, false)
		return
	}
	students := make([]map[string]any, 0, len(order))
	for _, id := range order {
		students = append(students, byID[id])
	}
	respond(w, http.StatusOK, map[string]any{"students": students})
}

func assignStudentJob(w http.ResponseWriter, r *http.Request) {
	var body struct {
		JobID    int `json:"jobId"`
		MinHours int `json:"minHours"`
		MaxHours int `json:"maxHours"`
	}
	decodeJSON(r, &body)
	if body.JobID == 0 || body.MaxHours < body.MinHours {
		respond(w, http.StatusBadRequest, msg("jobId required and maxHours must be >= minHours"))
		return
	}
	// Manager must stay within their location.
	if !hasRole(currentUser(r).Role, "admin") {
		locID, err := managerLocationID(r.Context(), currentUser(r).ID)
		if err != nil {
			respond500(w, "Assign Student Job Error", err, false)
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
	_, err := db.Exec(r.Context(), `
		INSERT INTO student_jobs (user_id, job_id, min_hours, max_hours, active)
		VALUES ($1, $2, $3, $4, true)
		ON CONFLICT (user_id, job_id) DO UPDATE
		SET min_hours = $3, max_hours = $4, active = true`,
		targetID(r), body.JobID, body.MinHours, body.MaxHours)
	if err != nil {
		respond500(w, "Assign Student Job Error", err, true)
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Student assigned to job"})
}

// Manager removes a student from a job (their qualification for it).
func removeStudentJob(w http.ResponseWriter, r *http.Request) {
	tag, err := db.Exec(r.Context(),
		`DELETE FROM student_jobs WHERE user_id = $1 AND job_id = $2`,
		targetID(r), targetJobID(r))
	if err != nil {
		respond500(w, "Remove Student Job Error", err, false)
		return
	}
	if tag.RowsAffected() == 0 {
		respond(w, http.StatusNotFound, msg("Student is not assigned to this job"))
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Student removed from job"})
}

// Manager/scheduler sets a worker's classification (student/fulltime/hourly)
// and, for hourly staff, their regular weekly-hour limit (under 40).
func setWorkerDetails(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkerType  string `json:"workerType"`
		HourlyLimit *int   `json:"hourlyLimit"`
	}
	decodeJSON(r, &body)
	switch body.WorkerType {
	case "student", "fulltime", "hourly":
	default:
		respond(w, http.StatusBadRequest, msg("workerType must be student, fulltime, or hourly"))
		return
	}
	var limit any
	if body.WorkerType == "hourly" && body.HourlyLimit != nil {
		if *body.HourlyLimit < 1 || *body.HourlyLimit > 40 {
			respond(w, http.StatusBadRequest, msg("hourlyLimit must be between 1 and 40"))
			return
		}
		limit = *body.HourlyLimit
	}
	if _, err := db.Exec(r.Context(),
		`UPDATE users SET worker_type = $1, hourly_limit = $2 WHERE id = $3`,
		body.WorkerType, limit, targetID(r)); err != nil {
		respond500(w, "Set Worker Details Error", err, false)
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Worker details updated"})
}

// Manager sets a weekly template for a student's job; also generates the
// current week's assigned shift so it shows up on the student's calendar.
func addWeeklySchedule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		JobID     int    `json:"jobId"`
		DayOfWeek int    `json:"dayOfWeek"`
		StartTime string `json:"startTime"`
		EndTime   string `json:"endTime"`
	}
	decodeJSON(r, &body)
	start, end := parseTime(body.StartTime), parseTime(body.EndTime)
	if body.JobID == 0 || body.DayOfWeek < 0 || body.DayOfWeek > 6 || start == "" || end == "" || hoursBetween(start, end) <= 0 {
		respond(w, http.StatusBadRequest, msg("jobId, dayOfWeek (0-6), startTime, endTime required"))
		return
	}
	studentID := targetID(r)
	// The student must hold this job.
	var ok bool
	if err := db.QueryRow(r.Context(),
		`SELECT EXISTS (SELECT 1 FROM student_jobs WHERE user_id = $1 AND job_id = $2 AND active)`,
		studentID, body.JobID).Scan(&ok); err != nil || !ok {
		respond(w, http.StatusBadRequest, msg("Student is not assigned to this job"))
		return
	}
	var deptID int
	if err := db.QueryRow(r.Context(),
		`SELECT department_id FROM jobs WHERE id = $1`, body.JobID).Scan(&deptID); err != nil {
		respond500(w, "Add Schedule Error", err, false)
		return
	}
	// Don't schedule a worker past their hard weekly cap (students 20/wk, staff 60/wk).
	date := weekDate(weekMondayOf(time.Now()), body.DayOfWeek)
	allowance, err := allowanceFor(r.Context(), studentID, hoursBetween(start, end), date)
	if err != nil {
		respond500(w, "Add Schedule Error", err, false)
		return
	}
	if allowance == "blocked" {
		respond(w, http.StatusBadRequest, msg("This schedule would exceed the worker's weekly hour limit"))
		return
	}
	if _, err := db.Exec(r.Context(), `
		INSERT INTO weekly_schedules (user_id, day_of_week, start_time, end_time)
		VALUES ($1, $2, $3, $4)`, studentID, body.DayOfWeek, start, end); err != nil {
		respond500(w, "Add Schedule Error", err, true)
		return
	}
	if _, err := db.Exec(r.Context(), `
			INSERT INTO workqueue (department_id, date, start_time, end_time, status, assigned_user_id)
			SELECT $1, $2, $3, $4, 'assigned', $5
			WHERE NOT EXISTS (
				SELECT 1 FROM workqueue WHERE assigned_user_id = $5 AND date = $2 AND start_time = $3)`,
		deptID, date, start, end, studentID); err != nil {
		respond500(w, "Add Schedule Error", err, true)
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Weekly schedule added"})
}

// Manager drops an open shift into the workqueue for students to pick.
func createWorkqueueShift(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DepartmentID int    `json:"departmentId"`
		Date         string `json:"date"`
		StartTime    string `json:"startTime"`
		EndTime      string `json:"endTime"`
	}
	decodeJSON(r, &body)
	start, end := parseTime(body.StartTime), parseTime(body.EndTime)
	date, err := time.Parse("2006-01-02", body.Date)
	if err != nil || start == "" || end == "" || hoursBetween(start, end) <= 0 {
		respond(w, http.StatusBadRequest, msg("date, startTime, endTime required"))
		return
	}
	if !hasRole(currentUser(r).Role, "admin") {
		if currentUser(r).Role == "scheduler" {
			deptID, err := schedulerDepartmentID(r.Context(), currentUser(r).ID)
			if err != nil {
				respond500(w, "Create Shift Error", err, false)
				return
			}
			if body.DepartmentID != deptID {
				respond(w, http.StatusForbidden, msg("Department not in your department"))
				return
			}
		} else {
			locID, err := managerLocationID(r.Context(), currentUser(r).ID)
			if err != nil {
				respond500(w, "Create Shift Error", err, false)
				return
			}
			var ok bool
			if err := db.QueryRow(r.Context(),
				`SELECT EXISTS (SELECT 1 FROM departments WHERE id = $1 AND location_id = $2)`,
				body.DepartmentID, locID).Scan(&ok); err != nil || !ok {
				respond(w, http.StatusForbidden, msg("Department not in your location"))
				return
			}
		}
	}
	if _, err := db.Exec(r.Context(), `
		INSERT INTO workqueue (department_id, date, start_time, end_time, status)
		VALUES ($1, $2, $3, $4, 'open')`,
		body.DepartmentID, date, start, end); err != nil {
		respond500(w, "Create Shift Error", err, true)
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Shift added to workqueue"})
}

// ---- staff: view & assign workqueue shifts ----

// All workqueue shifts in the staff member's location (open and assigned),
// with the assigned worker, so schedulers can assign shifts to workers.
func staffWorkqueue(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	query := `
		SELECT w.id, d.name, w.date, w.start_time, w.end_time, w.status,
		       COALESCE(w.assigned_user_id, 0), COALESCE(u.email, '')
		FROM workqueue w
		JOIN departments d ON d.id = w.department_id
		LEFT JOIN users u ON u.id = w.assigned_user_id`
	args := []any{}
	if !hasRole(u.Role, "admin") {
		if u.Role == "scheduler" {
			deptID, err := schedulerDepartmentID(r.Context(), u.ID)
			if err != nil {
				respond500(w, "List Workqueue Error", err, false)
				return
			}
			query += ` WHERE w.department_id = $1`
			args = append(args, deptID)
		} else {
			locID, err := managerLocationID(r.Context(), u.ID)
			if err != nil {
				respond500(w, "List Workqueue Error", err, false)
				return
			}
			query += ` WHERE d.location_id = $1`
			args = append(args, locID)
		}
	}
	query += ` ORDER BY w.date, w.start_time`
	rows, err := db.Query(r.Context(), query, args...)
	if err != nil {
		respond500(w, "List Workqueue Error", err, false)
		return
	}
	defer rows.Close()
	shifts := []map[string]any{}
	for rows.Next() {
		var id, assignedID int
		var dept, status, assignedEmail string
		var date time.Time
		var start, end string
		if err := rows.Scan(&id, &dept, &date, &start, &end, &status, &assignedID, &assignedEmail); err != nil {
			respond500(w, "List Workqueue Error", err, false)
			return
		}
		shifts = append(shifts, map[string]any{
			"id": id, "departmentName": dept, "date": date.Format("2006-01-02"),
			"startTime": start[:5], "endTime": end[:5], "status": status,
			"assignedUserId": assignedID, "assignedEmail": assignedEmail,
		})
	}
	respond(w, http.StatusOK, map[string]any{"shifts": shifts})
}

// Scheduler assigns an existing workqueue shift to a worker (or unassigns it
// back to open by omitting userId / sending 0).
func assignWorkqueueShift(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var body struct {
		UserID int `json:"userId"`
	}
	decodeJSON(r, &body)
	shiftID := targetID(r)

	// The shift must be in the caller's scope (non-admin).
	if !hasRole(u.Role, "admin") {
		if u.Role == "scheduler" {
			deptID, err := schedulerDepartmentID(r.Context(), u.ID)
			if err != nil {
				respond500(w, "Assign Shift Error", err, false)
				return
			}
			var ok bool
			if err := db.QueryRow(r.Context(),
				`SELECT EXISTS (SELECT 1 FROM workqueue WHERE id = $1 AND department_id = $2)`,
				shiftID, deptID).Scan(&ok); err != nil {
				respond500(w, "Assign Shift Error", err, false)
				return
			}
			if !ok {
				respond(w, http.StatusForbidden, msg("Shift not in your department"))
				return
			}
		} else {
			locID, err := managerLocationID(r.Context(), u.ID)
			if err != nil {
				respond500(w, "Assign Shift Error", err, false)
				return
			}
			var ok bool
			if err := db.QueryRow(r.Context(),
				`SELECT EXISTS (SELECT 1 FROM workqueue w JOIN departments d ON d.id = w.department_id WHERE w.id = $1 AND d.location_id = $2)`,
				shiftID, locID).Scan(&ok); err != nil {
				respond500(w, "Assign Shift Error", err, false)
				return
			}
			if !ok {
				respond(w, http.StatusForbidden, msg("Shift not in your location"))
				return
			}
		}
	}

	if body.UserID <= 0 {
		tag, err := db.Exec(r.Context(),
			`UPDATE workqueue SET status = 'open', assigned_user_id = NULL WHERE id = $1`, shiftID)
		if err != nil {
			respond500(w, "Assign Shift Error", err, false)
			return
		}
		if tag.RowsAffected() == 0 {
			respond(w, http.StatusNotFound, msg("Shift not found"))
			return
		}
		respond(w, http.StatusOK, map[string]any{"success": true, "message": "Shift unassigned"})
		return
	}

	// Target must be a real worker (student) — and, for non-admins, one with a
	// job in the caller's scope.
	if !hasRole(u.Role, "admin") {
		if u.Role == "scheduler" {
			deptID, err := schedulerDepartmentID(r.Context(), u.ID)
			if err != nil {
				respond500(w, "Assign Shift Error", err, false)
				return
			}
			var ok bool
			if err := db.QueryRow(r.Context(), `
				SELECT EXISTS (
					SELECT 1 FROM student_jobs sj
					JOIN jobs j ON j.id = sj.job_id
					WHERE sj.user_id = $1 AND sj.active AND j.department_id = $2)`, body.UserID, deptID).Scan(&ok); err != nil {
				respond500(w, "Assign Shift Error", err, false)
				return
			}
			if !ok {
				respond(w, http.StatusForbidden, msg("Worker does not work in your department"))
				return
			}
		} else {
			locID, err := managerLocationID(r.Context(), u.ID)
			if err != nil {
				respond500(w, "Assign Shift Error", err, false)
				return
			}
			var ok bool
			if err := db.QueryRow(r.Context(), `
				SELECT EXISTS (
					SELECT 1 FROM student_jobs sj
					JOIN jobs j ON j.id = sj.job_id
					JOIN departments d ON d.id = j.department_id
					WHERE sj.user_id = $1 AND sj.active AND d.location_id = $2)`, body.UserID, locID).Scan(&ok); err != nil {
				respond500(w, "Assign Shift Error", err, false)
				return
			}
			if !ok {
				respond(w, http.StatusForbidden, msg("Worker does not work in your location"))
				return
			}
		}
	} else {
		var ok bool
		if err := db.QueryRow(r.Context(),
			`SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND role IN ('student','staff'))`, body.UserID).Scan(&ok); err != nil {
			respond500(w, "Assign Shift Error", err, false)
			return
		}
		if !ok {
			respond(w, http.StatusBadRequest, msg("Worker not found"))
			return
		}
	}

	// Enforce the worker's weekly-hour limits (students cap at 20/wk; staff
	// overtime past their regular hours needs manager approval).
	var date time.Time
	var start, end string
	if err := db.QueryRow(r.Context(),
		`SELECT date, start_time, end_time FROM workqueue WHERE id = $1`, shiftID).Scan(&date, &start, &end); err != nil {
		respond500(w, "Assign Shift Error", err, false)
		return
	}
	allowance, err := allowanceFor(r.Context(), body.UserID, hoursBetween(start, end), date)
	if err != nil {
		respond500(w, "Assign Shift Error", err, false)
		return
	}
	switch allowance {
	case "blocked":
		respond(w, http.StatusBadRequest, msg("Assignment exceeds the worker's weekly hour limit"))
		return
	case "pending":
		if err := createOverflowRequest(r.Context(), body.UserID, shiftID, "Exceeds regular hours (overtime)"); err != nil {
			respond500(w, "Assign Shift Error", err, true)
			return
		}
		respond(w, http.StatusAccepted, map[string]any{
			"success": true, "assigned": false,
			"message": "Shift would put worker on overtime — pending manager approval",
		})
		return
	}

	tag, err := db.Exec(r.Context(),
		`UPDATE workqueue SET status = 'assigned', assigned_user_id = $2 WHERE id = $1`,
		shiftID, body.UserID)
	if err != nil {
		respond500(w, "Assign Shift Error", err, false)
		return
	}
	if tag.RowsAffected() == 0 {
		respond(w, http.StatusNotFound, msg("Shift not found"))
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Shift assigned"})
}

// ---- manager: requests ----

func listRequests(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	query := `
		SELECT r.id, r.user_id, u.email, r.workqueue_id, w.date, w.start_time, w.end_time,
		       r.type, r.status, r.reason
		FROM schedule_requests r
		JOIN workqueue w ON w.id = r.workqueue_id
		JOIN departments d ON d.id = w.department_id
		JOIN users u ON u.id = r.user_id
		WHERE r.status = 'pending'`
	args := []any{}
	if !hasRole(u.Role, "admin") {
		if u.Role == "scheduler" {
			deptID, err := schedulerDepartmentID(r.Context(), u.ID)
			if err != nil {
				respond500(w, "List Requests Error", err, false)
				return
			}
			query += ` AND w.department_id = $1`
			args = append(args, deptID)
		} else {
			locID, err := managerLocationID(r.Context(), u.ID)
			if err != nil {
				respond500(w, "List Requests Error", err, false)
				return
			}
			query += ` AND d.location_id = $1`
			args = append(args, locID)
		}
	}
	query += ` ORDER BY r.created_at`
	rows, err := db.Query(r.Context(), query, args...)
	if err != nil {
		respond500(w, "List Requests Error", err, false)
		return
	}
	defer rows.Close()
	requests := []map[string]any{}
	for rows.Next() {
		var id, userID, wqID int
		var email, typ, status string
		var date time.Time
		var start, end string
		var reason *string
		if err := rows.Scan(&id, &userID, &email, &wqID, &date, &start, &end, &typ, &status, &reason); err != nil {
			respond500(w, "List Requests Error", err, false)
			return
		}
		requests = append(requests, map[string]any{
			"id": id, "userId": userID, "email": email, "workqueueId": wqID,
			"date": date.Format("2006-01-02"), "startTime": start, "endTime": end,
			"type": typ, "status": status, "reason": reason,
		})
	}
	respond(w, http.StatusOK, map[string]any{"requests": requests})
}

func approveRequest(w http.ResponseWriter, r *http.Request) {
	var reqID, userID, wqID int
	var typ string
	err := db.QueryRow(r.Context(),
		`SELECT id, user_id, workqueue_id, type FROM schedule_requests WHERE id = $1 AND status = 'pending'`,
		targetID(r)).Scan(&reqID, &userID, &wqID, &typ)
	if errors.Is(err, pgx.ErrNoRows) {
		respond(w, http.StatusNotFound, msg("Pending request not found"))
		return
	}
	if err != nil {
		respond500(w, "Approve Request Error", err, false)
		return
	}
	if !requestInCallerLocation(w, r, targetID(r)) {
		return
	}
	// miss: return the shift to the workqueue. overflow: assign it to the student.
	if typ == "miss" {
		_, err = db.Exec(r.Context(),
			`UPDATE workqueue SET status = 'open', assigned_user_id = NULL WHERE id = $1`, wqID)
	} else {
		// Re-check the worker's weekly cap at approval time: the worker's
		// schedule may have filled up since the request was created.
		var date time.Time
		var start, end string
		if err := db.QueryRow(r.Context(),
			`SELECT date, start_time, end_time FROM workqueue WHERE id = $1`, wqID).Scan(&date, &start, &end); err != nil {
			respond500(w, "Approve Request Error", err, false)
			return
		}
		allowance, err := allowanceFor(r.Context(), userID, hoursBetween(start, end), date)
		if err != nil {
			respond500(w, "Approve Request Error", err, false)
			return
		}
		if allowance == "blocked" {
			if _, err := db.Exec(r.Context(),
				`UPDATE schedule_requests SET status = 'denied' WHERE id = $1`, reqID); err != nil {
				respond500(w, "Approve Request Error", err, false)
				return
			}
			respond(w, http.StatusBadRequest, msg("Approval would exceed the worker's weekly hour limit"))
			return
		}
		// Only assign if the shift is still open — someone else may have picked
		// it while this request sat pending.
		tag, execErr := db.Exec(r.Context(),
			`UPDATE workqueue SET status = 'assigned', assigned_user_id = $1 WHERE id = $2 AND status = 'open'`,
			userID, wqID)
		if execErr != nil {
			respond500(w, "Approve Request Error", execErr, false)
			return
		}
		if tag.RowsAffected() == 0 {
			if _, err := db.Exec(r.Context(),
				`UPDATE schedule_requests SET status = 'denied' WHERE id = $1`, reqID); err != nil {
				respond500(w, "Approve Request Error", err, false)
				return
			}
			respond(w, http.StatusConflict, msg("Shift was already taken"))
			return
		}
	}
	if err != nil {
		respond500(w, "Approve Request Error", err, false)
		return
	}
	if _, err := db.Exec(r.Context(),
		`UPDATE schedule_requests SET status = 'approved' WHERE id = $1`, reqID); err != nil {
		respond500(w, "Approve Request Error", err, false)
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Request approved"})
}

func denyRequest(w http.ResponseWriter, r *http.Request) {
	if !requestInCallerLocation(w, r, targetID(r)) {
		return
	}
	tag, err := db.Exec(r.Context(),
		`UPDATE schedule_requests SET status = 'denied' WHERE id = $1 AND status = 'pending'`, targetID(r))
	if err != nil {
		respond500(w, "Deny Request Error", err, false)
		return
	}
	if tag.RowsAffected() == 0 {
		respond(w, http.StatusNotFound, msg("Pending request not found"))
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Request denied"})
}

// ---- student: calendar, workqueue, pick, requests, preferences ----

func myCalendar(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	monday := weekMondayOf(time.Now())
	rows, err := db.Query(r.Context(), `
		SELECT w.id, w.date, w.start_time, w.end_time, d.name
		FROM workqueue w JOIN departments d ON d.id = w.department_id
		WHERE w.assigned_user_id = $1 AND w.date >= $2 AND w.date < $3
		ORDER BY w.date, w.start_time`,
		u.ID, monday, monday.AddDate(0, 0, 7))
	if err != nil {
		respond500(w, "Calendar Error", err, false)
		return
	}
	defer rows.Close()
	calendar := []map[string]any{}
	for rows.Next() {
		var id int
		var date time.Time
		var start, end, dept string
		if err := rows.Scan(&id, &date, &start, &end, &dept); err != nil {
			respond500(w, "Calendar Error", err, false)
			return
		}
		calendar = append(calendar, map[string]any{
			"id": id, "date": date.Format("2006-01-02"), "startTime": start, "endTime": end, "departmentName": dept,
		})
	}
	respond(w, http.StatusOK, map[string]any{"calendar": calendar})
}

// Manager-scoped: a worker's assigned shifts for the current week.
func workerCalendar(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	// The worker must be in the caller's location (non-admin).
	if !hasRole(u.Role, "admin") {
		locID, err := managerLocationID(r.Context(), u.ID)
		if err != nil {
			respond500(w, "Worker Calendar Error", err, false)
			return
		}
		var ok bool
		if err := db.QueryRow(r.Context(), `
			SELECT EXISTS (
				SELECT 1 FROM student_jobs sj
				JOIN jobs j ON j.id = sj.job_id
				JOIN departments d ON d.id = j.department_id
				WHERE sj.user_id = $1 AND d.location_id = $2)`, targetID(r), locID).Scan(&ok); err != nil || !ok {
			respond(w, http.StatusForbidden, msg("Worker not in your location"))
			return
		}
	}
	monday := weekMondayOf(time.Now())
	rows, err := db.Query(r.Context(), `
		SELECT w.id, w.date, w.start_time, w.end_time, d.name
		FROM workqueue w JOIN departments d ON d.id = w.department_id
		WHERE w.assigned_user_id = $1 AND w.date >= $2 AND w.date < $3
		ORDER BY w.date, w.start_time`,
		targetID(r), monday, monday.AddDate(0, 0, 7))
	if err != nil {
		respond500(w, "Worker Calendar Error", err, false)
		return
	}
	defer rows.Close()
	calendar := []map[string]any{}
	for rows.Next() {
		var id int
		var date time.Time
		var start, end, dept string
		if err := rows.Scan(&id, &date, &start, &end, &dept); err != nil {
			respond500(w, "Worker Calendar Error", err, false)
			return
		}
		calendar = append(calendar, map[string]any{
			"id": id, "date": date.Format("2006-01-02"), "startTime": start, "endTime": end, "departmentName": dept,
		})
	}
	respond(w, http.StatusOK, map[string]any{"calendar": calendar})
}

// Manager-scoped: a worker's preferred days/times.
func workerPreferences(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if !hasRole(u.Role, "admin") {
		locID, err := managerLocationID(r.Context(), u.ID)
		if err != nil {
			respond500(w, "Worker Preferences Error", err, false)
			return
		}
		var ok bool
		if err := db.QueryRow(r.Context(), `
			SELECT EXISTS (
				SELECT 1 FROM student_jobs sj
				JOIN jobs j ON j.id = sj.job_id
				JOIN departments d ON d.id = j.department_id
				WHERE sj.user_id = $1 AND d.location_id = $2)`, targetID(r), locID).Scan(&ok); err != nil || !ok {
			respond(w, http.StatusForbidden, msg("Worker not in your location"))
			return
		}
	}
	rows, err := db.Query(r.Context(), `
		SELECT day_of_week, start_time, end_time FROM preferred_times
		WHERE user_id = $1 ORDER BY day_of_week, start_time`, targetID(r))
	if err != nil {
		respond500(w, "Worker Preferences Error", err, false)
		return
	}
	defer rows.Close()
	prefs := []map[string]any{}
	for rows.Next() {
		var dow int
		var start, end string
		if err := rows.Scan(&dow, &start, &end); err != nil {
			respond500(w, "Worker Preferences Error", err, false)
			return
		}
		prefs = append(prefs, map[string]any{"dayOfWeek": dow, "startTime": start, "endTime": end})
	}
	respond(w, http.StatusOK, map[string]any{"preferences": prefs})
}

func myWorkqueue(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	deptIDs, err := studentDepartmentIDs(r.Context(), u.ID)
	if err != nil {
		respond500(w, "Workqueue Error", err, false)
		return
	}
	if len(deptIDs) == 0 {
		respond(w, http.StatusOK, map[string]any{"workqueue": []map[string]any{}})
		return
	}
	// Only surface open shifts if the worker still has weekly hours left.
	wt, hl, err := workerSettings(r.Context(), u.ID)
	if err != nil {
		respond500(w, "Workqueue Error", err, false)
		return
	}
	used, err := workerWeekHours(r.Context(), u.ID, time.Now())
	if err != nil {
		respond500(w, "Workqueue Error", err, false)
		return
	}
	if used >= workerPolicyFor(wt, hl).cap {
		respond(w, http.StatusOK, map[string]any{"workqueue": []map[string]any{}})
		return
	}
	rows, err := db.Query(r.Context(), `
		SELECT w.id, w.date, w.start_time, w.end_time, d.name
		FROM workqueue w JOIN departments d ON d.id = w.department_id
		WHERE w.status = 'open' AND w.department_id = ANY($1)
		ORDER BY w.date, w.start_time`, deptIDs)
	if err != nil {
		respond500(w, "Workqueue Error", err, false)
		return
	}
	defer rows.Close()
	remaining := workerPolicyFor(wt, hl).cap - used
	workqueue := []map[string]any{}
	for rows.Next() {
		var id int
		var date time.Time
		var start, end, dept string
		if err := rows.Scan(&id, &date, &start, &end, &dept); err != nil {
			respond500(w, "Workqueue Error", err, false)
			return
		}
		// Only show shifts the worker can actually take without exceeding their cap.
		if hoursBetween(start, end) > remaining {
			continue
		}
		workqueue = append(workqueue, map[string]any{
			"id": id, "date": date.Format("2006-01-02"), "startTime": start, "endTime": end, "departmentName": dept,
		})
	}
	respond(w, http.StatusOK, map[string]any{"workqueue": workqueue})
}

// Records an overtime/overflow request that a manager must approve for the
// worker to actually be assigned that shift.
func createOverflowRequest(ctx context.Context, userID, shiftID int, reason string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO schedule_requests (user_id, workqueue_id, type, status, reason)
		VALUES ($1, $2, 'overflow', 'pending', $3)`, userID, shiftID, reason)
	return err
}

func pickShift(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	deptIDs, err := studentDepartmentIDs(r.Context(), u.ID)
	if err != nil {
		respond500(w, "Pick Shift Error", err, false)
		return
	}
	if len(deptIDs) == 0 {
		respond(w, http.StatusBadRequest, msg("You have no job assignment"))
		return
	}
	var wqDept int
	var date time.Time
	var start, end string
	var status string
	err = db.QueryRow(r.Context(),
		`SELECT department_id, date, start_time, end_time, status FROM workqueue WHERE id = $1`,
		targetID(r)).Scan(&wqDept, &date, &start, &end, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		respond(w, http.StatusNotFound, msg("Shift not found"))
		return
	}
	if err != nil {
		respond500(w, "Pick Shift Error", err, false)
		return
	}
	if status != "open" {
		respond(w, http.StatusBadRequest, msg("Shift is no longer available"))
		return
	}
	inDept := false
	for _, id := range deptIDs {
		if id == wqDept {
			inDept = true
			break
		}
	}
	if !inDept {
		respond(w, http.StatusForbidden, msg("Shift is not in one of your jobs"))
		return
	}
	// Current assigned hours this week + this shift's hours are evaluated by
	// allowanceFor (using the worker's type-specific weekly limits).
	shiftHours := hoursBetween(start, end)
	// Enforce the worker's weekly-hour policy (students cap at 20/wk; staff
	// overtime past their regular hours still needs manager approval).
	allowance, err := allowanceFor(r.Context(), u.ID, shiftHours, date)
	if err != nil {
		respond500(w, "Pick Shift Error", err, false)
		return
	}
	if allowance == "blocked" {
		respond(w, http.StatusBadRequest, msg("This shift would exceed your weekly hour limit"))
		return
	}
	if allowance == "pending" {
		if err := createOverflowRequest(r.Context(), u.ID, targetID(r), "Exceeds regular hours (overtime)"); err != nil {
			respond500(w, "Pick Shift Error", err, true)
			return
		}
		respond(w, http.StatusOK, map[string]any{
			"success": true, "assigned": false,
			"message": "Shift exceeds your regular hours — request sent to manager",
		})
		return
	}
	tag, err := db.Exec(r.Context(),
		`UPDATE workqueue SET status = 'assigned', assigned_user_id = $1 WHERE id = $2 AND status = 'open'`,
		u.ID, targetID(r))
	if err != nil {
		respond500(w, "Pick Shift Error", err, false)
		return
	}
	if tag.RowsAffected() == 0 {
		respond(w, http.StatusBadRequest, msg("Shift is no longer available"))
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "assigned": true, "message": "Shift assigned"})
}

// A student's own schedule requests, newest first.
func myRequests(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	rows, err := db.Query(r.Context(), `
		SELECT r.id, r.workqueue_id, w.date, w.start_time, w.end_time, r.type, r.status, r.reason
		FROM schedule_requests r
		JOIN workqueue w ON w.id = r.workqueue_id
		WHERE r.user_id = $1
		ORDER BY r.created_at DESC`, u.ID)
	if err != nil {
		respond500(w, "My Requests Error", err, false)
		return
	}
	defer rows.Close()
	requests := []map[string]any{}
	for rows.Next() {
		var id, wqID int
		var date time.Time
		var start, end, typ, status string
		var reason *string
		if err := rows.Scan(&id, &wqID, &date, &start, &end, &typ, &status, &reason); err != nil {
			respond500(w, "My Requests Error", err, false)
			return
		}
		requests = append(requests, map[string]any{
			"id": id, "workqueueId": wqID, "date": date.Format("2006-01-02"),
			"startTime": start, "endTime": end, "type": typ, "status": status, "reason": reason,
		})
	}
	respond(w, http.StatusOK, map[string]any{"requests": requests})
}

// A worker cancels their own pending request.
func cancelRequest(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	tag, err := db.Exec(r.Context(),
		`UPDATE schedule_requests SET status = 'cancelled' WHERE id = $1 AND user_id = $2 AND status = 'pending'`,
		targetID(r), u.ID)
	if err != nil {
		respond500(w, "Cancel Request Error", err, false)
		return
	}
	if tag.RowsAffected() == 0 {
		respond(w, http.StatusNotFound, msg("Pending request not found"))
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Request cancelled"})
}

func createRequest(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var body struct {
		WorkqueueID int    `json:"workqueueId"`
		Type        string `json:"type"`
		Reason      string `json:"reason"`
	}
	decodeJSON(r, &body)
	if body.WorkqueueID == 0 || (body.Type != "miss" && body.Type != "overflow") {
		respond(w, http.StatusBadRequest, msg("workqueueId and type (miss|overflow) required"))
		return
	}
	// miss: the shift must be currently assigned to this student.
	if body.Type == "miss" {
		var ok bool
		if err := db.QueryRow(r.Context(),
			`SELECT EXISTS (SELECT 1 FROM workqueue WHERE id = $1 AND assigned_user_id = $2)`,
			body.WorkqueueID, u.ID).Scan(&ok); err != nil || !ok {
			respond(w, http.StatusBadRequest, msg("Shift is not assigned to you"))
			return
		}
	}
	if _, err := db.Exec(r.Context(), `
		INSERT INTO schedule_requests (user_id, workqueue_id, type, status, reason)
		VALUES ($1, $2, $3, 'pending', $4)`,
		u.ID, body.WorkqueueID, body.Type, body.Reason); err != nil {
		respond500(w, "Create Request Error", err, true)
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Request submitted"})
}

// A student's preferred days/times.
func myPreferences(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	rows, err := db.Query(r.Context(), `
		SELECT day_of_week, start_time, end_time FROM preferred_times
		WHERE user_id = $1 ORDER BY day_of_week, start_time`, u.ID)
	if err != nil {
		respond500(w, "My Preferences Error", err, false)
		return
	}
	defer rows.Close()
	prefs := []map[string]any{}
	for rows.Next() {
		var dow int
		var start, end string
		if err := rows.Scan(&dow, &start, &end); err != nil {
			respond500(w, "My Preferences Error", err, false)
			return
		}
		prefs = append(prefs, map[string]any{"dayOfWeek": dow, "startTime": start, "endTime": end})
	}
	respond(w, http.StatusOK, map[string]any{"preferences": prefs})
}

func addPreference(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DayOfWeek int    `json:"dayOfWeek"`
		StartTime string `json:"startTime"`
		EndTime   string `json:"endTime"`
	}
	decodeJSON(r, &body)
	start, end := parseTime(body.StartTime), parseTime(body.EndTime)
	if body.DayOfWeek < 0 || body.DayOfWeek > 6 || start == "" || end == "" || hoursBetween(start, end) <= 0 {
		respond(w, http.StatusBadRequest, msg("dayOfWeek (0-6), startTime, endTime required"))
		return
	}
	if _, err := db.Exec(r.Context(), `
		INSERT INTO preferred_times (user_id, day_of_week, start_time, end_time)
		VALUES ($1, $2, $3, $4)`, currentUser(r).ID, body.DayOfWeek, start, end); err != nil {
		respond500(w, "Add Preference Error", err, true)
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Preference saved"})
}
