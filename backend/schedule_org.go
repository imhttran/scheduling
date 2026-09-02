package main

import (
	"net/http"
)

// Org chart for the manager page: departments in the caller's scope, each with
// its department manager and the teams under it with their managers and worker
// counts. Scope mirrors listTeams — admins see the whole org; a manager sees
// the departments they manage (every team in them) plus the departments
// holding the teams they manage directly (only those teams attached).
func listOrg(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)

	deptQuery := `
		SELECT d.id, d.name, d.manager_id, um.email,
		       COALESCE(up.first_name || ' ' || up.last_name, '')
		FROM departments d
		LEFT JOIN users um ON um.id = d.manager_id
		LEFT JOIN user_profiles up ON up.user_id = um.id`
	teamQuery := `
		SELECT t.id, t.name, t.department_id, t.manager_id, um.email,
		       COALESCE(up.first_name || ' ' || up.last_name, ''),
		       (SELECT COUNT(*) FROM worker_teams wt WHERE wt.team_id = t.id AND wt.active)
		FROM teams t
		LEFT JOIN users um ON um.id = t.manager_id
		LEFT JOIN user_profiles up ON up.user_id = um.id`
	args := []any{}
	if !hasRole(u.Role, "admin") {
		deptQuery += ` WHERE d.manager_id = $1
			OR d.id IN (SELECT department_id FROM teams WHERE manager_id = $1)`
		teamQuery += ` WHERE t.manager_id = $1
			OR t.department_id IN (SELECT id FROM departments WHERE manager_id = $1)`
		args = append(args, u.ID)
	}
	deptQuery += ` ORDER BY d.name`
	teamQuery += ` ORDER BY t.name`

	deptRows, err := db.Query(r.Context(), deptQuery, args...)
	if err != nil {
		respond500(w, "Org Chart Error", err, false)
		return
	}
	defer deptRows.Close()

	byDept := map[int]map[string]any{}
	order := []int{}
	for deptRows.Next() {
		var id int
		var name string
		var managerID *int
		var managerEmail, managerName *string
		if err := deptRows.Scan(&id, &name, &managerID, &managerEmail, &managerName); err != nil {
			respond500(w, "Org Chart Error", err, false)
			return
		}
		byDept[id] = map[string]any{
			"id": id, "name": name,
			"managerId": managerID, "managerEmail": managerEmail, "managerName": managerName,
			"teams": []map[string]any{},
		}
		order = append(order, id)
	}

	teamRows, err := db.Query(r.Context(), teamQuery, args...)
	if err != nil {
		respond500(w, "Org Chart Error", err, false)
		return
	}
	defer teamRows.Close()

	for teamRows.Next() {
		var id, deptID, workerCount int
		var name string
		var managerID *int
		var managerEmail, managerName *string
		if err := teamRows.Scan(&id, &name, &deptID, &managerID, &managerEmail, &managerName, &workerCount); err != nil {
			respond500(w, "Org Chart Error", err, false)
			return
		}
		if dept, ok := byDept[deptID]; ok {
			dept["teams"] = append(dept["teams"].([]map[string]any), map[string]any{
				"id": id, "name": name,
				"managerId": managerID, "managerEmail": managerEmail, "managerName": managerName,
				"workerCount": workerCount,
			})
		}
	}

	org := []map[string]any{}
	for _, id := range order {
		org = append(org, byDept[id])
	}
	respond(w, http.StatusOK, map[string]any{"org": org})
}
