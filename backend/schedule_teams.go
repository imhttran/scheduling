package main

import (
	"net/http"
)

func listTeams(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	query := `
		SELECT t.id, t.name, t.team_code, t.department_id, d.name
		FROM teams t JOIN departments d ON d.id = t.department_id`
	args := []any{}
	if !hasRole(u.Role, "admin") {
		teamIDs, err := callerScopeTeamIDs(r.Context(), u.ID)
		if err != nil {
			respond500(w, "List Teams Error", err, false)
			return
		}
		query += ` WHERE t.id = ANY($1)`
		args = append(args, teamIDs)
	}
	query += ` ORDER BY t.name`
	rows, err := db.Query(r.Context(), query, args...)
	if err != nil {
		respond500(w, "List Teams Error", err, false)
		return
	}
	defer rows.Close()
	teams := []map[string]any{}
	for rows.Next() {
		var id, deptID int
		var name, deptName string
		var code *string
		if err := rows.Scan(&id, &name, &code, &deptID, &deptName); err != nil {
			respond500(w, "List Teams Error", err, false)
			return
		}
		teams = append(teams, map[string]any{
			"id": id, "name": name, "teamCode": code, "departmentId": deptID, "departmentName": deptName,
		})
	}
	respond(w, http.StatusOK, map[string]any{"teams": teams})
}

func createTeam(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name         string `json:"name"`
		TeamCode     string `json:"teamCode"`
		DepartmentID int    `json:"departmentId"`
	}
	decodeJSON(r, &body)
	if body.Name == "" || body.DepartmentID == 0 {
		respond(w, http.StatusBadRequest, msg("name and departmentId are required"))
		return
	}
	if len(body.TeamCode) > 20 {
		respond(w, http.StatusBadRequest, msg("teamCode must be 20 characters or fewer"))
		return
	}
	var id int
	err := db.QueryRow(r.Context(), `
		INSERT INTO teams (name, team_code, department_id) VALUES ($1, NULLIF($2, ''), $3)
		RETURNING id`,
		body.Name, body.TeamCode, body.DepartmentID).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			respond(w, http.StatusBadRequest, msg("Team already exists"))
			return
		}
		respond500(w, "Create Team Error", err, true)
		return
	}
	respond(w, http.StatusCreated, map[string]any{"success": true, "team": map[string]any{"id": id, "name": body.Name, "teamCode": body.TeamCode, "departmentId": body.DepartmentID}})
}

func updateTeam(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name         string `json:"name"`
		TeamCode     string `json:"teamCode"`
		DepartmentID int    `json:"departmentId"`
	}
	decodeJSON(r, &body)
	if body.Name == "" || body.DepartmentID == 0 {
		respond(w, http.StatusBadRequest, msg("name and departmentId are required"))
		return
	}
	if len(body.TeamCode) > 20 {
		respond(w, http.StatusBadRequest, msg("teamCode must be 20 characters or fewer"))
		return
	}
	tag, err := db.Exec(r.Context(), `
		UPDATE teams SET name = $1, team_code = NULLIF($2, ''), department_id = $3
		WHERE id = $4`,
		body.Name, body.TeamCode, body.DepartmentID, targetID(r))
	if err != nil {
		if isUniqueViolation(err) {
			respond(w, http.StatusBadRequest, msg("Team already exists"))
			return
		}
		respond500(w, "Update Team Error", err, true)
		return
	}
	if tag.RowsAffected() == 0 {
		respond(w, http.StatusNotFound, msg("Team not found"))
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Team updated"})
}
