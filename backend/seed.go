package main

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Dev-only: seeds users/profiles/assignments from seed data (backend/seed) so
// local testing has data without clicking through the admin UI. Idempotent —
// rows whose email already exists are skipped. Each row's team is created under
// its department; managers are scoped to their row's department (or team for
// the old scheduler rows).

// readSeedCSV finds and parses a CSV file in backend/seed (or next to the
// backend, or one dir up, for legacy layouts).
func readSeedCSV(name string) ([][]string, error) {
	path := ""
	for _, dir := range []string{"seed", ".", ".."} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			path = filepath.Join(dir, name)
			break
		}
	}
	if path == "" {
		return nil, os.ErrNotExist
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return csv.NewReader(f).ReadAll()
}

func seedFromCSV(ctx context.Context) {
	if cfg.Env != "development" {
		return
	}
	records, err := readSeedCSV("seed.csv")
	if err != nil {
		log.Printf("[seed] seed.csv: %v", err)
		return
	}
	if len(records) < 2 {
		return
	}

	seeded := 0
	for _, row := range records[1:] {
		if len(row) < 26 {
			continue
		}
		email, password := strings.TrimSpace(row[0]), row[1]
		firstName, lastName := row[2], row[3]
		address1, address2 := row[4], row[5]
		city, state, zip, country := row[6], row[7], row[8], row[9]
		role, team := strings.TrimSpace(row[10]), strings.TrimSpace(row[11])
		teamCode := row[12]
		department := strings.TrimSpace(row[13])
		deptAbbr, deptAddress := row[14], row[15]
		deptCity, deptState, deptZip, deptCountry := row[16], row[17], row[18], row[19]
		phone := row[20]
		uid := row[21]
		minHours, _ := strconv.Atoi(row[22])
		maxHours, _ := strconv.Atoi(row[23])
		workerType := row[24]
		var hourlyLimit *int
		if v, err := strconv.Atoi(row[25]); err == nil {
			hourlyLimit = &v
		}

		var exists bool
		if err := db.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM users WHERE email = $1)`, email).Scan(&exists); err != nil {
			log.Printf("[seed] %s: %v", email, err)
			continue
		}
		if exists {
			continue
		}

		// Resolve (or create) the department, then the team under it.
		var deptID int
		if department != "" {
			if err := db.QueryRow(ctx, `
				INSERT INTO departments
					(name, abbreviation, address, city, state, zip, country)
				VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''))
				ON CONFLICT (name) DO UPDATE SET
					abbreviation = EXCLUDED.abbreviation, address = EXCLUDED.address,
					city = EXCLUDED.city, state = EXCLUDED.state,
					zip = EXCLUDED.zip, country = EXCLUDED.country
				RETURNING id`,
				department, deptAbbr, deptAddress, deptCity, deptState, deptZip, deptCountry).Scan(&deptID); err != nil {
				log.Printf("[seed] %s department: %v", email, err)
				continue
			}
		}
		var teamID int
		if team != "" && deptID != 0 {
			if err := db.QueryRow(ctx, `
				INSERT INTO teams (name, team_code, department_id) VALUES ($1, NULLIF($2, ''), $3)
				ON CONFLICT (department_id, name) DO UPDATE SET
					team_code = EXCLUDED.team_code
				RETURNING id`, team, teamCode, deptID).Scan(&teamID); err != nil {
				log.Printf("[seed] %s team: %v", email, err)
				continue
			}
		}

		// The scheduler role is gone — its holders become team-scoped managers.
		wasScheduler := false
		if role == "scheduler" {
			wasScheduler = true
			role = "manager"
		}

		var userID int
		if err := db.QueryRow(ctx, `
			INSERT INTO users (email, uid, password, roles, worker_type, hourly_limit, email_verified)
			VALUES ($1, NULLIF($2, ''), $3, ARRAY[$4], COALESCE(NULLIF($5, ''), 'student'), $6, true) RETURNING id`,
			email, uid, hashPassword(password), role, workerType, hourlyLimit).Scan(&userID); err != nil {
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

		switch {
		case role == "student" || role == "staff":
			if teamID != 0 {
				// Ensure a job exists for this team (default: named after it),
				// then assign the worker to it so they see the workqueue and can be
				// assigned shifts.
				var jobID int
				err := db.QueryRow(ctx,
					`SELECT id FROM jobs WHERE team_id = $1 LIMIT 1`, teamID).Scan(&jobID)
				if errors.Is(err, pgx.ErrNoRows) {
					err = db.QueryRow(ctx, `
						INSERT INTO jobs (name, team_id) VALUES ($1, $2) RETURNING id`,
						team, teamID).Scan(&jobID)
				}
				if err != nil {
					log.Printf("[seed] %s job: %v", email, err)
					continue
				}
				if err := insertDefaultJobSchedules(ctx, jobID); err != nil {
					log.Printf("[seed] %s job schedules: %v", email, err)
				}
				if _, err := db.Exec(ctx, `
					INSERT INTO student_jobs (user_id, job_id, min_hours, max_hours)
					VALUES ($1, $2, $3, $4)`, userID, jobID, minHours, maxHours); err != nil {
					log.Printf("[seed] %s job assignment: %v", email, err)
				}
				if _, err := db.Exec(ctx, `
					INSERT INTO worker_teams (user_id, team_id, active)
					VALUES ($1, $2, true)
					ON CONFLICT (user_id, team_id) DO UPDATE SET active = true`,
					userID, teamID); err != nil {
					log.Printf("[seed] %s team membership: %v", email, err)
				}
			}
		case wasScheduler:
			// Old scheduler rows become managers scoped to their team.
			if teamID != 0 {
				if _, err := db.Exec(ctx, `
					UPDATE teams SET manager_id = $1 WHERE id = $2`, userID, teamID); err != nil {
					log.Printf("[seed] %s team manager: %v", email, err)
				}
			}
		case role == "manager":
			if deptID != 0 {
				if _, err := db.Exec(ctx, `
					UPDATE departments SET manager_id = $1 WHERE id = $2`, userID, deptID); err != nil {
					log.Printf("[seed] %s manager: %v", email, err)
				}
			}
		}
		seeded++
	}
	if seeded > 0 {
		log.Printf("[seed] seeded %d users from seed.csv", seeded)
	}
}

// Dev-only: seeds extra jobs (jobs.csv) and full-time staff (staff.csv) on top
// of the one-per-team defaults. Idempotent — existing jobs/users are skipped.
func seedExtraData(ctx context.Context) {
	if cfg.Env != "development" {
		return
	}
	// resolveTeam finds (or creates) a team by name under a department.
	resolveTeam := func(name, department string) (int, error) {
		var deptID int
		if err := db.QueryRow(ctx, `SELECT id FROM departments WHERE name = $1`, department).Scan(&deptID); err != nil {
			return 0, err
		}
		var teamID int
		err := db.QueryRow(ctx, `
			INSERT INTO teams (name, team_code, department_id)
			VALUES ($1, 'FAC', $2)
			ON CONFLICT (department_id, name) DO UPDATE SET team_code = EXCLUDED.team_code
			RETURNING id`, name, deptID).Scan(&teamID)
		return teamID, err
	}

	// Extra jobs.
	jobs, err := readSeedCSV("jobs.csv")
	if err != nil {
		log.Printf("[seed] jobs.csv: %v", err)
		return
	}
	seededJobs := 0
	for _, row := range jobs[1:] {
		if len(row) < 4 {
			continue
		}
		teamID, err := resolveTeam(row[1], row[2])
		if err != nil {
			log.Printf("[seed] jobs team: %v", err)
			continue
		}
		optimal, _ := strconv.Atoi(row[3])
		var jobID int
		err = db.QueryRow(ctx, `
			INSERT INTO jobs (name, team_id, optimal_workers)
			VALUES ($1, $2, $3)
			ON CONFLICT (name, team_id) DO NOTHING
			RETURNING id`, row[0], teamID, optimal).Scan(&jobID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // already exists
		}
		if err != nil {
			log.Printf("[seed] jobs: %v", err)
			continue
		}
		if row[0] != "Security Guard" {
			if err := insertDefaultJobSchedules(ctx, jobID); err != nil {
				log.Printf("[seed] jobs schedules: %v", err)
			}
		}
		seededJobs++
	}

	// Full-time staff. Start UIDs above the highest existing one so they never
	// collide with users seeded from seed.csv (which uses U00001..U00055).
	staff, err := readSeedCSV("staff.csv")
	if err != nil {
		log.Printf("[seed] staff.csv: %v", err)
		return
	}
	var maxUID int
	if err := db.QueryRow(ctx, `
		SELECT COALESCE(MAX((substring(uid, 2)::int)), 0) FROM users
		WHERE uid ~ '^U[0-9]+$'`).Scan(&maxUID); err != nil {
		log.Printf("[seed] staff uid: %v", err)
		return
	}
	uidNum, phoneNum := maxUID+1, 138
	seededStaff := 0
	for _, row := range staff[1:] {
		if len(row) < 7 {
			continue
		}
		email, first, last := row[0], row[1], row[2]
		jobName, teamName, deptName, workerType := row[3], row[4], row[5], row[6]
		teamID, err := resolveTeam(teamName, deptName)
		if err != nil {
			log.Printf("[seed] staff team: %v", err)
			continue
		}
		var jobID int
		if err := db.QueryRow(ctx, `SELECT id FROM jobs WHERE name = $1 AND team_id = $2`, jobName, teamID).Scan(&jobID); err != nil {
			log.Printf("[seed] staff job: %v", err)
			continue
		}
		uid := fmt.Sprintf("U%05d", uidNum)
		phone := fmt.Sprintf("512-555-%04d", phoneNum)
		uidNum++
		phoneNum++
		var userID int
		err = db.QueryRow(ctx, `
			INSERT INTO users (email, uid, password, roles, worker_type, email_verified)
			VALUES ($1, $2, $3, ARRAY['staff'], $4, true)
			ON CONFLICT (email) DO NOTHING
			RETURNING id`, email, uid, hashPassword("Fulltime1234!"), workerType).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // user already exists
		}
		if err != nil {
			log.Printf("[seed] staff user: %v", err)
			continue
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO user_profiles (user_id, first_name, last_name, address, city, state, zip, country, phone)
			VALUES ($1, $2, $3, '100 Campus Dr', 'Austin', 'TX', '78705', 'US', $4)`,
			userID, first, last, phone); err != nil {
			log.Printf("[seed] staff profile: %v", err)
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO student_jobs (user_id, job_id, min_hours, max_hours)
			VALUES ($1, $2, 0, 40)`, userID, jobID); err != nil {
			log.Printf("[seed] staff assignment: %v", err)
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO worker_teams (user_id, team_id, active)
			SELECT $1, j.team_id, true FROM jobs j WHERE j.id = $2
			ON CONFLICT (user_id, team_id) DO NOTHING`, userID, jobID); err != nil {
			log.Printf("[seed] staff membership: %v", err)
		}
		seededStaff++
	}
	if seededJobs > 0 {
		log.Printf("[seed] seeded %d extra jobs", seededJobs)
	}
	if seededStaff > 0 {
		log.Printf("[seed] seeded %d full-time staff", seededStaff)
	}
}

