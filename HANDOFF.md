# Handoff: Scheduling App — Manager Worker-Management Feature

## Goal

Full-stack scheduling app (Next.js frontend → Go API → PostgreSQL, see
`README.md`). Roles: `student` < `staff` < `manager` < `scheduler` < `admin`.
Managers/schedulers run per-location scheduling (jobs, shifts, workqueue,
requests). This session's work added **manager-side worker management**:
creating workers directly (instead of only via admin), and viewing a
worker's calendar/preferred-times from the manager UI.

## Current Progress

Committed as `e34fe5e` — "Add manager scheduling features and backend
endpoints" (clean working tree, nothing pending). Changes:

- **`backend/app.go`** — new routes: `POST /api/workers` (manager or admin),
  `GET /api/workers/{id}/calendar` (manager+), `GET /api/workers/{id}/preferences`
  (manager+).
- **`backend/users.go`** — `createWorker`: manager/admin creates a
  student-or-staff account (role hard-restricted to those two — no
  privilege-escalation path), scoped to the caller's location via
  `managerLocationID` + job-location `EXISTS` check for non-admins. Sets
  `email_verified=true, must_change_password=true` (manager vouches).
- **`backend/schedule.go`** — `workerCalendar` and `workerPreferences`:
  read-only, location-scoped the same way (IDOR-safe, matches the pattern
  used by every other manager-scoped handler in the file).
- **`backend/seed.go`** — `seedStaffConversions`: dev-only (`cfg.Env ==
"development"` guard, same convention as every other seeder), converts 4
  seeded students into staff so the staff screen has variety.
- **`backend/seed.go`** — `seedAssignments`: dev-only, fills ~80% of the
  workqueue (leaving ~20% open for new data) while respecting each worker's
  weekly cap (`worker_type`). The pure planner `planAssignments` is unit-tested
  in `backend/seed_test.go`.
- **`backend/main.go`** — wires `seedStaffConversions` and `seedAssignments`
  into the seed sequence (conversions run before assignments so converted staff
  use their real caps).
- **`frontend/app/manager/page.tsx`** — "Add Worker" modal (posts to
  `/api/workers`); worker Edit modal now also shows this-week's calendar and
  preferred times (loaded on open via the two new GET endpoints); the
  shift-assignment dropdown now filters out students who'd exceed their
  weekly hour cap (`hoursBetween` helper) instead of listing everyone.

## What Worked

- Full `/security-review` pass on the diff (identify sub-agent → would-be
  false-positive filter stage) found **zero high-confidence vulnerabilities**.
  New endpoints follow the codebase's established location-scoping pattern
  (`managerLocationID` + `EXISTS` join on `student_jobs`/`jobs`/`departments`),
  all SQL is parameterized, `targetID(r)` is only reachable after `parseID`
  middleware validates it's numeric, role assignment in `createWorker` can't
  reach `manager`/`scheduler`/`admin`, and the frontend has no
  `dangerouslySetInnerHTML` — plain JSX interpolation only.
- Comparing new manager-scoped handlers against existing ones
  (`assignStudentJob` et al.) in `schedule.go` was the fast way to confirm
  the IDOR-prevention pattern was replicated correctly — worth doing that
  comparison first on any future addition of a manager/scheduler-scoped
  endpoint here.

## What Didn't Work

Nothing notable failed this session — the review turned up no rework
needed.

## Next Steps

- No open backend/security items. If continuing UI work: the "Add Worker"
  form doesn't validate `minHours <= maxHours` client- or server-side
  (business-logic gap, not a security issue) — worth a guard if it comes up
  as a real bug.
- `docs/FEATURE.md` hasn't been updated to mention manager-initiated worker
  creation or the calendar/preferences views — consider adding a line if
  docs are meant to stay in sync with `app.go`'s route list.
- Nothing is staged/uncommitted; start fresh from `e34fe5e` on `main`.
