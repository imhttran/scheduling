package main

import (
	"context"
	"net/http"
	"time"
)

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
		SELECT j.id, j.name, j.team_id, t.name, t.department_id, d.name, j.optimal_workers,
		       (SELECT COUNT(*) FROM student_jobs sj WHERE sj.job_id = j.id AND sj.active) AS current_workers
		FROM jobs j
		JOIN teams t ON t.id = j.team_id
		JOIN departments d ON d.id = t.department_id`
	args := []any{}
	if !hasRole(u.Role, "admin") {
		teamIDs, err := callerScopeTeamIDs(r.Context(), u.ID)
		if err != nil {
			respond500(w, "List Jobs Error", err, false)
			return
		}
		query += ` WHERE t.id = ANY($1)`
		args = append(args, teamIDs)
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
		var id, teamID, deptID, optimalWorkers, currentWorkers int
		var name, teamName, deptName string
		if err := rows.Scan(&id, &name, &teamID, &teamName, &deptID, &deptName, &optimalWorkers, &currentWorkers); err != nil {
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
			"id": id, "name": name, "teamId": teamID, "teamName": teamName,
			"departmentId": deptID, "departmentName": deptName,
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

// Default operating hours for a new job: weekdays 9am-5pm (8h each, 40h/wk)
// and weekends 10h each (20h/wk). Used when a job is created without explicit
// schedules.
func defaultJobSchedules() []jobScheduleInput {
	return []jobScheduleInput{
		{DayOfWeek: 1, StartTime: "09:00", EndTime: "17:00"},
		{DayOfWeek: 2, StartTime: "09:00", EndTime: "17:00"},
		{DayOfWeek: 3, StartTime: "09:00", EndTime: "17:00"},
		{DayOfWeek: 4, StartTime: "09:00", EndTime: "17:00"},
		{DayOfWeek: 5, StartTime: "09:00", EndTime: "17:00"},
		{DayOfWeek: 6, StartTime: "10:00", EndTime: "20:00"},
		{DayOfWeek: 0, StartTime: "10:00", EndTime: "20:00"},
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
		TeamID         int                `json:"teamId"`
		OptimalWorkers int                `json:"optimalWorkers"`
		Schedules      []jobScheduleInput `json:"schedules"`
		Holidays       []jobHolidayInput  `json:"holidays"`
	}
	decodeJSON(r, &body)
	if body.Name == "" || body.TeamID == 0 {
		respond(w, http.StatusBadRequest, msg("name and teamId are required"))
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
	if !teamInCallerScope(w, r, body.TeamID) {
		return
	}
	var id int
	err := db.QueryRow(r.Context(), `
		INSERT INTO jobs (name, team_id, optimal_workers) VALUES ($1, $2, $3)
		RETURNING id`,
		body.Name, body.TeamID, body.OptimalWorkers).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			respond(w, http.StatusBadRequest, msg("Job already exists in this team"))
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
	respond(w, http.StatusCreated, map[string]any{"success": true, "job": map[string]any{"id": id, "name": body.Name, "teamId": body.TeamID}})
}

func updateJob(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name           string             `json:"name"`
		TeamID         int                `json:"teamId"`
		OptimalWorkers int                `json:"optimalWorkers"`
		Schedules      []jobScheduleInput `json:"schedules"`
		Holidays       []jobHolidayInput  `json:"holidays"`
	}
	decodeJSON(r, &body)
	if body.Name == "" || body.TeamID == 0 {
		respond(w, http.StatusBadRequest, msg("name and teamId are required"))
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
	if !teamInCallerScope(w, r, body.TeamID) {
		return
	}
	tag, err := db.Exec(r.Context(), `
		UPDATE jobs SET name = $1, team_id = $2, optimal_workers = $3 WHERE id = $4`,
		body.Name, body.TeamID, body.OptimalWorkers, targetID(r))
	if err != nil {
		if isUniqueViolation(err) {
			respond(w, http.StatusBadRequest, msg("Job already exists in this team"))
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
