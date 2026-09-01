// Sets an existing user's role. There's no HTTP endpoint for this on purpose —
// admin/staff are granted out-of-band (usage: set-role <email> <role>).
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Mirrors common/roles.js.
var roles = []string{"student", "staff", "manager", "scheduler", "admin"}

const defaultDatabaseURL = "postgres://postgres:postgres@localhost:5432/go_template?sslmode=disable"

func main() {
	if len(os.Args) != 3 || !validRole(os.Args[2]) {
		fmt.Fprintf(os.Stderr, "Usage: set-role <email> <%s>\n", strings.Join(roles, "|"))
		os.Exit(1)
	}
	email, role := os.Args[1], os.Args[2]

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = defaultDatabaseURL
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to set role:", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	var exists bool
	err = conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE email = $1)`, email).Scan(&exists)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to set role:", err)
		os.Exit(1)
	}
	if !exists {
		fmt.Fprintf(os.Stderr, "No user found with email %s\n", email)
		os.Exit(1)
	}
	if _, err := conn.Exec(ctx, `UPDATE users SET role = $1 WHERE email = $2`, role, email); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to set role:", err)
		os.Exit(1)
	}
	fmt.Printf("%s is now %s\n", email, role)
}

func validRole(role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}
