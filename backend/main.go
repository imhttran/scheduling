package main

import (
	"context"
	_ "embed"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/001_init.sql
var schemaSQL string

//go:embed migrations/002_jobs.sql
var jobsSQL string

//go:embed migrations/003_job_requirements.sql
var jobRequirementsSQL string

//go:embed migrations/004_job_holidays.sql
var jobHolidaysSQL string

//go:embed migrations/005_worker_hours.sql
var workerHoursSQL string

//go:embed migrations/006_job_default_schedules.sql
var jobDefaultSchedulesSQL string

//go:embed migrations/007_weekday_work_hours.sql
var weekdayWorkHoursSQL string

func main() {
	loadEnvFiles()
	loadConfig()

	ctx := context.Background()
	var err error
	db, err = pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	migrate(ctx)
	seedDevAdmin(ctx)
	seedFromCSV(ctx)
	seedWorkqueue(ctx)
	seedStudentAvailability(ctx)
	seedStudentShifts(ctx)
	seedStaff(ctx)
	seedRequests(ctx)
	seedJobHolidays(ctx)
	seedWeeklySchedules(ctx)

	go startEmailWorker(ctx)

	log.Printf("Backend server running at http://localhost:%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, newRouter()); err != nil {
		log.Fatal(err)
	}
}

// One embedded SQL migration, applied at boot and recorded in schema_migrations.
func migrate(ctx context.Context) {
	if _, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	for _, m := range []struct {
		version int
		sql     string
	}{
		{1, schemaSQL},
		{2, jobsSQL},
		{3, jobRequirementsSQL},
		{4, jobHolidaysSQL},
		{5, workerHoursSQL},
		{6, jobDefaultSchedulesSQL},
		{7, weekdayWorkHoursSQL},
	} {
		var applied bool
		if err := db.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, m.version).Scan(&applied); err != nil {
			log.Fatalf("migrate: %v", err)
		}
		if applied {
			continue
		}
		// pgx's extended protocol rejects multi-statement strings; strip comment
		// lines (they can contain semicolons), then run one statement at a time.
		for _, stmt := range splitStatements(m.sql) {
			if _, err := db.Exec(ctx, stmt); err != nil {
				log.Fatalf("migrate: %v", err)
			}
		}
		if _, err := db.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, m.version); err != nil {
			log.Fatalf("migrate: %v", err)
		}
		log.Printf("[migrate] applied migration %d", m.version)
	}
}

func splitStatements(sqlText string) []string {
	var statements []string
	var cleaned strings.Builder
	for _, line := range strings.Split(sqlText, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			cleaned.WriteString(line)
			cleaned.WriteString("\n")
		}
	}
	for _, stmt := range strings.Split(cleaned.String(), ";") {
		if strings.TrimSpace(stmt) != "" {
			statements = append(statements, stmt)
		}
	}
	return statements
}

// Dev-only convenience: guarantees a known admin login exists locally, so
// there's no manual set-role step for local dev. Gated on NODE_ENV so these
// credentials can never appear in a qa/prod database.
func seedDevAdmin(ctx context.Context) {
	if cfg.Env != "development" {
		return
	}
	const devAdminEmail = "admin@mail.com"
	const devAdminPassword = "Password1234!"
	var id int
	err := db.QueryRow(ctx, `
		INSERT INTO users (email, password, role, email_verified)
		VALUES ($1, $2, 'admin', true)
		ON CONFLICT (email) DO NOTHING
		RETURNING id`,
		devAdminEmail, hashPassword(devAdminPassword)).Scan(&id)
	if err != nil {
		if err := db.QueryRow(ctx,
			`SELECT id FROM users WHERE email = $1`, devAdminEmail).Scan(&id); err != nil {
			log.Printf("[seed] failed: %v", err)
			return
		}
	}
	// Pre-fill the profile too, so the dev admin isn't stopped by its own
	// onboarding gate (see onboardingGates in app.go).
	if _, err := db.Exec(ctx, `
		INSERT INTO user_profiles (user_id, first_name, last_name, address, state, zip, phone)
		VALUES ($1, 'Dev', 'Admin', 'N/A', 'N/A', '00000', 'N/A')
		ON CONFLICT (user_id) DO NOTHING`, id); err != nil {
		log.Printf("[seed] failed: %v", err)
		return
	}
	log.Printf("[seed] dev admin ready: %s", devAdminEmail)
}
