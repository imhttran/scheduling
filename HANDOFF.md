# Handoff: Scheduling App — Manager Worker-Management Feature

> **Historical** — this describes the pre-rework RBAC (`manager`/`scheduler`
> roles, assignment side-tables, `locations`/`departments` tables, `public`
> schema). The current model is documented in `CONTEXT.md`,
> `docs/adr/0002-single-manager-role-scope-as-data.md`, and
> `docs/RBAC-REWORK.md`.

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
- **`backend/seed.go`** — `seedExtraData`: dev-only, reads `jobs.csv` (extra
  jobs) and `staff.csv` (full-time staff) on top of the one-per-department
  defaults. Replaces the old `seedJobs` + `seedFulltimeStaff` functions.
- **`backend/seed.go`** — `seedAssignments`: dev-only, fills ~90% of the
  workqueue (leaving ~10% open for new data) while respecting each worker's
  weekly cap (`worker_type`). The pure planner `planAssignments` is unit-tested
  in `backend/seed_test.go`.
- **`backend/seed.csv`** — now carries `worker_type`/`hourly_limit` columns, so
  the four full-time/hourly staff are created as staff directly (the old
  `seedStaffConversions` post-hoc conversion is gone).
- **`backend/main.go`** — wires `seedExtraData` and `seedAssignments` into the
  seed sequence.
- **`frontend/app/manager/page.tsx`** — "Add Worker" modal (posts to
  `/api/workers`); worker Edit modal now also shows this-week's calendar and
  preferred times (loaded on open via the two new GET endpoints); the
  shift-assignment dropdown now filters out students who'd exceed their
  weekly hour cap (`hoursBetween` helper) instead of listing everyone.

## Scheduler Department Scoping

Schedulers are now scoped to a single department (not just a location): one
scheduler is seeded per department, and each only sees/manages that
department's schedule.

- **`backend/migrations/008_scheduler_assignments.sql`** — new
  `scheduler_assignments (user_id, department_id)` table; embedded and applied
  as migration 8 in `main.go`.
- **`backend/seed.csv`** — 6 schedulers, one per department (dining, facility,
  residential life, business service, marketing, communication).
- **`backend/seed.go`** — `seedFromCSV` inserts a `scheduler_assignments` row
  for scheduler rows (in addition to the location assignment).
- **`backend/schedule.go`** — `schedulerDepartmentID` helper; schedulers are
  department-scoped (managers stay location-scoped, admins exempt) across
  `staffWorkqueue`, `assignWorkqueueShift`, `createWorkqueueShift`,
  `listRequests`, `approveRequest`/`denyRequest` (via `requestInCallerLocation`),
  `listDepartments`, and `listStudents`.
- **`frontend/app/manager/page.tsx`** — workqueue hint reads "All slots in your
  department" for schedulers.
- **`backend/app_test.go`** — `ensureTestSchema` now applies all migrations
  (1–8); `loginAs` completes the 2FA flow (dev code `1234`) so integration
  tests run against a fresh DB; `TestSchedulerDepartmentScoping` verifies a
  scheduler only sees/assigns shifts in their department.

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