// securityShifts are the three 8h shifts that give 24h coverage, every day of
// the week (00:00-08:00, 08:00-16:00, 16:00-24:00).
var securityShifts = []struct {
	dow        int
	start, end string
}{
	{0, "00:00", "08:00"}, {0, "08:00", "16:00"}, {0, "16:00", "24:00"},
	{1, "00:00", "08:00"}, {1, "08:00", "16:00"}, {1, "16:00", "24:00"},
	{2, "00:00", "08:00"}, {2, "08:00", "16:00"}, {2, "16:00", "24:00"},
	{3, "00:00", "08:00"}, {3, "08:00", "16:00"}, {3, "16:00", "24:00"},
	{4, "00:00", "08:00"}, {4, "08:00", "16:00"}, {4, "16:00", "24:00"},
	{5, "00:00", "08:00"}, {5, "08:00", "16:00"}, {5, "16:00", "24:00"},
	{6, "00:00", "08:00"}, {6, "08:00", "16:00"}, {6, "16:00", "24:00"},
}

// Dev-only: gives every Security Guard job 24h coverage (three 8h shifts per
// day, all week). Idempotent — replaces whatever schedules the job had so a
// re-run converges on full coverage.
func seedSecuritySchedules(ctx context.Context) {
	if cfg.Env != "development" {
		return
	}
	rows, err := db.Query(ctx, `SELECT id FROM jobs WHERE name = 'Security Guard'`)
	if err != nil {
		log.Printf("[seed] security schedules: %v", err)
		return
	}
	defer rows.Close()
	var jobIDs []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			log.Printf("[seed] security schedules: %v", err)
			return
		}
		jobIDs = append(jobIDs, id)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[seed] security schedules: %v", err)
		return
	}
	seeded := 0
	for _, jobID := range jobIDs {
		if _, err := db.Exec(ctx, `DELETE FROM job_schedules WHERE job_id = $1`, jobID); err != nil {
			log.Printf("[seed] security schedules: %v", err)
			return
		}
		for _, s := range securityShifts {
			if _, err := db.Exec(ctx, `
				INSERT INTO job_schedules (job_id, day_of_week, start_time, end_time)
				VALUES ($1, $2, $3, $4)`, jobID, s.dow, s.start, s.end); err != nil {
				log.Printf("[seed] security schedules: %v", err)
				return
			}
		}
		seeded++
	}
	if seeded > 0 {
		log.Printf("[seed] set 24h schedules for %d security jobs", seeded)
	}
}

