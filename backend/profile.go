package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

type profile struct {
	ID                      int     `json:"id"`
	UserID                  int     `json:"userId"`
	FirstName               string  `json:"firstName"`
	LastName                string  `json:"lastName"`
	Address                 string  `json:"address"`
	Address2                *string `json:"address2"`
	State                   string  `json:"state"`
	Zip                     string  `json:"zip"`
	Country                 string  `json:"country"`
	Phone                   string  `json:"phone"`
	CommunicationPreference string  `json:"communicationPreference"`
	Linkedin                *string `json:"linkedin"`
	Github                  *string `json:"github"`
	AltEmail                *string `json:"altEmail"`
}

const profileColumns = `id, user_id, first_name, last_name, address, address2, state, zip, country, phone, communication_preference, linkedin, github, alt_email`

func scanProfile(row pgx.Row) (*profile, error) {
	var p profile
	err := row.Scan(&p.ID, &p.UserID, &p.FirstName, &p.LastName, &p.Address, &p.Address2,
		&p.State, &p.Zip, &p.Country, &p.Phone, &p.CommunicationPreference,
		&p.Linkedin, &p.Github, &p.AltEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func getProfile(w http.ResponseWriter, r *http.Request) {
	p, err := scanProfile(db.QueryRow(r.Context(),
		`SELECT `+profileColumns+` FROM user_profiles WHERE user_id = $1`, currentUser(r).ID))
	if err != nil {
		respond500(w, "Get Profile Error", err, false)
		return
	}
	respond(w, http.StatusOK, map[string]any{"profile": p})
}

type profileInput struct {
	FirstName               string  `json:"firstName"`
	LastName                string  `json:"lastName"`
	Address                 string  `json:"address"`
	Address2                *string `json:"address2"`
	State                   string  `json:"state"`
	Zip                     string  `json:"zip"`
	Country                 string  `json:"country"`
	Phone                   string  `json:"phone"`
	CommunicationPreference string  `json:"communicationPreference"`
	Linkedin                *string `json:"linkedin"`
	Github                  *string `json:"github"`
	AltEmail                *string `json:"altEmail"`
}

var requiredProfileFields = []struct {
	name  string
	value func(b *profileInput) string
}{
	{"firstName", func(b *profileInput) string { return b.FirstName }},
	{"lastName", func(b *profileInput) string { return b.LastName }},
	{"address", func(b *profileInput) string { return b.Address }},
	{"state", func(b *profileInput) string { return b.State }},
	{"zip", func(b *profileInput) string { return b.Zip }},
	{"phone", func(b *profileInput) string { return b.Phone }},
	{"communicationPreference", func(b *profileInput) string { return b.CommunicationPreference }},
}

var communicationPreferences = []string{"email", "text", "phone"}

func optionalTrimmed(s *string) *string {
	if s == nil {
		return nil
	}
	if v := strings.TrimSpace(*s); v != "" {
		return &v
	}
	return nil // `body.x?.trim() || null`
}

// One-time registration form, submitted once per user. Returns an error
// message describing the first unmet rule, or "" if valid (same convention
// as validatePassword in common/validators.js).
func validateProfileFields(body *profileInput) string {
	missing := []string{}
	for _, field := range requiredProfileFields {
		if strings.TrimSpace(field.value(body)) == "" {
			missing = append(missing, field.name)
		}
	}
	if len(missing) > 0 {
		return "Missing required field(s): " + strings.Join(missing, ", ")
	}
	validPreference := false
	for _, pref := range communicationPreferences {
		if body.CommunicationPreference == pref {
			validPreference = true
		}
	}
	if !validPreference {
		return "communicationPreference must be one of: " + strings.Join(communicationPreferences, ", ")
	}
	if !validatePhone(body.Phone) {
		return "Phone number is invalid"
	}
	if !validateZip(body.Zip) {
		return "Zip code is invalid"
	}
	if !usStateCodes[body.State] {
		return "State is invalid"
	}
	// Dropdown only ever offers what's in COUNTRIES, but a direct API call
	// could still send something else.
	if body.Country != "" && !countryCodes[body.Country] {
		return "Country is invalid"
	}
	if body.AltEmail != nil && *body.AltEmail != "" && !validateEmail(*body.AltEmail) {
		return "Additional email address is invalid"
	}
	if body.Linkedin != nil && *body.Linkedin != "" && !validateUrl(*body.Linkedin) {
		return "LinkedIn URL is invalid"
	}
	if body.Github != nil && *body.Github != "" && !validateUrl(*body.Github) {
		return "GitHub URL is invalid"
	}
	return ""
}

func saveProfile(w http.ResponseWriter, r *http.Request) {
	body := &profileInput{}
	decodeJSON(r, body)
	if validationError := validateProfileFields(body); validationError != "" {
		respond(w, http.StatusBadRequest, msg(validationError))
		return
	}
	// Omitted (not null'd) when blank, so the column's default('US') applies.
	country := strings.TrimSpace(body.Country)
	if country == "" {
		country = "US"
	}
	var p profile
	err := db.QueryRow(r.Context(), `
		INSERT INTO user_profiles
			(user_id, first_name, last_name, address, address2, state, zip, country,
			 phone, communication_preference, linkedin, github, alt_email)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING `+profileColumns,
		currentUser(r).ID,
		strings.TrimSpace(body.FirstName),
		strings.TrimSpace(body.LastName),
		strings.TrimSpace(body.Address),
		optionalTrimmed(body.Address2),
		strings.TrimSpace(body.State),
		strings.TrimSpace(body.Zip),
		country,
		strings.TrimSpace(body.Phone),
		body.CommunicationPreference,
		optionalTrimmed(body.Linkedin),
		optionalTrimmed(body.Github),
		optionalTrimmed(body.AltEmail),
	).Scan(&p.ID, &p.UserID, &p.FirstName, &p.LastName, &p.Address, &p.Address2,
		&p.State, &p.Zip, &p.Country, &p.Phone, &p.CommunicationPreference,
		&p.Linkedin, &p.Github, &p.AltEmail)
	if err != nil {
		if isUniqueViolation(err) {
			respond(w, http.StatusBadRequest, msg("Profile already exists"))
			return
		}
		respond500(w, "Save Profile Error", err, false)
		return
	}
	respond(w, http.StatusCreated, map[string]any{
		"success": true,
		"message": "Profile saved!",
		"profile": &p,
	})
}
