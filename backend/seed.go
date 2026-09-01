package main

import (
	"context"
	"encoding/csv"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Dev-only: seeds users/profiles/assignments from seed.csv (repo root) so local
// testing has data without clicking through the admin UI. Idempotent — rows
// whose email already exists are skipped. Each row's department is created
// under its location; managers are scoped to their row's location.

func seedFromCSV(ctx context.Context) {
	if cfg.Env != "development" {
		return
	}
	path := ""
	for _, dir := range []string{".", ".."} {
		if _, err := os.Stat(filepath.Join(dir, "seed.csv")); err == nil {
			path = filepath.Join(dir, "seed.csv")
			break
		}
	}
	if path == "" {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		log.Printf("[seed] open %s: %v", path, err)
		return
	}
	defer f.Close()

	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		log.Printf("[seed] parse %s: %v", path, err)
		return
	}
	if len(records) < 2 {
		return
	}

	seeded := 0
	for _, row := range records[1:] {
		if len(row) < 24 {
			continue
		}
		email, password := strings.TrimSpace(row[0]), row[1]
		firstName, lastName := row[2], row[3]
		address1, address2 := row[4], row[5]
		city, state, zip, country := row[6], row[7], row[8], row[9]
		role, department := strings.TrimSpace(row[10]), strings.TrimSpace(row[11])
		departmentCode := row[12]
		location := strings.TrimSpace(row[13])
		locAbbr, locAddress := row[14], row[15]
		locCity, locState, locZip, locCountry := row[16], row[17], row[18], row[19]
		phone := row[20]
		uid := row[21]
		minHours, _ := strconv.Atoi(row[22])
		maxHours, _ := strconv.Atoi(row[23])

		var exists bool
		if err := db.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM users WHERE email = $1)`, email).Scan(&exists); err != nil {
			log.Printf("[seed] %s: %v", email, err)
			continue
		}
		if exists {
			continue
		}

		// Resolve (or create) the location, then the department under it.
		var locID int
		if location != "" {
			if err := db.QueryRow(ctx, `
				INSERT INTO locations
					(name, abbreviation, address, city, state, zip, country)
				VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''))
				ON CONFLICT (name) DO UPDATE SET
					abbreviation = EXCLUDED.abbreviation, address = EXCLUDED.address,
					city = EXCLUDED.city, state = EXCLUDED.state,
					zip = EXCLUDED.zip, country = EXCLUDED.country
				RETURNING id`,
				location, locAbbr, locAddress, locCity, locState, locZip, locCountry).Scan(&locID); err != nil {
				log.Printf("[seed] %s location: %v", email, err)
				continue
			}
		}
		var deptID int
		if department != "" && locID != 0 {
			if err := db.QueryRow(ctx, `
				INSERT INTO departments (name, department_code, location_id) VALUES ($1, NULLIF($2, ''), $3)
				ON CONFLICT (name) DO UPDATE SET
					department_code = EXCLUDED.department_code, location_id = EXCLUDED.location_id
				RETURNING id`, department, departmentCode, locID).Scan(&deptID); err != nil {
				log.Printf("[seed] %s department: %v", email, err)
				continue
			}
		}

		var userID int
		if err := db.QueryRow(ctx, `
			INSERT INTO users (email, uid, password, role, email_verified)
			VALUES ($1, NULLIF($2, ''), $3, $4, true) RETURNING id`,
			email, uid, hashPassword(password), role).Scan(&userID); err != nil {
			log.Printf("[seed] %s user: %v", email, err)
			continue
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO user_profiles
				(user_id, first_name, last_name, address, address2, city, state, zip, country, phone)
			VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8, $9, $10)`,
			userID, firstName, lastName, address1, address2, city, state, zip, country, phone); err != nil {
			log.Printf("[seed] %s profile: %v", email, err)
			continue
		}

		switch role {
		case "student":
			if deptID != 0 {
				// Ensure a job exists for this department (default: named after it),
				// then assign the student to it.
				var jobID int
				err := db.QueryRow(ctx,
					`SELECT id FROM jobs WHERE department_id = $1 LIMIT 1`, deptID).Scan(&jobID)
				if errors.Is(err, pgx.ErrNoRows) {
					err = db.QueryRow(ctx, `
						INSERT INTO jobs (name, department_id) VALUES ($1, $2) RETURNING id`,
						department, deptID).Scan(&jobID)
				}
				if err != nil {
					log.Printf("[seed] %s job: %v", email, err)
					continue
				}
				if _, err := db.Exec(ctx, `
					INSERT INTO student_jobs (user_id, job_id, min_hours, max_hours)
					VALUES ($1, $2, $3, $4)`, userID, jobID, minHours, maxHours); err != nil {
					log.Printf("[seed] %s job assignment: %v", email, err)
				}
			}
		case "manager":
			if _, err := db.Exec(ctx, `
				INSERT INTO manager_assignments (user_id, location_id)
				VALUES ($1, $2)`, userID, locID); err != nil {
				log.Printf("[seed] %s manager: %v", email, err)
			}
		case "scheduler":
			// Schedulers are location-scoped like managers, so seed the same
			// assignment so their scoped scheduling queries resolve.
			if _, err := db.Exec(ctx, `
				INSERT INTO manager_assignments (user_id, location_id)
				VALUES ($1, $2)`, userID, locID); err != nil {
				log.Printf("[seed] %s scheduler: %v", email, err)
			}
		}
		seeded++
	}
	if seeded > 0 {
		log.Printf("[seed] seeded %d users from %s", seeded, path)
	}
}