// Dev-only: sets each default job's optimal_workers to match the number of
// active workers assigned to it, so the Job Requirements view reads sensibly
// (e.g. 7/7 instead of 7/1). Only touches jobs named after their team
// (the one-per-team defaults); extra jobs keep their explicit values.
// Idempotent.
func seedJobOptimalWorkers(ctx context.Context) {
	if cfg.Env != "development" {
		return
	}
	tag, err := db.Exec(ctx, `
		UPDATE jobs j SET optimal_workers = (
			SELECT COUNT(*) FROM student_jobs sj WHERE sj.job_id = j.id AND sj.active)
		WHERE j.name = (SELECT t.name FROM teams t WHERE t.id = j.team_id)`)
	if err != nil {
		log.Printf("[seed] job optimal workers: %v", err)
		return
	}
	if tag.RowsAffected() > 0 {
		log.Printf("[seed] set optimal_workers for %d default jobs", tag.RowsAffected())
	}
}

// Dev-only: backfills default operating hours for every job with no
// job_schedules rows — the old schema's migration-time backfill, now a seed
// step (weekday 09:00-17:00, weekend 10:00-20:00). Idempotent.
func seedDefaultJobSchedules(ctx context.Context) {
	if cfg.Env != "development" {
		return
	}
	rows, err := db.Query(ctx, `
		SELECT j.id FROM jobs j
		WHERE NOT EXISTS (SELECT 1 FROM job_schedules js WHERE js.job_id = j.id)`)
	if err != nil {
		log.Printf("[seed] default job schedules: %v", err)
		return
	}
	defer rows.Close()
	var jobIDs []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			log.Printf("[seed] default job schedules: %v", err)
			return
		}
		jobIDs = append(jobIDs, id)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[seed] default job schedules: %v", err)
		return
	}
	seeded := 0
	for _, jobID := range jobIDs {
		if err := insertDefaultJobSchedules(ctx, jobID); err != nil {
			log.Printf("[seed] default job schedules: %v", err)
			return
		}
		seeded++
	}
	if seeded > 0 {
		log.Printf("[seed] backfilled default schedules for %d jobs", seeded)
	}
}

