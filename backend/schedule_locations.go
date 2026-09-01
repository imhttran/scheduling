package main

import (
	"net/http"
)

func listLocations(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(r.Context(), `
		SELECT l.id, l.name, l.abbreviation, l.address, l.address2, l.city, l.state, l.zip, l.country,
		       ma.user_id, u.email
		FROM locations l
		LEFT JOIN LATERAL (
			SELECT ma.user_id FROM manager_assignments ma
			WHERE ma.location_id = l.id LIMIT 1
		) ma ON true
		LEFT JOIN users u ON u.id = ma.user_id
		ORDER BY l.name`)
	if err != nil {
		respond500(w, "List Locations Error", err, false)
		return
	}
	defer rows.Close()
	locations := []map[string]any{}
	for rows.Next() {
		var id int
		var name, abbreviation, address, address2, city, state, zip, country *string
		var managerID *int
		var managerEmail *string
		if err := rows.Scan(&id, &name, &abbreviation, &address, &address2, &city, &state, &zip, &country, &managerID, &managerEmail); err != nil {
			respond500(w, "List Locations Error", err, false)
			return
		}
		locations = append(locations, map[string]any{
			"id": id, "name": name, "abbreviation": abbreviation,
			"address": address, "address2": address2, "city": city,
			"state": state, "zip": zip, "country": country,
			"managerId": managerID, "managerEmail": managerEmail,
		})
	}
	respond(w, http.StatusOK, map[string]any{"locations": locations})
}

func createLocation(w http.ResponseWriter, r *http.Request) {
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
		INSERT INTO locations
			(name, abbreviation, address, address2, city, state, zip, country)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''))
		RETURNING id`,
		body.Name, body.Abbreviation, body.Address, body.Address2,
		body.City, body.State, body.Zip, body.Country).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			respond(w, http.StatusBadRequest, msg("Location already exists"))
			return
		}
		respond500(w, "Create Location Error", err, true)
		return
	}
	respond(w, http.StatusCreated, map[string]any{"success": true, "location": map[string]any{"id": id, "name": body.Name}})
}

func updateLocation(w http.ResponseWriter, r *http.Request) {
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
		UPDATE locations SET
			name = $1, abbreviation = NULLIF($2, ''), address = NULLIF($3, ''),
			address2 = NULLIF($4, ''), city = NULLIF($5, ''), state = NULLIF($6, ''),
			zip = NULLIF($7, ''), country = NULLIF($8, '')
		WHERE id = $9`,
		body.Name, body.Abbreviation, body.Address, body.Address2,
		body.City, body.State, body.Zip, body.Country, targetID(r))
	if err != nil {
		if isUniqueViolation(err) {
			respond(w, http.StatusBadRequest, msg("Location already exists"))
			return
		}
		respond500(w, "Update Location Error", err, true)
		return
	}
	if tag.RowsAffected() == 0 {
		respond(w, http.StatusNotFound, msg("Location not found"))
		return
	}
	respond(w, http.StatusOK, map[string]any{"success": true, "message": "Location updated"})
}
