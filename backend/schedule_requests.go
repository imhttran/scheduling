package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

func listRequests(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	query := `
		SELECT r.id, r.user_id, u.email, r.workqueue_id, w.date, w.start_time, w.end_time,
		       r.type, r.status, r.reason
		FROM schedule_requests r
		JOIN workqueue w ON w.id = r.workqueue_id
		JOIN users u ON u.id = r.user_id
		WHERE r.status = 'pending'`
	args := []any{}
	if !hasRole(u.Role, "admin") {
		teamIDs, err := callerScopeTeamIDs(r.Context(), u.ID)
		if err != nil {
			respond500(w, "List Requests Error", err, false)
			return
		}
		query += ` AND w.team_id = ANY($1)`
		args = append(args, teamIDs)
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
	u := currentUser(r)
	var reqID, userID, wqID, teamID int
	var typ string
	err := db.QueryRow(r.Context(), `
		SELECT r.id, r.user_id, r.workqueue_id, r.type, w.team_id
		FROM schedule_requests r
		JOIN workqueue w ON w.id = r.workqueue_id
		WHERE r.id = $1 AND r.status = 'pending'`,
		targetID(r)).Scan(&reqID, &userID, &wqID, &typ, &teamID)
	if errors.Is(err, pgx.ErrNoRows) {
		respond(w, http.StatusNotFound, msg("Pending request not found"))
		return
	}
	if err != nil {
		respond500(w, "Approve Request Error", err, false)
		return
	}
	if !requestInCallerScope(w, r, targetID(r)) {
		return
	}

	if typ == "miss" {
		if _, err = db.Exec(r.Context(),
			`UPDATE workqueue SET status = 'open', assigned_user_id = NULL WHERE id = $1`, wqID); err != nil {
			respond500(w, "Approve Request Error", err, false)
			return
		}
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
			logAudit(r.Context(), u.ID, "request.denied", "schedule_request", reqID, teamID, map[string]any{
				"type": typ, "workqueueId": wqID, "reason": "approval would exceed the worker's weekly hour limit",
			})
			respond(w, http.StatusBadRequest, msg("Approval would exceed the worker's weekly hour limit"))
			return
		}

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
			logAudit(r.Context(), u.ID, "request.denied", "schedule_request", reqID, teamID, map[string]any{
				"type": typ, "workqueueId": wqID, "reason": "shift was already taken",
			})
			respond(w, http.StatusConflict, msg("Shift was already taken"))
			return
		}
	}
	if _, err := db.Exec(r.Context(),
		`UPDATE schedule_requests SET status = 'approved' WHERE id = $1`, reqID); err != nil {
		respond500(w, "Approve Request Error", err, false)
		return
	}
	logAudit(r.Context(), u.ID, "request.approved", "schedule_request", reqID, teamID, map[string]any{
		"type": typ, "workqueueId": wqID, "workerId": userID,
	})
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Request approved"})
}

func denyRequest(w http.ResponseWriter, r *http.Request) {
	if !requestInCallerScope(w, r, targetID(r)) {
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
	wqID, workerID, teamID, typ := requestInfo(r.Context(), targetID(r))
	logAudit(r.Context(), currentUser(r).ID, "request.denied", "schedule_request", targetID(r), teamID, map[string]any{
		"type": typ, "workqueueId": wqID, "workerId": workerID,
	})
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Request denied"})
}

// A worker's own schedule requests, newest first.
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
	wqID, _, teamID, typ := requestInfo(r.Context(), targetID(r))
	logAudit(r.Context(), u.ID, "request.cancelled", "schedule_request", targetID(r), teamID, map[string]any{
		"type": typ, "workqueueId": wqID,
	})
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

	if body.Type == "miss" {
		var ok bool
		if err := db.QueryRow(r.Context(),
			`SELECT EXISTS (SELECT 1 FROM workqueue WHERE id = $1 AND assigned_user_id = $2)`,
			body.WorkqueueID, u.ID).Scan(&ok); err != nil || !ok {
			respond(w, http.StatusBadRequest, msg("Shift is not assigned to you"))
			return
		}
	}
	var reqID, teamID int
	if err := db.QueryRow(r.Context(), `
		INSERT INTO schedule_requests (user_id, workqueue_id, type, status, reason)
		VALUES ($1, $2, $3, 'pending', $4)
		RETURNING id, (SELECT team_id FROM workqueue WHERE id = $2)`,
		u.ID, body.WorkqueueID, body.Type, body.Reason).Scan(&reqID, &teamID); err != nil {
		respond500(w, "Create Request Error", err, true)
		return
	}
	logAudit(r.Context(), u.ID, "request.created", "schedule_request", reqID, teamID, map[string]any{
		"type": body.Type, "workqueueId": body.WorkqueueID, "reason": body.Reason,
	})
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Request submitted"})
}
