# Features

- **Auth** — JWT login, scrypt password hashing, email verification, password
  reset, resend-verification, self-service change-password
- **RBAC** — `student` < `staff` < `manager` < `admin` roles with role-gated
  routes; promotion is CLI-only so there's no self-service escalation. There is
  a single `manager` role: scope comes from `departments.manager_id` /
  `teams.manager_id` (a manager may hold several of each; department scope
  covers the teams under it), and department-level actions (creating teams,
  jobs, workers) additionally require managing ≥1 department.
- **Coverage calendar** — managers/admins get a scope-wide week grid
  (`/calendar`) showing every shift in their teams color-coded by worker, with
  unassigned workqueue shifts shown as open slots.
- **Onboarding gates** — forced password change and required profile block
  API access until completed
- **Admin user management** — create, delete, verify/unverify, change role,
  and trigger password resets from the dashboard
- **Email queue** — Postgres-backed queue with a bounded-retry worker; logs to
  stdout when no SMTP is configured, so dev needs no mail server
- **Audit trail** — every request/shift/job action (create, cancel, approve,
  deny, assign, unassign, pick) is recorded in an append-only `audit_log`
  table, scoped to a team. The Audit Report page (`/audit`, in the menu
  bar) shows the last 15 days: managers see every team in their departments
  and their own teams, admins everything (`GET /api/audit` enforces
  the same scoping), with CSV export (`GET /api/audit/export`). The coverage
  calendar's assign modal shows each shift's own recent audit entries.
- **Enumeration-safe endpoints** — generic responses on signup/forgot-password
  so the API can't be used to probe registered emails
- **Server-side proxy** — the browser only talks to Next.js; `/api/*` is
  forwarded to the Go API, so it's never exposed directly
- **Theming** — UT Austin navy/orange, light and dark variants that follow the
  system setting
