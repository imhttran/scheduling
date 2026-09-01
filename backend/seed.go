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
		case "student", "staff":
			if deptID != 0 {
				// Ensure a job exists for this department (default: named after it),
				// then assign the worker to it so they see the workqueue and can be
				// assigned shifts.
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
				if err := insertDefaultJobSchedules(ctx, jobID); err != nil {
					log.Printf("[seed] %s job schedules: %v", email, err)
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

// Dev-only: fills the workqueue with open shifts for this week and next week,
// one per job's daily operating hours. Idempotent — existing shifts are kept.
func seedWorkqueue(ctx context.Context) {
	if cfg.Env != "development" {
		return
	}
	rows, err := db.Query(ctx, `
		SELECT j.department_id, js.day_of_week, js.start_time, js.end_time
		FROM jobs j
		JOIN job_schedules js ON js.job_id = j.id`)
	if err != nil {
		log.Printf("[seed] workqueue: %v", err)
		return
	}
	defer rows.Close()
	type shift struct {
		deptID, dow int
		start, end  string
	}
	var shifts []shift
	for rows.Next() {
		var s shift
		if err := rows.Scan(&s.deptID, &s.dow, &s.start, &s.end); err != nil {
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
				INSERT INTO workqueue (department_id, date, start_time, end_time, status)
				SELECT $1, $2, $3, $4, 'open'
				WHERE NOT EXISTS (
					SELECT 1 FROM workqueue WHERE department_id = $1 AND date = $2 AND start_time = $3)`,
				s.deptID, date, s.start, s.end)
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
	id, dept, week int
	hours          float64
}

// assignWorker is a worker eligible to take shifts, with their weekly cap.
type assignWorker struct {
	id    int
	depts map[int]bool
	cap   float64
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
		// Candidate workers in this department with weekly capacity.
		var candidates []int
		for _, wi := range order {
			w := workers[wi]
			if !w.depts[s.dept] {
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
		SELECT sj.user_id, j.department_id
		FROM student_jobs sj JOIN jobs j ON j.id = sj.job_id
		WHERE sj.active`)
	if err != nil {
		log.Printf("[seed] assignments: %v", err)
		return
	}
	workers := map[int]*assignWorker{}
	var order []int
	for rows.Next() {
		var uid, dept int
		if err := rows.Scan(&uid, &dept); err != nil {
			rows.Close()
			log.Printf("[seed] assignments: %v", err)
			return
		}
		w, ok := workers[uid]
		if !ok {
			w = &assignWorker{id: uid, depts: map[int]bool{}}
			workers[uid] = w
			order = append(order, uid)
		}
		w.depts[dept] = true
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
		SELECT id, department_id, date, start_time, end_time FROM workqueue
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
		if err := srows.Scan(&s.id, &s.dept, &date, &start, &end); err != nil {
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
	plan := planAssignments(shifts, workerList, 0.8)

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
	var deptID int
	err = db.QueryRow(ctx, `
		SELECT j.department_id FROM student_jobs sj JOIN jobs j ON j.id = sj.job_id
		WHERE sj.user_id = $1 LIMIT 1`, staffID).Scan(&deptID)
	if errors.Is(err, pgx.ErrNoRows) {
		var jobID int
		err = db.QueryRow(ctx, `
			INSERT INTO jobs (name, department_id)
			SELECT 'Staff Work', id FROM departments ORDER BY id LIMIT 1
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
	}
}

// Dev-only: converts a few students into staff (full-time and part-time) so the
// staff screen and worker management have variety.
func seedStaffConversions(ctx context.Context) {
	if cfg.Env != "development" {
		return
	}
	limit := func(n int) *int { return &n }
	conversions := []struct {
		email       string
		workerType  string
		hourlyLimit *int
	}{
		{"fulltime1@mail.edu", "fulltime", nil},
		{"hourly2@mail.edu", "hourly", limit(20)},
		{"fulltime3@mail.edu", "fulltime", nil},
		{"hourly4@mail.edu", "hourly", limit(15)},
	}
	for _, c := range conversions {
		if _, err := db.Exec(ctx, `
			UPDATE users SET role = 'staff', worker_type = $1, hourly_limit = $2
			WHERE email = $3`, c.workerType, c.hourlyLimit, c.email); err != nil {
			log.Printf("[seed] staff conversion %s: %v", c.email, err)
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
					WHERE w.status = 'open' AND w.department_id = j.department_id
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
