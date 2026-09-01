package main

import (
	"net/http"
)

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
