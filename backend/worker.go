package main

import (
	"context"
	"time"
)

// workerPolicy captures a worker's weekly-hour rules.
//
//	student  -> regular 20, no overtime, hard cap 20.
//	fulltime -> regular 40, overtime >40 allowed (with manager approval), cap 60.
//	hourly   -> regular = manager-set limit (default 40, can be < 40); overtime
//	           over 40 allowed (with manager approval), cap 60.
type workerPolicy struct {
	regular  float64 // hours that schedule normally without approval
	overtime bool    // whether hours beyond regular are allowed with approval
	cap      float64 // hard weekly cap (no assignment beyond this)
}

func workerPolicyFor(workerType string, hourlyLimit int) workerPolicy {
	switch workerType {
	case "fulltime":
		return workerPolicy{40, true, 60}
	case "hourly":
		reg := 40.0
		if hourlyLimit > 0 && hourlyLimit <= 40 {
			reg = float64(hourlyLimit)
		}
		return workerPolicy{reg, true, 60}
	default: // "student" or anything unrecognized
		return workerPolicy{20, false, 20}
	}
}

// workerSettings loads a worker's type and manager-set hourly limit.
func workerSettings(ctx context.Context, userID int) (string, int, error) {
	var wt string
	var hl *int
	if err := db.QueryRow(ctx,
		`SELECT worker_type, hourly_limit FROM users WHERE id = $1`, userID).Scan(&wt, &hl); err != nil {
		return "", 0, err
	}
	val := 0
	if hl != nil {
		val = *hl
	}
	return wt, val, nil
}

// workerWeekHours is the worker's total assigned hours for the week containing
// ref, across all jobs and teams.
func workerWeekHours(ctx context.Context, userID int, ref time.Time) (float64, error) {
	var hours float64
	monday := weekMondayOf(ref)
	err := db.QueryRow(ctx, `
		SELECT COALESCE(SUM(EXTRACT(EPOCH FROM (end_time - start_time)) / 3600), 0)
		FROM workqueue
		WHERE assigned_user_id = $1 AND date >= $2 AND date < $3`,
		userID, monday, monday.AddDate(0, 0, 7)).Scan(&hours)
	return hours, err
}

// weekMondayOf returns the Monday (midnight) of the week containing t.
func weekMondayOf(t time.Time) time.Time {
	offset := (int(t.Weekday()) + 6) % 7 // days since Monday
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).AddDate(0, 0, -offset)
}

// allowanceFor decides whether a worker may take on shiftHours more hours in the
// week containing ref. Returns:
//
//	"allowed" - within regular hours, assign immediately.
//	"pending" - within the hard cap but past regular hours -> needs manager
//	            approval (overtime for staff).
//	"blocked" - would exceed the hard weekly cap.
func allowanceFor(ctx context.Context, userID int, shiftHours float64, ref time.Time) (string, error) {
	wt, hl, err := workerSettings(ctx, userID)
	if err != nil {
		return "", err
	}
	pol := workerPolicyFor(wt, hl)
	used, err := workerWeekHours(ctx, userID, ref)
	if err != nil {
		return "", err
	}
	total := used + shiftHours
	if total > pol.cap {
		return "blocked", nil
	}
	if total > pol.regular {
		if pol.overtime {
			return "pending", nil
		}
		return "blocked", nil
	}
	return "allowed", nil
}

// workerTypeLabel returns a human label for a worker_type value.
func workerTypeLabel(workerType string) string {
	switch workerType {
	case "fulltime":
		return "Full-time staff"
	case "hourly":
		return "Hourly staff"
	default:
		return "Student"
	}
}