// Dev-only: fills the workqueue with open shifts for this week and next week,
// one per job's daily operating hours. Idempotent — existing shifts are kept.
func seedWorkqueue(ctx context.Context) {
	if cfg.Env != "development" {
		return
	}
	rows, err := db.Query(ctx, `
		SELECT j.team_id, js.job_id, js.day_of_week, js.start_time, js.end_time
		FROM jobs j
		JOIN job_schedules js ON js.job_id = j.id`)
	if err != nil {
		log.Printf("[seed] workqueue: %v", err)
		return
	}
	defer rows.Close()
	type shift struct {
		teamID, jobID, dow int
		start, end         string
	}
	var shifts []shift
	for rows.Next() {
		var s shift
		if err := rows.Scan(&s.teamID, &s.jobID, &s.dow, &s.start, &s.end); err != nil {
			log.Printf("[seed] workqueue: %v", err)
			return
		}
		shifts = append(shifts, s)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[seed] workqueue: %v", err)
		return
	}
	// This week and next week.
	monday := weekMondayOf(time.Now())
	seeded := 0
	for _, s := range shifts {
		for week := 0; week < 2; week++ {
			date := weekDate(monday.AddDate(0, 0, week*7), s.dow)
			tag, err := db.Exec(ctx, `
				INSERT INTO workqueue (team_id, job_id, date, start_time, end_time, status)
				SELECT $1, $2, $3, $4, $5, 'open'
				WHERE NOT EXISTS (
					SELECT 1 FROM workqueue WHERE job_id = $2 AND date = $3 AND start_time = $4)`, s.teamID, s.jobID, date, s.start, s.end)
			if err != nil {
				log.Printf("[seed] workqueue: %v", err)
				return
			}
			seeded += int(tag.RowsAffected())
		}
	}
	if seeded > 0 {
		log.Printf("[seed] seeded %d workqueue shifts", seeded)
	}
}

