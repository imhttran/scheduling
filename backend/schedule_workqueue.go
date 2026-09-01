package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
	"time"
)

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
		UserID    int     `json:"userId"`
		Hours     float64 `json:"hours"`
		StartTime string  `json:"startTime"`
		Blocks    []struct {
			StartTime string `json:"startTime"`
			EndTime   string `json:"endTime"`
		} `json:"blocks"`
	}
	decodeJSON(r, &body)
	shiftID := targetID(r)

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
	// overtime past their regular hours needs manager approval). Hourly workers
	// can be assigned one or more 2-hour blocks of the shift; the rest of the
	// slot stays open.
	var date time.Time
	var start, end string
	if err := db.QueryRow(r.Context(),
		`SELECT date, start_time, end_time FROM workqueue WHERE id = $1`, shiftID).Scan(&date, &start, &end); err != nil {
		respond500(w, "Assign Shift Error", err, false)
		return
	}

	if len(body.Blocks) > 0 {
		var parentID *int
		if err := db.QueryRow(r.Context(),
			`SELECT parent_shift_id FROM workqueue WHERE id = $1`, shiftID).Scan(&parentID); err != nil {
			respond500(w, "Assign Shift Error", err, false)
			return
		}
		groupID := shiftID
		if parentID != nil {
			groupID = *parentID
		}
		var totalHours float64
		for _, b := range body.Blocks {
			if b.StartTime == "" || b.EndTime == "" {
				respond(w, http.StatusBadRequest, msg("Each block needs startTime and endTime"))
				return
			}
			h := hoursBetween(b.StartTime, b.EndTime)
			if h <= 0 {
				respond(w, http.StatusBadRequest, msg("Block must be a positive length"))
				return
			}
			totalHours += h
		}
		allowance, err := allowanceFor(r.Context(), body.UserID, totalHours, date)
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
		for _, b := range body.Blocks {
			var rowID int
			if err := db.QueryRow(r.Context(), `
				SELECT id FROM workqueue
				WHERE (parent_shift_id = $1 OR id = $2) AND status = 'open'
				  AND start_time <= $3 AND $3 < end_time
				ORDER BY start_time LIMIT 1`,
				groupID, shiftID, b.StartTime).Scan(&rowID); err != nil {
				respond500(w, "Assign Shift Error", err, false)
				return
			}
			if err := assignBlockToRow(r.Context(), rowID, body.UserID, b.StartTime[:5], b.EndTime[:5]); err != nil {
				respond500(w, "Assign Shift Error", err, false)
				return
			}
		}
		respond(w, http.StatusOK, map[string]any{"success": true, "message": "Shift assigned"})
		return
	}

	fullHours := hoursBetween(start, end)
	blockHours := fullHours
	blockStart := start[:5]
	if body.StartTime != "" {
		st := body.StartTime[:5]
		if hoursBetween(start[:5], st) >= 0 && hoursBetween(st, end[:5]) > 0 {
			blockStart = st
		}
	}
	if body.Hours > 0 && body.Hours < fullHours {
		blockHours = body.Hours
	}
	blockEnd := addHours(blockStart, blockHours)
	if hoursBetween(blockStart, blockEnd) <= 0 || hoursBetween(blockEnd, end[:5]) < 0 {
		respond(w, http.StatusBadRequest, msg("Block must fit within the shift"))
		return
	}
	allowance, err := allowanceFor(r.Context(), body.UserID, blockHours, date)
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

	if err := assignBlockToRow(r.Context(), shiftID, body.UserID, blockStart, blockEnd); err != nil {
		respond500(w, "Assign Shift Error", err, false)
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Shift assigned"})
}

