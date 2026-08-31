package main

// Port of common/validators.js

import (
	"net/url"
	"regexp"
)

// Port of common/usStates.js — shared by the profile form's State dropdown
// (frontend) and its server-side validation (backend), one list so the two
// can't drift out of sync.
var usStateCodes = map[string]bool{
	"AL": true, "AK": true, "AZ": true, "AR": true, "CA": true,
	"CO": true, "CT": true, "DE": true, "DC": true, "FL": true,
	"GA": true, "HI": true, "ID": true, "IL": true, "IN": true,
	"IA": true, "KS": true, "KY": true, "LA": true, "ME": true,
	"MD": true, "MA": true, "MI": true, "MN": true, "MS": true,
	"MO": true, "MT": true, "NE": true, "NV": true, "NH": true,
	"NJ": true, "NM": true, "NY": true, "NC": true, "ND": true,
	"OH": true, "OK": true, "OR": true, "PA": true, "RI": true,
	"SC": true, "SD": true, "TN": true, "TX": true, "UT": true,
	"VT": true, "VA": true, "WA": true, "WV": true, "WI": true,
	"WY": true,
}

// Same shared-list treatment as US_STATES — one entry today, but validated
// the same way (one list, checked on both ends) so adding a second country
// later is additive.
var countryCodes = map[string]bool{
	"US": true,
}

var (
	emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	// US phone numbers only, digits with optional standard formatting
	// (spaces/dots/dashes/parens) and an optional leading +1/1.
	phoneRe   = regexp.MustCompile(`^\+?1?[-.\s]?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}$`)
	zipRe     = regexp.MustCompile(`^\d{5}$`)
	upperRe   = regexp.MustCompile(`[A-Z]`)
	digitRe   = regexp.MustCompile(`\d`)
	specialRe = regexp.MustCompile(`[!@#$%^&*(),.?":{}|<>]`)
)

func validateEmail(email string) bool {
	return emailRe.MatchString(email)
}

func validatePhone(phone string) bool {
	return phoneRe.MatchString(phone)
}

// US zip codes only — 5 digits, matching the frontend's pattern="[0-9]{5}"
// (no ZIP+4 support yet).
func validateZip(zip string) bool {
	return zipRe.MatchString(zip)
}

// http(s) only — good enough for LinkedIn/GitHub profile links.
func validateUrl(rawUrl string) bool {
	u, err := url.Parse(rawUrl)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// Returns an error message describing the first unmet rule, or "" if valid.
func validatePassword(password string) string {
	if len(password) < 8 {
		return "Password must be at least 8 characters long"
	}
	if !upperRe.MatchString(password) {
		return "Password must contain at least one uppercase letter"
	}
	if !digitRe.MatchString(password) {
		return "Password must contain at least one number"
	}
	if !specialRe.MatchString(password) {
		return "Password must contain at least one special character"
	}
	return ""
}