// Dev-only: seeds each student's weekday availability (preferred_times) with
// random 2-6 hour slots until they're close to the 20h weekly max. Idempotent —
// students who already have preferences are skipped.
func seedStudentAvailability(ctx context.Context) {
	if cfg.Env != "development" {
		return
	}
	rows, err := db.Query(ctx, `SELECT id FROM users WHERE role = 'student'`)
	if err != nil {
		log.Printf("[seed] availability: %v", err)
		return
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			log.Printf("[seed] availability: %v", err)
			return
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[seed] availability: %v", err)
		return
	}
	seeded := 0
	for _, id := range ids {
		var existing int
		if err := db.QueryRow(ctx,
			`SELECT COUNT(*) FROM preferred_times WHERE user_id = $1`, id).Scan(&existing); err != nil {
			log.Printf("[seed] availability: %v", err)
			return
		}
		if existing > 0 {
			continue
		}
		total := 0
		for total < 18 {
			length := 2 + rand.Intn(5) // 2..6 hours
			if total+length > 20 {
				length = 20 - total
				if length < 2 {
					break
				}
			}
			startHour := 8 + rand.Intn(22-8-length+1) // keep end <= 22:00
			dow := 1 + rand.Intn(5)                   // Mon..Fri
			start := fmt.Sprintf("%02d:00", startHour)
			end := fmt.Sprintf("%02d:00", startHour+length)
			if _, err := db.Exec(ctx, `
				INSERT INTO preferred_times (user_id, day_of_week, start_time, end_time)
				VALUES ($1, $2, $3, $4)`, id, dow, start, end); err != nil {
				log.Printf("[seed] availability: %v", err)
				return
			}
			total += length
		}
		seeded++
	}
	if seeded > 0 {
		log.Printf("[seed] seeded availability for %d students", seeded)
	}
}

// assignShift is an open workqueue shift eligible for assignment.
type assignShift struct {
	id, job, team, week int
	hours               float64
}

// assignWorker is a worker eligible to take shifts, with their weekly cap and
// the jobs they're qualified for.
type assignWorker struct {
	id   int
	jobs map[int]bool
	cap  float64
}

type weekUsage struct{ w0, w1 float64 }

// planAssignments picks which open shifts to assign to which workers so that
// roughly targetFraction of the total shift hours are filled, respecting each
// worker's weekly cap. It returns a map of shift id -> worker id. Inputs are not
// modified; the result varies run to run (shuffled).
func planAssignments(shifts []assignShift, workers []assignWorker, targetFraction float64) map[int]int {
	var total float64
	for _, s := range shifts {
		total += s.hours
	}
	target := total * targetFraction

	order := make([]int, len(workers))
	for i := range workers {
		order[i] = i
	}
	rand.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })

	shuffled := make([]assignShift, len(shifts))
	copy(shuffled, shifts)
	rand.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	used := make([]weekUsage, len(workers))
	plan := map[int]int{}
	assignedHours := 0.0
	for _, s := range shuffled {
		if assignedHours >= target {
			break
		}
		// Candidate workers qualified for this job with weekly capacity.
		var candidates []int
		for _, wi := range order {
			w := workers[wi]
			if !w.jobs[s.job] {
				continue
			}
			var weekHours float64
			if s.week == 0 {
				weekHours = used[wi].w0
			} else {
				weekHours = used[wi].w1
			}
			if weekHours+s.hours <= w.cap {
				candidates = append(candidates, wi)
			}
		}
		if len(candidates) == 0 {
			continue
		}
		rand.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
		wi := candidates[0]
		if s.week == 0 {
			used[wi].w0 += s.hours
		} else {
			used[wi].w1 += s.hours
		}
		plan[s.id] = workers[wi].id
		assignedHours += s.hours
	}
	return plan
}

