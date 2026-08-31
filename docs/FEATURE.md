# Features

- **Auth** — JWT login, scrypt password hashing, email verification, password
  reset, resend-verification, self-service change-password
- **RBAC** — `client` < `staff` < `admin` roles with role-gated routes;
  promotion is CLI-only so there's no self-service escalation
- **Onboarding gates** — forced password change and required profile block
  API access until completed
- **Admin user management** — create, delete, verify/unverify, change role,
  and trigger password resets from the dashboard
- **Email queue** — Postgres-backed queue with a bounded-retry worker; logs to
  stdout when no SMTP is configured, so dev needs no mail server
- **Enumeration-safe endpoints** — generic responses on signup/forgot-password
  so the API can't be used to probe registered emails
- **Server-side proxy** — the browser only talks to Next.js; `/api/*` is
  forwarded to the Go API, so it's never exposed directly
- **Theming** — UT Austin navy/orange, light and dark variants that follow the
  system setting
