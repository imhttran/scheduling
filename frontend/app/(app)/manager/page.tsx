"use client";

import { useCallback, useEffect, useState, type FormEvent } from "react";
import { API_BASE, callApi } from "@/lib/api";
import { PageHeader } from "@/components/PageHeader";
import { PageFooter } from "@/components/PageFooter";
import { PageTitle } from "@/components/PageTitle";
import { Modal } from "@/components/Modal";
import { JobModal, type JobInput } from "@/components/JobModal";
import { AIAssistantPanel } from "@/components/AIAssistantPanel";
import { fmtTime } from "@/components/WeekCalendar";
import { hasAnyRole, hasRole } from "@/lib/roles";
import { useSortablePage } from "@/lib/pagination";
import { DataGrid, type Column } from "@/components/DataGrid";
import type {
  Job,
  OrgDepartment,
  Preference,
  Request,
  Shift,
  Team,
} from "@/lib/types";

type WorkerJob = {
  jobId: number;
  jobName: string;
  teamId: number;
  teamName: string;
  departmentId: number;
  minHours: number;
  maxHours: number;
  active: boolean;
};

type Worker = {
  id: number;
  email: string;
  disabled: boolean;
  workerType: string;
  workerTypeLabel: string;
  hourlyLimit: number;
  weekHoursCap: number;
  weekHoursUsed: number;
  jobs: WorkerJob[];
};

const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

const hoursBetween = (start: string, end: string) => {
  const [sh, sm] = start.split(":").map(Number);
  const [eh, em] = end.split(":").map(Number);
  return (eh * 60 + em - (sh * 60 + sm)) / 60;
};

// Org chart node label: profile name when set, else the email.
const managerLabel = (email?: string | null, name?: string | null) =>
  name || email || "No manager assigned";