// Dev-only: assigns open workqueue shifts to workers (students and staff) until
// roughly 80% of the workqueue is filled, leaving ~20% open so there's room to
// create new data. Respects each worker's weekly cap (worker_type). Idempotent —
// only touches open shifts.
func seedAssignments(ctx context.Context) {
	if cfg.Env != "development" {
		return
	}
	rows, err := db.Query(ctx, `
		SELECT sj.user_id, j.id AS job_id
		FROM student_jobs sj JOIN jobs j ON j.id = sj.job_id
		WHERE sj.active`)
	if err != nil {
		log.Printf("[seed] assignments: %v", err)
		return
	}
	workers := map[int]*assignWorker{}
	var order []int
	for rows.Next() {
		var uid, jobID int
		if err := rows.Scan(&uid, &jobID); err != nil {
			rows.Close()
			log.Printf("[seed] assignments: %v", err)
			return
		}
		w, ok := workers[uid]
		if !ok {
			w = &assignWorker{id: uid, jobs: map[int]bool{}}
			workers[uid] = w
			order = append(order, uid)
		}
		w.jobs[jobID] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("[seed] assignments: %v", err)
		return
	}
	for _, uid := range order {
		wt, hl, err := workerSettings(ctx, uid)
		if err != nil {
			log.Printf("[seed] assignments: %v", err)
			return
		}
		workers[uid].cap = workerPolicyFor(wt, hl).cap
	}

	// Open shifts in the next two weeks.
	monday := weekMondayOf(time.Now())
	srows, err := db.Query(ctx, `
		SELECT id, team_id, job_id, date, start_time, end_time FROM workqueue
		WHERE status = 'open' AND date >= $1 AND date < $2
		ORDER BY date, start_time`, monday, monday.AddDate(0, 0, 14))
	if err != nil {
		log.Printf("[seed] assignments: %v", err)
		return
	}
	var shifts []assignShift
	for srows.Next() {
		var s assignShift
		var date time.Time
		var start, end string
		if err := srows.Scan(&s.id, &s.team, &s.job, &date, &start, &end); err != nil {
			srows.Close()
			log.Printf("[seed] assignments: %v", err)
			return
		}
		if date.After(monday.AddDate(0, 0, 6)) {
			s.week = 1
		}
		s.hours = hoursBetween(start, end)
		shifts = append(shifts, s)
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		log.Printf("[seed] assignments: %v", err)
		return
	}

	var workerList []assignWorker
	for _, uid := range order {
		workerList = append(workerList, *workers[uid])
	}
	plan := planAssignments(shifts, workerList, 0.9)

	assigned := 0
	for shiftID, userID := range plan {
		if _, err := db.Exec(ctx,
			`UPDATE workqueue SET status = 'assigned', assigned_user_id = $1 WHERE id = $2 AND status = 'open'`,
			userID, shiftID); err != nil {
			log.Printf("[seed] assignments: %v", err)
			return
		}
		assigned++
	}
	if assigned > 0 {
		var total, filled float64
		for _, s := range shifts {
			total += s.hours
			if _, ok := plan[s.id]; ok {
				filled += s.hours
			}
		}
		log.Printf("[seed] assigned %d workqueue shifts (%.0f%% of %.0fh)", assigned, 100*filled/total, total)
	}
}

// Dev-only: assigns every open Security Guard shift to a security guard so the
// building is fully covered 24h. seedAssignments leaves ~10% of the workqueue
// open globally; this tops up the security shifts to 100% using the guards'
// remaining weekly capacity. Idempotent — only touches open shifts.
func seedSecurityAssignments(ctx context.Context) {
	if cfg.Env != "development" {
		return
	}
	// Security guards: users assigned to a Security Guard job.
	rows, err := db.Query(ctx, `
		SELECT DISTINCT sj.user_id, j.id AS job_id
		FROM student_jobs sj JOIN jobs j ON j.id = sj.job_id
		WHERE j.name = 'Security Guard' AND sj.active`)
	if err != nil {
		log.Printf("[seed] security assignments: %v", err)
		return
	}
	workers := map[int]*assignWorker{}
	var order []int
	for rows.Next() {
		var uid, jobID int
		if err := rows.Scan(&uid, &jobID); err != nil {
			rows.Close()
			log.Printf("[seed] security assignments: %v", err)
			return
		}
		w, ok := workers[uid]
		if !ok {
			w = &assignWorker{id: uid, jobs: map[int]bool{}}
			workers[uid] = w
			order = append(order, uid)
		}
		w.jobs[jobID] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("[seed] security assignments: %v", err)
		return
	}
	for _, uid := range order {
		wt, hl, err := workerSettings(ctx, uid)
		if err != nil {
			log.Printf("[seed] security assignments: %v", err)
			return
		}
		workers[uid].cap = workerPolicyFor(wt, hl).cap
	}

	// Open security shifts in the next two weeks.
	monday := weekMondayOf(time.Now())
	srows, err := db.Query(ctx, `
		SELECT w.id, w.team_id, w.job_id, w.date, w.start_time, w.end_time
		FROM workqueue w JOIN jobs j ON j.id = w.job_id
		WHERE w.status = 'open' AND j.name = 'Security Guard'
		  AND w.date >= $1 AND w.date < $2
		ORDER BY w.date, w.start_time`, monday, monday.AddDate(0, 0, 14))
	if err != nil {
		log.Printf("[seed] security assignments: %v", err)
		return
	}
	var shifts []assignShift
	for srows.Next() {
		var s assignShift
		var date time.Time
		var start, end string
		if err := srows.Scan(&s.id, &s.team, &s.job, &date, &start, &end); err != nil {
			srows.Close()
			log.Printf("[seed] security assignments: %v", err)
			return
		}
		if date.After(monday.AddDate(0, 0, 6)) {
			s.week = 1
		}
		s.hours = hoursBetween(start, end)
		shifts = append(shifts, s)
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		log.Printf("[seed] security assignments: %v", err)
		return
	}

	var workerList []assignWorker
	for _, uid := range order {
		workerList = append(workerList, *workers[uid])
	}
	plan := planAssignments(shifts, workerList, 1.0)

	assigned := 0
	for shiftID, userID := range plan {
		if _, err := db.Exec(ctx,
			`UPDATE workqueue SET status = 'assigned', assigned_user_id = $1 WHERE id = $2 AND status = 'open'`,
			userID, shiftID); err != nil {
			log.Printf("[seed] security assignments: %v", err)
			return
		}
		assigned++
	}
	if assigned > 0 {
		log.Printf("[seed] assigned %d security shifts to guards", assigned)
	}
}