// assignBlockToRow assigns [blockStart, blockEnd] of the given open workqueue
// row to userID, leaving the uncovered parts of the slot open. New rows inherit
// the group's parent_shift_id so the calendar can reconstruct the full shift.
func assignBlockToRow(ctx context.Context, rowID, userID int, blockStart, blockEnd string) error {
	var start, end string
	var parentID *int
	if err := db.QueryRow(ctx,
		`SELECT start_time, end_time, parent_shift_id FROM workqueue WHERE id = $1`, rowID).Scan(&start, &end, &parentID); err != nil {
		return err
	}
	groupID := rowID
	if parentID != nil {
		groupID = *parentID
	}
	if blockEnd != end[:5] {
		if _, err := db.Exec(ctx, `
			INSERT INTO workqueue (department_id, date, start_time, end_time, status, parent_shift_id)
			SELECT department_id, date, $1, end_time, 'open', $3 FROM workqueue WHERE id = $2`,
			blockEnd, rowID, groupID); err != nil {
			return err
		}
	}
	if blockStart != start[:5] {
		if _, err := db.Exec(ctx, `
			INSERT INTO workqueue (department_id, date, start_time, end_time, status, parent_shift_id)
			SELECT department_id, date, start_time, $1, 'open', $3 FROM workqueue WHERE id = $2`,
			blockStart, rowID, groupID); err != nil {
			return err
		}
	}
	_, err := db.Exec(ctx,
		`UPDATE workqueue SET status = 'assigned', assigned_user_id = $2, start_time = $3, end_time = $4, parent_shift_id = $5 WHERE id = $1`,
		rowID, userID, blockStart, blockEnd, groupID)
	return err
}

func myCalendar(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	anchor := time.Now()
	if d := r.URL.Query().Get("date"); d != "" {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			anchor = t
		}
	}
	monday := weekMondayOf(anchor)
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

// Scheduler/manager coverage view: every workqueue shift in the caller's scope
// for the given week, with the assigned worker's name. Schedulers see their
// department, managers their location, admins everything.
func schedulerCalendar(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	anchor := time.Now()
	if d := r.URL.Query().Get("date"); d != "" {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			anchor = t
		}
	}
	monday := weekMondayOf(anchor)
	query := `
		SELECT w.id, w.date, w.start_time, w.end_time, d.name, w.status,
		       COALESCE(w.assigned_user_id, 0), COALESCE(up.first_name || ' ' || up.last_name, ''),
		       COALESCE(w.parent_shift_id, 0)
		FROM workqueue w
		JOIN departments d ON d.id = w.department_id
		LEFT JOIN user_profiles up ON up.user_id = w.assigned_user_id`
	where := []string{}
	args := []any{}
	if !hasRole(u.Role, "admin") {
		if u.Role == "scheduler" {
			deptID, err := schedulerDepartmentID(r.Context(), u.ID)
			if err != nil {
				respond500(w, "Scheduler Calendar Error", err, false)
				return
			}
			where = append(where, "w.department_id = $1")
			args = append(args, deptID)
		} else {
			locID, err := managerLocationID(r.Context(), u.ID)
			if err != nil {
				respond500(w, "Scheduler Calendar Error", err, false)
				return
			}
			where = append(where, "d.location_id = $1")
			args = append(args, locID)
		}
	}
	where = append(where, fmt.Sprintf("w.date >= $%d", len(args)+1), fmt.Sprintf("w.date < $%d", len(args)+2))
	args = append(args, monday, monday.AddDate(0, 0, 7))
	query += " WHERE " + strings.Join(where, " AND ") + " ORDER BY w.date, w.start_time"

	rows, err := db.Query(r.Context(), query, args...)
	if err != nil {
		respond500(w, "Scheduler Calendar Error", err, false)
		return
	}
	defer rows.Close()
	shifts := []map[string]any{}
	for rows.Next() {
		var id, assignedID, parentID int
		var dept, status, assignedName string
		var date time.Time
		var start, end string
		if err := rows.Scan(&id, &date, &start, &end, &dept, &status, &assignedID, &assignedName, &parentID); err != nil {
			respond500(w, "Scheduler Calendar Error", err, false)
			return
		}
		shifts = append(shifts, map[string]any{
			"id": id, "date": date.Format("2006-01-02"),
			"startTime": start[:5], "endTime": end[:5],
			"departmentName": dept, "status": status,
			"assignedUserId": assignedID, "assignedName": assignedName,
			"parentShiftId": parentID,
		})
	}
	respond(w, http.StatusOK, map[string]any{"shifts": shifts})
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

	shiftHours := hoursBetween(start, end)

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
