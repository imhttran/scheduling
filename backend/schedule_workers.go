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

// Admin sets a department's manager — the scope of everything they see.
func assignDepartmentManager(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID int `json:"userId"`
	}
	decodeJSON(r, &body)
	if body.UserID == 0 {
		respond(w, http.StatusBadRequest, msg("userId required"))
		return
	}
	tag, err := db.Exec(r.Context(),
		`UPDATE departments SET manager_id = $1 WHERE id = $2`, body.UserID, targetID(r))
	if err != nil {
		respond500(w, "Assign Department Manager Error", err, true)
		return
	}
	if tag.RowsAffected() == 0 {
		respond(w, http.StatusNotFound, msg("Department not found"))
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Manager assigned to department"})
}

// Admin sets a team's manager — the scope of everything they see.
func assignTeamManager(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID int `json:"userId"`
	}
	decodeJSON(r, &body)
	if body.UserID == 0 {
		respond(w, http.StatusBadRequest, msg("userId required"))
		return
	}
	tag, err := db.Exec(r.Context(),
		`UPDATE teams SET manager_id = $1 WHERE id = $2`, body.UserID, targetID(r))
	if err != nil {
		respond500(w, "Assign Team Manager Error", err, true)
		return
	}
	if tag.RowsAffected() == 0 {
		respond(w, http.StatusNotFound, msg("Team not found"))
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Manager assigned to team"})
}

// Workers (students and staff) the caller may see: active members of the
// caller's scope teams (admins see everyone). Job/hours info still comes from
// student_jobs.
func listWorkers(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	query := `
		SELECT u.id, u.email, u.disabled, u.worker_type, COALESCE(u.hourly_limit, 0),
		       COALESCE(up.first_name, ''), COALESCE(up.last_name, ''),
		       sj.job_id, j.name, j.team_id, t.name, t.department_id,
		       sj.min_hours, sj.max_hours, sj.active
		FROM users u
		LEFT JOIN user_profiles up ON up.user_id = u.id
		LEFT JOIN student_jobs sj ON sj.user_id = u.id
		LEFT JOIN jobs j ON j.id = sj.job_id
		LEFT JOIN teams t ON t.id = j.team_id
		WHERE u.role IN ('student','staff')`
	args := []any{}
	if !hasRole(u.Role, "admin") {
		teamIDs, err := callerScopeTeamIDs(r.Context(), u.ID)
		if err != nil {
			respond500(w, "List Workers Error", err, false)
			return
		}
		query += ` AND EXISTS (
			SELECT 1 FROM worker_teams wt
			WHERE wt.user_id = u.id AND wt.active AND wt.team_id = ANY($1))`
		args = append(args, teamIDs)
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
		var jobName, teamName *string
		var teamID, deptID *int
		var minH, maxH *int
		var active *bool
		if err := rows.Scan(&id, &email, &disabled, &workerType, &hourlyLimit, &firstName, &lastName, &jobID, &jobName, &teamID, &teamName, &deptID, &minH, &maxH, &active); err != nil {
			respond500(w, "List Workers Error", err, false)
			return
		}
		row, ok := byID[id]
		if !ok {
			pol := workerPolicyFor(workerType, hourlyLimit)
			used, err := workerWeekHours(r.Context(), id, time.Now())
			if err != nil {
				respond500(w, "List Workers Error", err, false)
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
				"jobId": *jobID, "jobName": *jobName, "teamId": *teamID,
				"teamName": *teamName, "departmentId": *deptID,
				"minHours": *minH, "maxHours": *maxH, "active": *active,
			})
		}
	}
	if err := rows.Err(); err != nil {
		respond500(w, "List Workers Error", err, false)
		return
	}
	workers := make([]map[string]any, 0, len(order))
	for _, id := range order {
		workers = append(workers, byID[id])
	}
	respond(w, http.StatusOK, map[string]any{"workers": workers})
}

func assignWorkerJob(w http.ResponseWriter, r *http.Request) {
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

	if !jobInCallerScope(w, r, body.JobID) {
		return
	}
	_, err := db.Exec(r.Context(), `
		INSERT INTO student_jobs (user_id, job_id, min_hours, max_hours, active)
		VALUES ($1, $2, $3, $4, true)
		ON CONFLICT (user_id, job_id) DO UPDATE
		SET min_hours = $3, max_hours = $4, active = true`,
		targetID(r), body.JobID, body.MinHours, body.MaxHours)
	if err != nil {
		respond500(w, "Assign Worker Job Error", err, true)
		return
	}
	// Assigning a job grants team membership.
	if _, err := db.Exec(r.Context(), `
		INSERT INTO worker_teams (user_id, team_id, active)
		SELECT $1, j.team_id, true FROM jobs j WHERE j.id = $2
		ON CONFLICT (user_id, team_id) DO UPDATE SET active = true`,
		targetID(r), body.JobID); err != nil {
		respond500(w, "Assign Worker Job Error", err, true)
		return
	}
	logAudit(r.Context(), currentUser(r).ID, "job.assigned", "job", body.JobID, jobTeam(r.Context(), body.JobID), map[string]any{
		"workerId": targetID(r), "minHours": body.MinHours, "maxHours": body.MaxHours,
	})
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Worker assigned to job"})
}

// Manager removes a worker from a job (their qualification for it).
func removeWorkerJob(w http.ResponseWriter, r *http.Request) {
	tag, err := db.Exec(r.Context(),
		`DELETE FROM student_jobs WHERE user_id = $1 AND job_id = $2`,
		targetID(r), targetJobID(r))
	if err != nil {
		respond500(w, "Remove Worker Job Error", err, false)
		return
	}
	if tag.RowsAffected() == 0 {
		respond(w, http.StatusNotFound, msg("Worker is not assigned to this job"))
		return
	}
	logAudit(r.Context(), currentUser(r).ID, "job.removed", "job", targetJobID(r), jobTeam(r.Context(), targetJobID(r)), map[string]any{
		"workerId": targetID(r),
	})
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Worker removed from job"})
}

// Manager sets a worker's classification (student/fulltime/hourly)
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