// Dev-only: ensures the seeded staff account has a job so they see the
// workqueue. Shift assignment is handled by seedAssignments.
func seedStaff(ctx context.Context) {
	if cfg.Env != "development" {
		return
	}
	var staffID int
	err := db.QueryRow(ctx, `SELECT id FROM users WHERE email = 'staff@mail.edu'`).Scan(&staffID)
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err != nil {
		log.Printf("[seed] staff: %v", err)
		return
	}

	// Ensure the staff has a job so they see the workqueue.
	var teamID int
	err = db.QueryRow(ctx, `
		SELECT j.team_id FROM student_jobs sj JOIN jobs j ON j.id = sj.job_id
		WHERE sj.user_id = $1 LIMIT 1`, staffID).Scan(&teamID)
	if errors.Is(err, pgx.ErrNoRows) {
		var jobID int
		err = db.QueryRow(ctx, `
			INSERT INTO jobs (name, team_id)
			SELECT 'Staff Work', id FROM teams ORDER BY id LIMIT 1
			RETURNING id`).Scan(&jobID)
		if err != nil {
			log.Printf("[seed] staff job: %v", err)
			return
		}
		if err := insertDefaultJobSchedules(ctx, jobID); err != nil {
			log.Printf("[seed] staff job schedules: %v", err)
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO student_jobs (user_id, job_id, min_hours, max_hours)
			VALUES ($1, $2, 0, 40)`, staffID, jobID); err != nil {
			log.Printf("[seed] staff job assignment: %v", err)
			return
		}
		var newTeamID int
		if err := db.QueryRow(ctx, `SELECT team_id FROM jobs WHERE id = $1`, jobID).Scan(&newTeamID); err != nil {
			log.Printf("[seed] staff team: %v", err)
			return
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO worker_teams (user_id, team_id, active)
			VALUES ($1, $2, true)
			ON CONFLICT (user_id, team_id) DO NOTHING`, staffID, newTeamID); err != nil {
			log.Printf("[seed] staff membership: %v", err)
		}
	}
}

// Dev-only: seeds a few pending schedule_requests (miss/overflow) per student so
// the manager approval view has data. Idempotent — students who already have
// requests are skipped.
func seedRequests(ctx context.Context) {
	if cfg.Env != "development" {
		return
	}
	rows, err := db.Query(ctx, `SELECT id FROM users WHERE role IN ('student','staff')`)
	if err != nil {
		log.Printf("[seed] requests: %v", err)
		return
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			log.Printf("[seed] requests: %v", err)
			return
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[seed] requests: %v", err)
		return
	}
	missReasons := []string{"Class conflict", "Doctor's appointment", "Family event", "Exam preparation", "Traveling"}
	overflowReasons := []string{"Need extra hours", "Covering a coworker", "Available to work more"}
	seeded := 0
	for _, id := range ids {
		var existing int
		if err := db.QueryRow(ctx,
			`SELECT COUNT(*) FROM schedule_requests WHERE user_id = $1`, id).Scan(&existing); err != nil {
			log.Printf("[seed] requests: %v", err)
			return
		}
		if existing > 0 {
			continue
		}
		for i := 0; i < 1+rand.Intn(2); i++ { // 1..2 requests
			typ := "miss"
			reason := missReasons[rand.Intn(len(missReasons))]
			if rand.Intn(2) == 0 {
				typ = "overflow"
				reason = overflowReasons[rand.Intn(len(overflowReasons))]
			}
			var wqID int
			var qerr error
			if typ == "miss" {
				qerr = db.QueryRow(ctx,
					`SELECT id FROM workqueue WHERE assigned_user_id = $1 LIMIT 1`, id).Scan(&wqID)
			} else {
				qerr = db.QueryRow(ctx, `
					SELECT w.id FROM workqueue w
					JOIN student_jobs sj ON sj.user_id = $1
					JOIN jobs j ON j.id = sj.job_id
					WHERE w.status = 'open' AND w.team_id = j.team_id
					LIMIT 1`, id).Scan(&wqID)
			}
			if errors.Is(qerr, pgx.ErrNoRows) {
				continue
			}
			if qerr != nil {
				log.Printf("[seed] requests: %v", qerr)
				return
			}
			if _, err := db.Exec(ctx, `
				INSERT INTO schedule_requests (user_id, workqueue_id, type, status, reason)
				VALUES ($1, $2, $3, 'pending', $4)`, id, wqID, typ, reason); err != nil {
				log.Printf("[seed] requests: %v", err)
				return
			}
			seeded++
		}
	}
	if seeded > 0 {
		log.Printf("[seed] seeded %d schedule requests", seeded)
	}
}

