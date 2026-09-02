# Scheduling

Employee scheduling and HR system for a university. Workers request time off and shifts; managers approve and staff the schedule; scope of visibility narrows or widens by role.

## Language

**Role**:
A named rank in the linear permission ladder a user holds: `student < staff < manager < admin`. Each rank includes everything the rank below it grants, plus more. `users.roles` holds every role; `users.role` is the generated highest rank.

**Manager**:
The single manager role. Authority differences between managers are **scope**, not rank: a manager is the `manager_id` on one or more Teams (team scope) and/or one or more Departments (department scope — which covers every team under it). Creating teams, jobs, or workers requires department scope. The former `manager` (department-scoped) and `scheduler` (team-scoped) roles collapsed into this one role.
_Avoid_: Team Manager / Department Manager as role names (they are scopes); Scheduler (old term)

**Team**:
The smallest scoped group of employees, managed day-to-day by a manager. Was previously called "Department" in this codebase.
_Avoid_: Department (old term, now means the level above Team)

**Department**:
A group of Teams, overseen by a manager. Was previously called "Location" in this codebase.
_Avoid_: Location (old term)

**Worker**:
An employee who holds shifts: a user with role `staff` or `student`, belonging to one or more Teams via `worker_teams`. Jobs (`student_jobs`) are the work-function layer on top, carrying per-job hour caps; a worker with no active job is still visible to their teams' managers.
_Avoid_: Student (bare)

**Student Worker**:
A student employed by the university. Same permission set as Staff (self view/edit only); kept as a separate, lower role for future divergence in work-function rules.
_Avoid_: Student (bare)

**Staff**:
A non-manager employee. Part-Time (hourly) and Full-Time (40+ with overtime approval) are `worker_type` values on Staff, not separate roles — their permissions are identical.

**Admin**:
Global scope. Only role that can delete employees, manage roles, or change system settings.

## Permission spec

Documents what each role/scope grants; not a runtime permission-check system — enforcement stays the rank-based `hasRole` check against the ladder above, plus scope checks against the `manager_id` columns.

| Permission                                   | Student | Staff | Manager (team scope) | Manager (dept scope) | Admin |
| -------------------------------------------- | :-----: | :---: | :------------------: | :------------------: | :---: |
| employee.view.self / employee.edit.self      |    ✓    |   ✓   |          ✓           |          ✓           |   ✓   |
| team.view / team.manage / team.assign        |    —    |   —   |     ✓ own teams      | ✓ department's teams |   ✓   |
| hr.approve                                   |    —    |   —   |          ✓           |          ✓           |   ✓   |
| hr.view                                      |    —    |   —   |      own teams       |   department-wide    |   ✓   |
| team.create / employee.create                |    —    |   —   |          —           |          ✓           |   ✓   |
| employee.delete                              |    —    |   —   |          —           |          —           |   ✓   |
| system.roles.manage / system.settings.manage |    —    |   —   |          —           |          —           |   ✓   |

**Known future wrinkle**: some managers may not need scheduling or approval capability. Deferred — no per-user capability flag exists yet; add one when a real manager needs it rather than building it speculatively.
