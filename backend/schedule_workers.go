package main

import (
	"net/http"
	"strings"
	"time"
)

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

func listStudents(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	query := `
		SELECT u.id, u.email, u.disabled, u.worker_type, COALESCE(u.hourly_limit, 0),
		       COALESCE(up.first_name, ''), COALESCE(up.last_name, ''),
		       sj.job_id, j.name, j.department_id, d.name, d.location_id,
		       sj.min_hours, sj.max_hours, sj.active
		FROM users u
		LEFT JOIN user_profiles up ON up.user_id = u.id
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

	byID := map[int]map[string]any{}
	order := []int{}
	for rows.Next() {
		var id, hourlyLimit int
		var email, workerType, firstName, lastName string
		var disabled bool
		var jobID *int
		var jobName, deptName *string
		var deptID, locID *int
		var minH, maxH *int
		var active *bool
		if err := rows.Scan(&id, &email, &disabled, &workerType, &hourlyLimit, &firstName, &lastName, &jobID, &jobName, &deptID, &deptName, &locID, &minH, &maxH, &active); err != nil {
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
				"name":       strings.TrimSpace(firstName + " " + lastName),
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
