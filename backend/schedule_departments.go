package main

import (
	"net/http"
)

func listDepartments(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	query := `
		SELECT d.id, d.name, d.department_code, d.location_id, l.name
		FROM departments d JOIN locations l ON l.id = d.location_id`
	args := []any{}
	if !hasRole(u.Role, "admin") {
		if u.Role == "scheduler" {
			deptID, err := schedulerDepartmentID(r.Context(), u.ID)
			if err != nil {
				respond500(w, "List Departments Error", err, false)
				return
			}
			query += ` WHERE d.id = $1`
			args = append(args, deptID)
		} else {
			locID, err := managerLocationID(r.Context(), u.ID)
			if err != nil {
				respond500(w, "List Departments Error", err, false)
				return
			}
			query += ` WHERE d.location_id = $1`
			args = append(args, locID)
		}
	}
	query += ` ORDER BY d.name`
	rows, err := db.Query(r.Context(), query, args...)
	if err != nil {
		respond500(w, "List Departments Error", err, false)
		return
	}
	defer rows.Close()
	departments := []map[string]any{}
	for rows.Next() {
		var id, locID int
		var name, locName string
		var code *string
		if err := rows.Scan(&id, &name, &code, &locID, &locName); err != nil {
			respond500(w, "List Departments Error", err, false)
			return
		}
		departments = append(departments, map[string]any{
			"id": id, "name": name, "departmentCode": code, "locationId": locID, "locationName": locName,
		})
	}
	respond(w, http.StatusOK, map[string]any{"departments": departments})
}

func createDepartment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name           string `json:"name"`
		DepartmentCode string `json:"departmentCode"`
		LocationID     int    `json:"locationId"`
	}
	decodeJSON(r, &body)
	if body.Name == "" || body.LocationID == 0 {
		respond(w, http.StatusBadRequest, msg("name and locationId are required"))
		return
	}
	if len(body.DepartmentCode) > 20 {
		respond(w, http.StatusBadRequest, msg("departmentCode must be 20 characters or fewer"))
		return
	}
	var id int
	err := db.QueryRow(r.Context(), `
		INSERT INTO departments (name, department_code, location_id) VALUES ($1, NULLIF($2, ''), $3)
		RETURNING id`,
		body.Name, body.DepartmentCode, body.LocationID).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			respond(w, http.StatusBadRequest, msg("Department already exists"))
			return
		}
		respond500(w, "Create Department Error", err, true)
		return
	}
	respond(w, http.StatusCreated, map[string]any{"success": true, "department": map[string]any{"id": id, "name": body.Name, "departmentCode": body.DepartmentCode, "locationId": body.LocationID}})
}

func updateDepartment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name           string `json:"name"`
		DepartmentCode string `json:"departmentCode"`
		LocationID     int    `json:"locationId"`
	}
	decodeJSON(r, &body)
	if body.Name == "" || body.LocationID == 0 {
		respond(w, http.StatusBadRequest, msg("name and locationId are required"))
		return
	}
	if len(body.DepartmentCode) > 20 {
		respond(w, http.StatusBadRequest, msg("departmentCode must be 20 characters or fewer"))
		return
	}
	tag, err := db.Exec(r.Context(), `
		UPDATE departments SET name = $1, department_code = NULLIF($2, ''), location_id = $3
		WHERE id = $4`,
		body.Name, body.DepartmentCode, body.LocationID, targetID(r))
	if err != nil {
		if isUniqueViolation(err) {
			respond(w, http.StatusBadRequest, msg("Department already exists"))
			return
		}
		respond500(w, "Update Department Error", err, true)
		return
	}
	if tag.RowsAffected() == 0 {
		respond(w, http.StatusNotFound, msg("Department not found"))
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Department updated"})
}