// Dev-only: seeds a few date-specific job_holidays in the next two weeks so the
// job requirement view shows closures. Idempotent — jobs that already have
// holidays are skipped.
func seedJobHolidays(ctx context.Context) {
	if cfg.Env != "development" {
		return
	}
	rows, err := db.Query(ctx, `SELECT id FROM jobs`)
	if err != nil {
		log.Printf("[seed] holidays: %v", err)
		return
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			log.Printf("[seed] holidays: %v", err)
			return
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[seed] holidays: %v", err)
		return
	}
	reasons := []string{"Thanksgiving", "Staff training day", "Maintenance closure", "Holiday", "Inventory day"}
	monday := weekMondayOf(time.Now())
	seeded := 0
	for _, id := range ids {
		var existing int
		if err := db.QueryRow(ctx,
			`SELECT COUNT(*) FROM job_holidays WHERE job_id = $1`, id).Scan(&existing); err != nil {
			log.Printf("[seed] holidays: %v", err)
			return
		}
		if existing > 0 {
			continue
		}
		if rand.Intn(2) == 0 { // only about half the jobs get a closure
			continue
		}
		date := monday.AddDate(0, 0, rand.Intn(14))
		reason := reasons[rand.Intn(len(reasons))]
		if _, err := db.Exec(ctx, `
			INSERT INTO job_holidays (job_id, date, reason) VALUES ($1, $2, $3)`,
			id, date, reason); err != nil {
			log.Printf("[seed] holidays: %v", err)
			return
		}
		seeded++
	}
	if seeded > 0 {
		log.Printf("[seed] seeded %d job holidays", seeded)
	}
}

// Dev-only: seeds preset weekly_schedules templates for each student (2-4
// weekday slots, ~10-16h total). Idempotent — students who already have a
// template are skipped.
func seedWeeklySchedules(ctx context.Context) {
	if cfg.Env != "development" {
		return
	}
	rows, err := db.Query(ctx, `SELECT id FROM users WHERE role = 'student'`)
	if err != nil {
		log.Printf("[seed] weekly: %v", err)
		return
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			log.Printf("[seed] weekly: %v", err)
			return
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[seed] weekly: %v", err)
		return
	}
	seeded := 0
	for _, id := range ids {
		var existing int
		if err := db.QueryRow(ctx,
			`SELECT COUNT(*) FROM weekly_schedules WHERE user_id = $1`, id).Scan(&existing); err != nil {
			log.Printf("[seed] weekly: %v", err)
			return
		}
		if existing > 0 {
			continue
		}
		total := 0
		for total < 10 {
			length := 2 + rand.Intn(3) // 2..4 hours
			if total+length > 16 {
				length = 16 - total
				if length < 2 {
					break
				}
			}
			startHour := 8 + rand.Intn(22-8-length+1) // keep end <= 22:00
			dow := 1 + rand.Intn(5)                   // Mon..Fri
			start := fmt.Sprintf("%02d:00", startHour)
			end := fmt.Sprintf("%02d:00", startHour+length)
			if _, err := db.Exec(ctx, `
				INSERT INTO weekly_schedules (user_id, day_of_week, start_time, end_time)
				VALUES ($1, $2, $3, $4)`, id, dow, start, end); err != nil {
				log.Printf("[seed] weekly: %v", err)
				return
			}
			total += length
		}
		seeded++
	}
	if seeded > 0 {
		log.Printf("[seed] seeded weekly schedules for %d students", seeded)
	}
}
