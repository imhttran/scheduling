package main

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Team IDs the worker belongs to via active worker_teams membership.
func workerTeamIDs(ctx context.Context, userID int) ([]int, error) {
	rows, err := db.Query(ctx, `
		SELECT wt.team_id FROM worker_teams wt
		WHERE wt.user_id = $1 AND wt.active`, userID)
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

// Department IDs the caller manages — their department-wide scope. Admins
// bypass scoping and never consult these.
func managerDepartmentIDs(ctx context.Context, userID int) ([]int, error) {
	return scanIDs(db.Query(ctx,
		`SELECT id FROM departments WHERE manager_id = $1`, userID))
}

// Team IDs the caller manages directly — their team scope.
func managerTeamIDs(ctx context.Context, userID int) ([]int, error) {
	return scanIDs(db.Query(ctx,
		`SELECT id FROM teams WHERE manager_id = $1`, userID))
}

// callerScopeTeamIDs is the caller's effective scope: the teams they manage
// directly plus every team under the departments they manage (a manager may
// hold several of each).
func callerScopeTeamIDs(ctx context.Context, userID int) ([]int, error) {
	return scanIDs(db.Query(ctx, `
		SELECT t.id FROM teams t
		WHERE t.manager_id = $1
		   OR t.department_id IN (SELECT id FROM departments WHERE manager_id = $1)`, userID))
}

// scanIDs drains a single-column id query.
func scanIDs(rows pgx.Rows, err error) ([]int, error) {
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

// Writes a 403 (and returns false) unless the team is in the caller's scope —
// one of their managed teams or a team under one of their managed departments.
// Admins are exempt (they manage everything).
func teamInCallerScope(w http.ResponseWriter, r *http.Request, teamID int) bool {
	u := currentUser(r)
	if hasRole(u.Role, "admin") {
		return true
	}
	deptIDs, err := managerDepartmentIDs(r.Context(), u.ID)
	if err != nil {
		respond500(w, "Scope Error", err, false)
		return false
	}
	teamIDs, err := managerTeamIDs(r.Context(), u.ID)
	if err != nil {
		respond500(w, "Scope Error", err, false)
		return false
	}
	var ok bool
	if err := db.QueryRow(r.Context(),
		`SELECT EXISTS (
			SELECT 1 FROM teams t
			WHERE t.id = $1 AND (t.id = ANY($2) OR t.department_id = ANY($3)))`,
		teamID, teamIDs, deptIDs).Scan(&ok); err != nil {
		respond500(w, "Scope Error", err, false)
		return false
	}
	if !ok {
		respond(w, http.StatusForbidden, msg("Team not in your scope"))
		return false
	}
	return true
}

// Writes a 403 (and returns false) unless the job's team is in the caller's
// scope. Admins are exempt (they manage everything).
func jobInCallerScope(w http.ResponseWriter, r *http.Request, jobID int) bool {
	u := currentUser(r)
	if hasRole(u.Role, "admin") {
		return true
	}
	deptIDs, err := managerDepartmentIDs(r.Context(), u.ID)
	if err != nil {
		respond500(w, "Scope Error", err, false)
		return false
	}
	teamIDs, err := managerTeamIDs(r.Context(), u.ID)
	if err != nil {
		respond500(w, "Scope Error", err, false)
		return false
	}
	var ok bool
	if err := db.QueryRow(r.Context(),
		`SELECT EXISTS (
			SELECT 1 FROM jobs j JOIN teams t ON t.id = j.team_id
			WHERE j.id = $1 AND (t.id = ANY($2) OR t.department_id = ANY($3)))`,
		jobID, teamIDs, deptIDs).Scan(&ok); err != nil {
		respond500(w, "Scope Error", err, false)
		return false
	}
	if !ok {
		respond(w, http.StatusForbidden, msg("Job not in your scope"))
		return false
	}
	return true
}

// Writes a 403 (and returns false) unless the workqueue shift is in the
// caller's scope. Admins are exempt (they manage everything).
func shiftInCallerScope(w http.ResponseWriter, r *http.Request, shiftID int) bool {
	u := currentUser(r)
	if hasRole(u.Role, "admin") {
		return true
	}
	teamIDs, err := callerScopeTeamIDs(r.Context(), u.ID)
	if err != nil {
		respond500(w, "Scope Error", err, false)
		return false
	}
	var ok bool
	if err := db.QueryRow(r.Context(),
		`SELECT EXISTS (SELECT 1 FROM workqueue WHERE id = $1 AND team_id = ANY($2))`,
		shiftID, teamIDs).Scan(&ok); err != nil {
		respond500(w, "Scope Error", err, false)
		return false
	}
	if !ok {
		respond(w, http.StatusForbidden, msg("Shift not in your scope"))
		return false
	}
	return true
}

// Writes a 403 (and returns false) unless the worker is a member of a team in
// the caller's scope. Admins are exempt (they manage everything).
func workerInCallerScope(w http.ResponseWriter, r *http.Request, workerID int) bool {
	u := currentUser(r)
	if hasRole(u.Role, "admin") {
		return true
	}
	teamIDs, err := callerScopeTeamIDs(r.Context(), u.ID)
	if err != nil {
		respond500(w, "Scope Error", err, false)
		return false
	}
	var ok bool
	if err := db.QueryRow(r.Context(), `
		SELECT EXISTS (
			SELECT 1 FROM worker_teams wt
			WHERE wt.user_id = $1 AND wt.active AND wt.team_id = ANY($2))`, workerID, teamIDs).Scan(&ok); err != nil {
		respond500(w, "Scope Error", err, false)
		return false
	}
	if !ok {
		respond(w, http.StatusForbidden, msg("Worker does not work in your scope"))
		return false
	}
	return true
}

// Writes a 403 (and returns false) unless the schedule request's shift is in
// the caller's scope. Admins are exempt (they manage everything).
func requestInCallerScope(w http.ResponseWriter, r *http.Request, reqID int) bool {
	u := currentUser(r)
	if hasRole(u.Role, "admin") {
		return true
	}
	teamIDs, err := callerScopeTeamIDs(r.Context(), u.ID)
	if err != nil {
		respond500(w, "Scope Error", err, false)
		return false
	}
	var ok bool
	if err := db.QueryRow(r.Context(), `
		SELECT EXISTS (
			SELECT 1 FROM schedule_requests r
			JOIN workqueue wq ON wq.id = r.workqueue_id
			WHERE r.id = $1 AND wq.team_id = ANY($2))`, reqID, teamIDs).Scan(&ok); err != nil {
		respond500(w, "Scope Error", err, false)
		return false
	}
	if !ok {
		respond(w, http.StatusForbidden, msg("Request not in your scope"))
		return false
	}
	return true
}

// Writes a 403 (and returns false) unless the caller manages at least one
// department — the bar for department-level actions (creating teams, jobs,
// workers), which a team-scoped manager doesn't clear. Admins are exempt.
func callerHasDepartmentScope(ctx context.Context, userID int) (bool, error) {
	ids, err := managerDepartmentIDs(ctx, userID)
	if err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}
