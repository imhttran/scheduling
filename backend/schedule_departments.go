package main

import (
	"net/http"
)

func listDepartments(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(r.Context(), `
		SELECT d.id, d.name, d.abbreviation, d.address, d.address2, d.city, d.state, d.zip, d.country,
		       d.manager_id, u.email
		FROM departments d
		LEFT JOIN users u ON u.id = d.manager_id
		ORDER BY d.name`)
	if err != nil {
		respond500(w, "List Departments Error", err, false)
		return
	}
	defer rows.Close()
	departments := []map[string]any{}
	for rows.Next() {
		var id int
		var name, abbreviation, address, address2, city, state, zip, country *string
		var managerID *int
		var managerEmail *string
		if err := rows.Scan(&id, &name, &abbreviation, &address, &address2, &city, &state, &zip, &country, &managerID, &managerEmail); err != nil {
			respond500(w, "List Departments Error", err, false)
			return
		}
		departments = append(departments, map[string]any{
			"id": id, "name": name, "abbreviation": abbreviation,
			"address": address, "address2": address2, "city": city,
			"state": state, "zip": zip, "country": country,
			"managerId": managerID, "managerEmail": managerEmail,
		})
	}
	respond(w, http.StatusOK, map[string]any{"departments": departments})
}

func createDepartment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name         string `json:"name"`
		Abbreviation string `json:"abbreviation"`
		Address      string `json:"address"`
		Address2     string `json:"address2"`
		City         string `json:"city"`
		State        string `json:"state"`
		Zip          string `json:"zip"`
		Country      string `json:"country"`
	}
	decodeJSON(r, &body)
	if body.Name == "" {
		respond(w, http.StatusBadRequest, msg("name is required"))
		return
	}
	var id int
	err := db.QueryRow(r.Context(), `
		INSERT INTO departments
			(name, abbreviation, address, address2, city, state, zip, country)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''))
		RETURNING id`,
		body.Name, body.Abbreviation, body.Address, body.Address2,
		body.City, body.State, body.Zip, body.Country).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			respond(w, http.StatusBadRequest, msg("Department already exists"))
			return
		}
		respond500(w, "Create Department Error", err, true)
		return
	}
	respond(w, http.StatusCreated, map[string]any{"success": true, "department": map[string]any{"id": id, "name": body.Name}})
}

func updateDepartment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name         string `json:"name"`
		Abbreviation string `json:"abbreviation"`
		Address      string `json:"address"`
		Address2     string `json:"address2"`
		City         string `json:"city"`
		State        string `json:"state"`
		Zip          string `json:"zip"`
		Country      string `json:"country"`
	}
	decodeJSON(r, &body)
	if body.Name == "" {
		respond(w, http.StatusBadRequest, msg("name is required"))
		return
	}
	tag, err := db.Exec(r.Context(), `
		UPDATE departments SET
			name = $1, abbreviation = NULLIF($2, ''), address = NULLIF($3, ''),
			address2 = NULLIF($4, ''), city = NULLIF($5, ''), state = NULLIF($6, ''),
			zip = NULLIF($7, ''), country = NULLIF($8, '')
		WHERE id = $9`,
		body.Name, body.Abbreviation, body.Address, body.Address2,
		body.City, body.State, body.Zip, body.Country, targetID(r))
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
