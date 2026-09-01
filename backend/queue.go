package main

// Port of emailQueue.js: the DB-backed queue (rows built in mail.go, enqueue
// helpers used by auth.go/users.go) and the polling worker.

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Prisma's P2025 (record not found in an update) as a sentinel.
var errNotFound = errors.New("user not found")

const resetTokenTTL = time.Hour

// Shared by /api/forgot-password (self-service, keyed by email) and the
// admin-triggered reset route (keyed by id). One transaction: the update
// itself both finds the user (errNotFound if it doesn't match) and sets the
// token, so callers don't need their own lookup+404 check.
func queuePasswordReset(ctx context.Context, whereCol string, arg any) error {
	token := randomToken()
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var email string
	err = tx.QueryRow(ctx, `
		UPDATE users SET reset_token = $1, reset_token_expiry = $2
		WHERE `+whereCol+` = $3
		RETURNING email`,
		token, time.Now().Add(resetTokenTTL), arg).
		Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	row := passwordResetEmail(email, tokenLink("reset-password", token))
	if _, err := tx.Exec(ctx,
		`INSERT INTO email_queue ("to", subject, body) VALUES ($1, $2, $3)`,
		row.To, row.Subject, row.Body); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Shared by /api/resend-verification (self-service) and the staff-triggered resend route.
func queueVerificationEmail(ctx context.Context, userID int, email string) error {
	token := randomToken()
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`UPDATE users SET verification_token = $1 WHERE id = $2`, token, userID); err != nil {
		return err
	}
	row := verificationEmail(email, tokenLink("verify", token))
	if _, err := tx.Exec(ctx,
		`INSERT INTO email_queue ("to", subject, body) VALUES ($1, $2, $3)`,
		row.To, row.Subject, row.Body); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Pick up pending emails, send them, mark sent / retry with a bounded cap.
func processEmailQueue(ctx context.Context, take int) int {
	rows, err := db.Query(ctx, `
		SELECT id, "to", subject, body, attempts
		FROM email_queue
		WHERE status = 'pending' AND attempts < $1
		ORDER BY created_at ASC
		LIMIT $2`, cfg.MaxAttempts, take)
	if err != nil {
		// Tables missing (e.g. the DB was reset while this process was running) —
		// recreate the schema instead of erroring every poll.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			log.Println("[emailQueue] schema missing, re-running migrations")
			migrate(ctx)
			return 0
		}
		log.Println("[emailQueue] worker error:", err)
		return 0
	}
	defer rows.Close()
	var jobs []struct {
		id       int
		to       string
		subject  string
		body     string
		attempts int
	}
	for rows.Next() {
		var job struct {
			id       int
			to       string
			subject  string
			body     string
			attempts int
		}
		if err := rows.Scan(&job.id, &job.to, &job.subject, &job.body, &job.attempts); err != nil {
			return len(jobs)
		}
		jobs = append(jobs, job)
	}
	rows.Close()
	for _, job := range jobs {
		if err := sendMail(job.to, job.subject, job.body); err != nil {
			attempts := job.attempts + 1
			status := "pending"
			// ponytail: no backoff; fixed-interval poll is the retry. Add exponential backoff if a slow mailer causes stampedes.
			if attempts >= cfg.MaxAttempts {
				status = "failed"
			}
			_, _ = db.Exec(ctx,
				`UPDATE email_queue SET attempts = $1, last_error = $2, status = $3 WHERE id = $4`,
				attempts, err.Error(), status, job.id)
			continue
		}
		_, _ = db.Exec(ctx,
			`UPDATE email_queue SET status = 'sent', sent_at = now() WHERE id = $1`, job.id)
	}
	return len(jobs)
}

// Polling worker.
func startEmailWorker(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			processEmailQueue(ctx, 10)
		}
	}
}
