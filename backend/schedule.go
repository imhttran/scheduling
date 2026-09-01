package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
	"strings"
	"time"
)

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

// addHours returns t ("HH:MM") plus hours, as "HH:MM" ("24:00" for end-of-day).
func addHours(t string, hours float64) string {
	if t == "24:00" {
		t = "00:00"
	}
	parts := strings.Split(t, ":")
	h, _ := strconv.Atoi(parts[0])
	m, _ := strconv.Atoi(parts[1])
	total := float64(h*60+m) + hours*60
	nh := int(total) / 60
	nm := int(total) % 60
	return fmt.Sprintf("%02d:%02d", nh, nm)
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
