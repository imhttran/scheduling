package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
)

// logAudit records an action in the audit trail. Best-effort: a failed insert
// is logged server-side but never fails the business action it accompanies —
// the alternative (transactional audit + action) would mean refactoring every
// handler onto explicit transactions. teamID scopes the row for the audit
// report (managers only see their scope's teams); 0 means unresolvable, which
// keeps the row admin-only.
func logAudit(ctx context.Context, actorID int, action, entityType string, entityID, teamID int, details map[string]any) {
	var team any
	if teamID > 0 {
		team = teamID
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO audit_log (actor_id, action, entity_type, entity_id, team_id, details)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		actorID, action, entityType, entityID, team, details); err != nil {
		log.Printf("[audit] failed to record %s (%s %d) by user %d: %v", action, entityType, entityID, actorID, err)
	}
}

// Team resolvers for audit rows. Best-effort: an unknown id logs as
// unscoped (0) rather than failing the action being audited.
func jobTeam(ctx context.Context, jobID int) int {
	var teamID int
	_ = db.QueryRow(ctx,
		`SELECT team_id FROM jobs WHERE id = $1`, jobID).Scan(&teamID)
	return teamID
}

// requestInfo resolves everything needed to audit a request action: its
// shift, worker, type, and team. Best-effort: zeros on failure.
func requestInfo(ctx context.Context, reqID int) (wqID, workerID, teamID int, typ string) {
	_ = db.QueryRow(ctx, `
		SELECT r.workqueue_id, r.user_id, r.type, w.team_id
		FROM schedule_requests r
		JOIN workqueue w ON w.id = r.workqueue_id
		WHERE r.id = $1`, reqID).Scan(&wqID, &workerID, &typ, &teamID)
	return
}

// auditWhere builds the WHERE clause shared by every audit read (report,
// export, per-entity viewers): the 15-day window, the caller's scope — a
// manager every team in their scope (managed teams plus teams under their
// managed departments), admins everything — plus the optional action/entity
// filters. NULL-team rows stay admin-only (they never match = ANY()).
func auditWhere(r *http.Request) (string, []any, error) {
	u := currentUser(r)
	where := `a.created_at >= now() - interval '15 days'`
	args := []any{}
	if !hasRole(u.Role, "admin") {
		teamIDs, err := callerScopeTeamIDs(r.Context(), u.ID)
		if err != nil {
			return "", nil, err
		}
		args = append(args, teamIDs)
		where += ` AND a.team_id = ANY($` + strconv.Itoa(len(args)) + `)`
	}
	if s := r.URL.Query().Get("action"); s != "" {
		args = append(args, s+"%")
		where += ` AND a.action LIKE $` + strconv.Itoa(len(args))
	}
	if s := r.URL.Query().Get("entityType"); s != "" {
		args = append(args, s)
		where += ` AND a.entity_type = $` + strconv.Itoa(len(args))
	}
	if s := r.URL.Query().Get("entityId"); s != "" {
		id, err := strconv.Atoi(s)
		if err != nil {
			return "", nil, err
		}
		args = append(args, id)
		where += ` AND a.entity_id = $` + strconv.Itoa(len(args))
	}
	return where, args, nil
}

const auditSelect = `
	SELECT a.id, a.actor_id, u.email, a.action, a.entity_type, a.entity_id,
	       a.team_id, t.name, a.details, a.created_at
	FROM audit_log a
	LEFT JOIN users u ON u.id = a.actor_id
	LEFT JOIN teams t ON t.id = a.team_id
	WHERE `

type auditRow struct {
	id         int
	actorID    *int
	actorEmail *string
	action     string
	entityType string
	entityID   *int
	teamID     *int
	teamName   *string
	details    []byte
	createdAt  time.Time
}

func queryAudit(r *http.Request) ([]auditRow, error) {
	where, args, err := auditWhere(r)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(r.Context(), auditSelect+where+` ORDER BY a.created_at DESC, a.id DESC LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []auditRow{}
	for rows.Next() {
		var e auditRow
		if err := rows.Scan(&e.id, &e.actorID, &e.actorEmail, &e.action, &e.entityType, &e.entityID, &e.teamID, &e.teamName, &e.details, &e.createdAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// Audit report: manager and up, scoped to what each caller manages, last 15
// days (see auditWhere).
func listAudit(w http.ResponseWriter, r *http.Request) {
	entries, err := queryAudit(r)
	if err != nil {
		respond500(w, "List Audit Error", err, false)
		return
	}
	out := []map[string]any{}
	for _, e := range entries {
		var parsed any
		if len(e.details) > 0 {
			if err := json.Unmarshal(e.details, &parsed); err != nil {
				parsed = json.RawMessage(e.details)
			}
		}
		out = append(out, map[string]any{
			"id": e.id, "actorId": e.actorID, "actorEmail": e.actorEmail,
			"action": e.action, "entityType": e.entityType, "entityId": e.entityID,
			"teamId": e.teamID, "teamName": e.teamName,
			"details": parsed, "createdAt": e.createdAt.Format(time.RFC3339),
		})
	}
	respond(w, http.StatusOK, map[string]any{"entries": out})
}

// CSV download of the same scoped report (same window, same filters).
func exportAudit(w http.ResponseWriter, r *http.Request) {
	entries, err := queryAudit(r)
	if err != nil {
		respond500(w, "Export Audit Error", err, false)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="audit-report.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"when", "who", "action", "team", "entity", "details"})
	for _, e := range entries {
		entity := e.entityType
		if e.entityID != nil {
			entity += " #" + strconv.Itoa(*e.entityID)
		}
		_ = cw.Write([]string{
			e.createdAt.Format(time.RFC3339),
			deref(e.actorEmail),
			e.action,
			deref(e.teamName),
			entity,
			string(e.details),
		})
	}
	cw.Flush()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