export default function ManagerPage() {
  const [token, setToken] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<string>("");
  const [roles, setRoles] = useState<string[]>([]);
  const [workers, setWorkers] = useState<Worker[]>([]);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [shifts, setShifts] = useState<Shift[]>([]);
  const [teams, setTeams] = useState<Team[]>([]);
  const [org, setOrg] = useState<OrgDepartment[]>([]);
  const [requests, setRequests] = useState<Request[]>([]);
  const [workerEditModal, setWorkerEditModal] = useState<Worker | null>(null);
  const [jobModal, setJobModal] = useState<Job | null>(null);
  const [workqueueModal, setWorkqueueModal] = useState(false);
  const [workerModal, setWorkerModal] = useState(false);
  const [workerShifts, setWorkerShifts] = useState<Shift[]>([]);
  const [workerPrefs, setWorkerPrefs] = useState<Preference[]>([]);

  // Any manager (or admin) can assign shifts; the server scopes each manager
  // to their own departments/teams.
  const canAssign = hasRole(role, "manager");
  // Managers and admins create/edit jobs; the server enforces the
  // department-level rule. Membership check so any multi-role holder gets
  // job management.
  const canManageJobs = hasAnyRole(roles, "manager", "admin");

  const workersGrid = useSortablePage<Worker>(workers, (s) => s.email, "email");
  const jobsGrid = useSortablePage<Job>(
    jobs,
    (j, key) => {
      if (key === "workers") return j.currentWorkers;
      return j[key as keyof Job];
    },
    "name",
  );
  const shiftsGrid = useSortablePage<Shift>(
    shifts,
    (sh, key) => sh[key as keyof Shift],
    "date",
  );
  const requestsGrid = useSortablePage<Request>(
    requests,
    (r, key) => r[key as keyof Request],
    "date",
  );

  const workerColumns: Column<Worker>[] = [
    { key: "email", label: "Email", sortable: true },
    { key: "workerTypeLabel", label: "Type", sortable: true },
    {
      key: "weekHoursUsed",
      label: "Hours",
      render: (s) => `${s.weekHoursUsed} / ${s.weekHoursCap} hrs`,
    },
    {
      key: "jobs",
      label: "Jobs",
      render: (s) =>
        s.jobs.length === 0 ? "—" : s.jobs.map((j) => j.jobName).join(", "),
    },
    {
      label: "",
      render: (s) => (
        <button
          type="button"
          onClick={() => {
            setWorkerEditModal(s);
            loadWorkerCalendar(s.id);
            loadWorkerPrefs(s.id);
          }}
        >
          Edit
        </button>
      ),
    },
  ];

  const jobColumns: Column<Job>[] = [
    { key: "name", label: "Job", sortable: true },
    { key: "teamName", label: "Team", sortable: true },
    ...DAYS.map((d, dow) => ({
      label: d,
      render: (j: Job) => {
        const s = j.schedules.find((x) => x.dayOfWeek === dow);
        return s ? `${fmtTime(s.startTime)}–${fmtTime(s.endTime)}` : "—";
      },
    })),
    {
      key: "weeklyHours",
      label: "Weekly",
      sortable: true,
      render: (j) => `${j.weeklyHours} hrs`,
    },
    {
      key: "workers",
      label: "Workers",
      sortable: true,
      render: (j) => `${j.currentWorkers} / ${j.optimalWorkers}`,
    },
    ...(canManageJobs
      ? [
          {
            label: "",
            render: (j: Job) => (
              <button type="button" onClick={() => setJobModal(j)}>
                Edit
              </button>
            ),
          },
        ]
      : []),
  ];

  const shiftColumns: Column<Shift>[] = [
    { key: "teamName", label: "Team", sortable: true },
    { key: "date", label: "Date", sortable: true },
    {
      key: "startTime",
      label: "Time",
      sortable: true,
      render: (sh) => `${fmtTime(sh.startTime)}–${fmtTime(sh.endTime)}`,
    },
    { key: "status", label: "Status", sortable: true },
    {
      key: "assignedEmail",
      label: "Assigned Worker",
      sortable: true,
      render: (sh) => sh.assignedEmail ?? "—",
    },
    ...(canAssign
      ? [
          {
            label: "",
            render: (sh: Shift) => (
              <form
                className="schedule-row assign-form"
                onSubmit={(e) => assignShift(e, sh.id)}
              >
                <select name="userId" defaultValue={sh.assignedUserId ?? ""}>
                  <option value="">Unassigned</option>
                  {workers
                    .filter(
                      (st) =>
                        st.id === sh.assignedUserId ||
                        st.weekHoursUsed +
                          hoursBetween(sh.startTime, sh.endTime) <=
                          st.weekHoursCap,
                    )
                    .map((st) => (
                      <option key={st.id} value={st.id}>
                        {st.email} ({st.weekHoursUsed}/{st.weekHoursCap}h)
                      </option>
                    ))}
                </select>
                <button type="submit" className="login-button">
                  Assign
                </button>
              </form>
            ),
          },
        ]
      : []),
  ];

  const requestColumns: Column<Request>[] = [
    { key: "email", label: "Worker", sortable: true },
    { key: "date", label: "Date", sortable: true },
    {
      key: "startTime",
      label: "Time",
      sortable: true,
      render: (r) => `${fmtTime(r.startTime)}–${fmtTime(r.endTime)}`,
    },
    { key: "type", label: "Type", sortable: true },
    {
      key: "reason",
      label: "Reason",
      sortable: true,
      render: (r) => r.reason ?? "—",
    },
    {
      label: "",
      render: (r) => (
        <>
          <button
            type="button"
            onClick={() => act(`/api/requests/${r.id}/approve`)}
          >
            Approve
          </button>
          <button
            type="button"
            onClick={() => act(`/api/requests/${r.id}/deny`)}
          >
            Deny
          </button>
        </>
      ),
    },
  ];

  const load = useCallback(async (authToken: string) => {
    const [w, j, sh, t, r, o] = await Promise.all([
      callApi<{ workers: Worker[] }>(
        authToken,
        "/api/workers",
        "GET",
        undefined,
        false,
      ),
      callApi<{ jobs: Job[] }>(authToken, "/api/jobs", "GET", undefined, false),
      callApi<{ shifts: Shift[] }>(
        authToken,
        "/api/staff/workqueue",
        "GET",
        undefined,
        false,
      ),
      callApi<{ teams: Team[] }>(
        authToken,
        "/api/teams",
        "GET",
        undefined,
        false,
      ),
      callApi<{ requests: Request[] }>(
        authToken,
        "/api/requests",
        "GET",
        undefined,
        false,
      ),
      callApi<{ org: OrgDepartment[] }>(
        authToken,
        "/api/org",
        "GET",
        undefined,
        false,
      ),
    ]);
    if (w) setWorkers(w.workers ?? []);
    if (j) setJobs(j.jobs ?? []);
    if (sh) setShifts(sh.shifts ?? []);
    if (t) setTeams(t.teams ?? []);
    if (r) setRequests(r.requests ?? []);
    if (o) setOrg(o.org ?? []);
  }, []);

  useEffect(() => {
    (async () => {
      const stored = localStorage.getItem("auth_token");
      if (!stored) {
        window.location.href = "/";
        return;
      }
      setToken(stored);
      const me = await callApi<{
        user: { email: string; role: string; roles?: string[] };
      }>(stored, "/api/me", "GET", undefined, false);
      if (!me) {
        localStorage.removeItem("auth_token");
        window.location.href = "/";
        return;
      }
      if (me.user.role !== "manager" && me.user.role !== "admin") {
        window.location.href = "/dashboard";
        return;
      }
      setEmail(me.user.email);
      setRole(me.user.role);
      setRoles(me.user.roles ?? [me.user.role]);
      await load(stored);
    })();
  }, [load]);

  const act = (
    path: string,
    body?: unknown,
    method: "POST" | "PATCH" | "DELETE" = "POST",
  ) => {
    if (!token) return;
    void (async () => {
      const result = await callApi(token, path, method, body);
      if (result) await load(token);
    })();
  };

  const submit = (event: FormEvent<HTMLFormElement>, path: string) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const body: Record<string, unknown> = {};
    for (const [k, v] of data.entries()) body[k] = v;
    act(path, body);
    event.currentTarget.reset();
  };

  const saveAssignment = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!workerEditModal) return;
    const data = new FormData(event.currentTarget);
    const body: Record<string, unknown> = {};
    for (const [k, v] of data.entries()) body[k] = v;
    // Backend expects numbers; FormData yields strings.
    body.jobId = Number(data.get("jobId"));
    body.minHours = Number(data.get("minHours"));
    body.maxHours = Number(data.get("maxHours"));
    act(`/api/workers/${workerEditModal.id}/jobs`, body);
  };

  const saveWorkerDetails = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!workerEditModal) return;
    const data = new FormData(event.currentTarget);
    const workerType = String(data.get("workerType"));
    let hourlyLimit = Number(data.get("hourlyLimit")) || 0;
    if (workerType !== "hourly") hourlyLimit = 0;
    act(
      `/api/users/${workerEditModal.id}/worker`,
      {
        workerType,
        hourlyLimit: hourlyLimit || null,
      },
      "PATCH",
    );
  };

  const saveWorker = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const body: Record<string, unknown> = {};
    for (const [k, v] of data.entries()) body[k] = v;
    body.jobId = Number(data.get("jobId"));
    body.minHours = Number(data.get("minHours"));
    body.maxHours = Number(data.get("maxHours"));
    act("/api/workers", body);
    setWorkerModal(false);
  };

  const removeJob = (jobId: number) => {
    if (!workerEditModal) return;
    act(
      `/api/workers/${workerEditModal.id}/jobs/${jobId}`,
      undefined,
      "DELETE",
    );
  };

  const loadWorkerCalendar = async (id: number) => {
    if (!token) return;
    const res = await callApi<{ calendar: Shift[] }>(
      token,
      `/api/workers/${id}/calendar`,
      "GET",
      undefined,
      false,
    );
    if (res) setWorkerShifts(res.calendar ?? []);
  };

  const loadWorkerPrefs = async (id: number) => {
    if (!token) return;
    const res = await callApi<{ preferences: Preference[] }>(
      token,
      `/api/workers/${id}/preferences`,
      "GET",
      undefined,
      false,
    );
    if (res) setWorkerPrefs(res.preferences ?? []);
  };

  const saveSchedule = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!workerEditModal) return;
    const data = new FormData(event.currentTarget);
    const body: Record<string, unknown> = {};
    for (const [k, v] of data.entries()) body[k] = v;
    body.jobId = Number(data.get("jobId"));
    body.dayOfWeek = Number(data.get("dayOfWeek"));
    act(`/api/workers/${workerEditModal.id}/schedule`, body);
    event.currentTarget.reset();
  };

  const saveWorkqueue = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const body: Record<string, unknown> = {};
    for (const [k, v] of data.entries()) body[k] = v;
    body.teamId = Number(data.get("teamId"));
    act("/api/workqueue", body);
    setWorkqueueModal(false);
  };

  const saveJob = (data: JobInput) => {
    const body: Record<string, unknown> = {
      name: data.name,
      teamId: data.teamId,
      optimalWorkers: data.optimalWorkers,
      schedules: data.schedules.map((s) => ({
        dayOfWeek: s.dayOfWeek,
        startTime: s.startTime,
        endTime: s.endTime,
      })),
      holidays: data.holidays.map((h) => ({
        date: h.date,
        reason: h.reason ?? "",
      })),
    };
    if (data.id) {
      act(`/api/jobs/${data.id}`, body, "PATCH");
    } else {
      act("/api/jobs", body);
    }
    setJobModal(null);
  };

  const assignShift = (event: FormEvent<HTMLFormElement>, shiftId: number) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const userId = Number(data.get("userId")) || 0;
    act(`/api/staff/workqueue/${shiftId}/assign`, { userId });
  };

  const logout = (event: { preventDefault: () => void }) => {
    event.preventDefault();
    localStorage.removeItem("auth_token");
    window.location.href = "/";
  };

  return (
    <div className="dashboard-container wide">
      <PageTitle title="Manager" />
      <PageHeader
        title="Manager"
        right={
          <>
            Welcome, <span className="highlight">{email}</span>
          </>
        }
      />

      <div className="with-sidebar">
        <nav className="sidebar">
          <a href="/calendar">Calendar view</a>
          <a href="#orgchart">Org Chart</a>
          <a href="#workers">Workers</a>
          <a href="#jobs">Job Requirements</a>
          <a href="#workqueue">Workqueue</a>
          <a href="#requests">Requests</a>
          <a href="/audit">Audit Report</a>
          <a className="logout-link" href="/" onClick={logout}>
            Logout
          </a>
        </nav>
        <div className="dashboard-card">
          <div className="user-list-section" id="orgchart">
            <h2>Org Chart</h2>
            <p className="section-hint">
              Departments and teams in your scope, with the manager responsible
              for each and the workers on every team.
            </p>
            {org.length === 0 ? (
              <p className="section-hint">No departments in your scope yet.</p>
            ) : (
              <div className="org-chart">
                {org.map((dept) => (
                  <div className="org-dept" key={dept.id}>
                    <div className="org-node org-dept-node">
                      <div className="org-name">{dept.name}</div>
                      <div className="org-role">Department</div>
                      <div className="org-manager">
                        {managerLabel(dept.managerEmail, dept.managerName)}
                        {dept.managerEmail === email && (
                          <span className="org-you">You</span>
                        )}
                      </div>
                    </div>
                    {dept.teams.length > 0 && (
                      <div className="org-teams">
                        {dept.teams.map((team) => (
                          <div className="org-node org-team-node" key={team.id}>
                            <div className="org-name">{team.name}</div>
                            <div className="org-role">Team</div>
                            <div className="org-manager">
                              {managerLabel(
                                team.managerEmail,
                                team.managerName,
                              )}
                              {team.managerEmail === email && (
                                <span className="org-you">You</span>
                              )}
                            </div>
                            <div className="org-count">
                              {team.workerCount} worker
                              {team.workerCount === 1 ? "" : "s"}
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className="user-list-section" id="workers">
            <div className="section-title-row">
              <h2>Workers</h2>
              {canManageJobs && (
                <button
                  type="button"
                  className="login-button add-button"
                  onClick={() => setWorkerModal(true)}
                >
                  Add Worker
                </button>
              )}
            </div>
            <p className="section-hint">
              Students and staff who work shifts. Students cap at 20 hrs/wk;
              full-time staff at 40 (+20 overtime); hourly staff at a
              manager-set limit. Overtime needs manager approval.
            </p>
            <DataGrid
              grid={workersGrid}
              columns={workerColumns}
              getRowKey={(s) => s.id}
              emptyText="No workers in your teams."
            />
          </div>

          <div className="user-list-section" id="jobs">
            <div className="section-title-row">
              <h2>Job Requirements</h2>
              {canManageJobs && (
                <button
                  type="button"
                  className="login-button add-button"
                  onClick={() =>
                    setJobModal({
                      id: 0,
                      name: "",
                      teamId: 0,
                      teamName: "",
                      departmentId: 0,
                      departmentName: "",
                      optimalWorkers: 1,
                      currentWorkers: 0,
                      weeklyHours: 0,
                      schedules: [],
                      holidays: [],
                    })
                  }
                >
                  Add Job
                </button>
              )}
            </div>
            <p className="section-hint">
              Daily operating hours, the weekly hour requirement, and staff
              coverage (current / optimal) for each job in your teams. Days with
              no hours are closed (weekend, holiday).
            </p>
            <DataGrid
              grid={jobsGrid}
              columns={jobColumns}
              getRowKey={(j) => j.id}
              emptyText="No jobs in your teams."
            />
          </div>

          <div className="user-list-section" id="workqueue">
            <div className="section-title-row">
              <h2>Workqueue</h2>
              <button
                type="button"
                className="login-button add-button"
                onClick={() => setWorkqueueModal(true)}
              >
                Add Shift
              </button>
            </div>
            <p className="section-hint">
              All slots in your teams.{" "}
              {canAssign
                ? "Assign a slot to a worker (or unassign it) to build the schedule."
                : "Open shifts students can pick from, or return missed shifts here."}
            </p>
            <DataGrid
              grid={shiftsGrid}
              columns={shiftColumns}
              getRowKey={(sh) => sh.id}
              emptyText="No shifts yet."
            />
          </div>

          <div className="user-list-section" id="requests">
            <h2>Pending Requests</h2>
            <DataGrid
              grid={requestsGrid}
              columns={requestColumns}
              getRowKey={(r) => r.id}
              emptyText="No pending requests."
            />
          </div>
        </div>
      </div>

      {workerEditModal && (
        <Modal
          title={`Edit ${workerEditModal.email}`}
          onClose={() => setWorkerEditModal(null)}
        >
          <div className="modal-form">
            <h4>This Week's Schedule</h4>
            {workerShifts.length === 0 ? (
              <p className="section-hint">No shifts assigned this week.</p>
            ) : (
              <ul className="job-list">
                {workerShifts.map((sh) => (
                  <li key={sh.id}>
                    <span>
                      {sh.date} · {fmtTime(sh.startTime)}–{fmtTime(sh.endTime)}{" "}
                      · {sh.teamName}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </div>

          <div className="modal-form">
            <h4>Preferred Times</h4>
            {workerPrefs.length === 0 ? (
              <p className="section-hint">No preferred times set.</p>
            ) : (
              <ul className="job-list">
                {workerPrefs.map((p) => (
                  <li key={p.id}>
                    <span>
                      {DAYS[p.dayOfWeek]} · {fmtTime(p.startTime)}–
                      {fmtTime(p.endTime)}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </div>

          <div className="modal-form">
            <h4>Current Jobs</h4>
            {workerEditModal.jobs.length === 0 ? (
              <p className="section-hint">No jobs assigned yet.</p>
            ) : (
              <ul className="job-list">
                {workerEditModal.jobs.map((j) => (
                  <li key={j.jobId}>
                    <span>
                      {j.jobName}{" "}
                      <em>
                        ({j.teamName} · {j.minHours}–{j.maxHours}h)
                      </em>
                    </span>
                    <button
                      type="button"
                      className="cancel-button"
                      onClick={() => removeJob(j.jobId)}
                    >
                      Remove
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>

          <form className="modal-form" onSubmit={saveWorkerDetails}>
            <h4>Worker Type</h4>
            <p className="section-hint">
              Weekly hours: student 20; full-time 40 (+20 overtime); hourly
              manager-set limit. Overtime needs manager approval.
            </p>
            <select name="workerType" defaultValue={workerEditModal.workerType}>
              <option value="student">Student (20 hrs/wk)</option>
              <option value="fulltime">Full-time staff (40 + 20 ot)</option>
              <option value="hourly">Hourly staff (set limit)</option>
            </select>
            <label className="modal-label">
              Hourly limit (1–40)
              <input
                type="number"
                name="hourlyLimit"
                min={1}
                max={40}
                defaultValue={
                  workerEditModal.workerType === "hourly"
                    ? workerEditModal.hourlyLimit
                    : 40
                }
              />
            </label>
            <div className="modal-actions">
              <button type="submit" className="login-button">
                Save Worker Type
              </button>
            </div>
          </form>

          <form className="modal-form" onSubmit={saveAssignment}>
            <h4>Assign a Job</h4>
            <select name="jobId" required>
              {jobs.map((j) => (
                <option key={j.id} value={j.id}>
                  {j.name} ({j.teamName})
                </option>
              ))}
            </select>
            <input
              type="number"
              name="minHours"
              placeholder="Min hours"
              min={0}
              required
              defaultValue={0}
            />
            <input
              type="number"
              name="maxHours"
              placeholder="Max hours"
              min={0}
              required
              defaultValue={20}
            />
            <div className="modal-actions">
              <button type="submit" className="login-button">
                Assign Job
              </button>
            </div>
          </form>

          <form className="modal-form" onSubmit={saveSchedule}>
            <h4>Add Weekly Schedule</h4>
            <select name="jobId" required>
              {workerEditModal.jobs.map((j) => (
                <option key={j.jobId} value={j.jobId}>
                  {j.jobName}
                </option>
              ))}
            </select>
            <select name="dayOfWeek" defaultValue="1">
              {DAYS.map((d, i) => (
                <option key={i} value={i}>
                  {d}
                </option>
              ))}
            </select>
            <input type="time" name="startTime" required />
            <input type="time" name="endTime" required />
            <div className="modal-actions">
              <button type="submit" className="login-button">
                Add Schedule
              </button>
            </div>
          </form>

          <div className="modal-actions">
            <button
              type="button"
              className="cancel-button"
              onClick={() => setWorkerEditModal(null)}
            >
              Close
            </button>
          </div>
        </Modal>
      )}

      {workerModal && (
        <Modal title="Add Worker" onClose={() => setWorkerModal(false)}>
          <form className="modal-form" onSubmit={saveWorker}>
            <input type="email" name="email" placeholder="Email" required />
            <input
              type="password"
              name="password"
              placeholder="Temporary password"
              required
            />
            <input
              type="text"
              name="firstName"
              placeholder="First name"
              required
            />
            <input
              type="text"
              name="lastName"
              placeholder="Last name"
              required
            />
            <select name="role" defaultValue="student">
              <option value="student">Student</option>
              <option value="staff">Staff</option>
            </select>
            <select name="jobId" required>
              {jobs.map((j) => (
                <option key={j.id} value={j.id}>
                  {j.name} ({j.teamName})
                </option>
              ))}
            </select>
            <input
              type="number"
              name="minHours"
              placeholder="Min hours"
              min={0}
              required
              defaultValue={0}
            />
            <input
              type="number"
              name="maxHours"
              placeholder="Max hours"
              min={0}
              required
              defaultValue={20}
            />
            <div className="modal-actions">
              <button type="submit" className="login-button">
                Create Worker
              </button>
              <button
                type="button"
                className="cancel-button"
                onClick={() => setWorkerModal(false)}
              >
                Cancel
              </button>
            </div>
          </form>
        </Modal>
      )}

      {workqueueModal && (
        <Modal
          title="Add Workqueue Shift"
          onClose={() => setWorkqueueModal(false)}
        >
          <form className="modal-form" onSubmit={saveWorkqueue}>
            <select name="teamId" required>
              {teams.map((d) => (
                <option key={d.id} value={d.id}>
                  {d.name}
                </option>
              ))}
            </select>
            <input type="date" name="date" required />
            <input type="time" name="startTime" required />
            <input type="time" name="endTime" required />
            <div className="modal-actions">
              <button
                type="button"
                className="cancel-button"
                onClick={() => setWorkqueueModal(false)}
              >
                Cancel
              </button>
              <button type="submit" className="login-button">
                Add
              </button>
            </div>
          </form>
        </Modal>
      )}

      {jobModal && (
        <JobModal
          job={jobModal.id > 0 ? jobModal : null}
          teams={teams}
          onSave={saveJob}
          onClose={() => setJobModal(null)}
        />
      )}

      <PageFooter meta={<span>Manager</span>} />

      {(role === "manager" || role === "admin") && (
        <AIAssistantPanel token={token ?? ""} />
      )}
    </div>
  );
}
