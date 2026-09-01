"use client";

import { useCallback, useEffect, useState, type FormEvent } from "react";
import { API_BASE, callApi } from "@/lib/api";
import { PageHeader } from "@/components/PageHeader";
import { PageFooter } from "@/components/PageFooter";
import { PageTitle } from "@/components/PageTitle";
import { Modal } from "@/components/Modal";
import { JobModal, type JobInput } from "@/components/JobModal";
import { hasRole } from "@/lib/roles";

type StudentJob = {
  jobId: number;
  jobName: string;
  departmentId: number;
  departmentName: string;
  locationId: number;
  minHours: number;
  maxHours: number;
  active: boolean;
};

type Student = {
  id: number;
  email: string;
  disabled: boolean;
  workerType: string;
  workerTypeLabel: string;
  hourlyLimit: number;
  weekHoursCap: number;
  weekHoursUsed: number;
  jobs: StudentJob[];
};

type JobSchedule = {
  dayOfWeek: number;
  startTime: string;
  endTime: string;
  hours: number;
};
type JobHoliday = {
  date: string;
  reason?: string | null;
};
type Job = {
  id: number;
  name: string;
  departmentId: number;
  departmentName: string;
  locationId: number;
  locationName: string;
  optimalWorkers: number;
  currentWorkers: number;
  weeklyHours: number;
  schedules: JobSchedule[];
  holidays: JobHoliday[];
};

type Shift = {
  id: number;
  departmentName: string;
  date: string;
  startTime: string;
  endTime: string;
  status: string;
  assignedUserId: number | null;
  assignedEmail: string | null;
};

type Preference = {
  dayOfWeek: number;
  startTime: string;
  endTime: string;
};

type Department = {
  id: number;
  name: string;
  locationId: number;
  locationName: string;
};

type Request = {
  id: number;
  email: string;
  date: string;
  startTime: string;
  endTime: string;
  type: string;
  reason: string | null;
};

const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

const hoursBetween = (start: string, end: string) => {
  const [sh, sm] = start.split(":").map(Number);
  const [eh, em] = end.split(":").map(Number);
  return (eh * 60 + em - (sh * 60 + sm)) / 60;
};

export default function ManagerPage() {
  const [token, setToken] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<string>("");
  const [students, setStudents] = useState<Student[]>([]);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [shifts, setShifts] = useState<Shift[]>([]);
  const [departments, setDepartments] = useState<Department[]>([]);
  const [requests, setRequests] = useState<Request[]>([]);
  const [sortBy, setSortBy] = useState<"email">("email");
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc");
  const [page, setPage] = useState(1);
  const [studentModal, setStudentModal] = useState<Student | null>(null);
  const [jobModal, setJobModal] = useState<Job | null>(null);
  const [workqueueModal, setWorkqueueModal] = useState(false);
  const [workerModal, setWorkerModal] = useState(false);
  const [workerShifts, setWorkerShifts] = useState<Shift[]>([]);
  const [workerPrefs, setWorkerPrefs] = useState<Preference[]>([]);

  // Schedulers (and admins) can assign shifts to workers; managers set up jobs.
  const canAssign = hasRole(role, "scheduler");
  // Managers and admins create/edit jobs; schedulers only view them.
  const canManageJobs = role === "manager" || role === "admin";

  const STUDENTS_PER_PAGE = 10;

  const sortedStudents = [...students].sort((a, b) => {
    const av = String(a[sortBy] ?? "");
    const bv = String(b[sortBy] ?? "");
    const cmp = av.localeCompare(bv, undefined, { numeric: true });
    return sortDir === "asc" ? cmp : -cmp;
  });
  const pageCount = Math.max(
    1,
    Math.ceil(sortedStudents.length / STUDENTS_PER_PAGE),
  );
  const currentPage = Math.min(page, pageCount);
  const pageStudents = sortedStudents.slice(
    (currentPage - 1) * STUDENTS_PER_PAGE,
    currentPage * STUDENTS_PER_PAGE,
  );

  const toggleSort = (key: "email") => {
    if (sortBy === key) {
      setSortDir(sortDir === "asc" ? "desc" : "asc");
    } else {
      setSortBy(key);
      setSortDir("asc");
    }
    setPage(1);
  };

  const load = useCallback(async (authToken: string) => {
    const [s, j, sh, d, r] = await Promise.all([
      callApi<{ students: Student[] }>(
        authToken,
        "/api/students",
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
      callApi<{ departments: Department[] }>(
        authToken,
        "/api/departments",
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
    ]);
    if (s) setStudents(s.students ?? []);
    if (j) setJobs(j.jobs ?? []);
    if (sh) setShifts(sh.shifts ?? []);
    if (d) setDepartments(d.departments ?? []);
    if (r) setRequests(r.requests ?? []);
  }, []);

  useEffect(() => {
    (async () => {
      const stored = localStorage.getItem("auth_token");
      if (!stored) {
        window.location.href = "/";
        return;
      }
      setToken(stored);
      const me = await callApi<{ user: { email: string; role: string } }>(
        stored,
        "/api/me",
        "GET",
        undefined,
        false,
      );
      if (!me) {
        localStorage.removeItem("auth_token");
        window.location.href = "/";
        return;
      }
      if (
        me.user.role !== "manager" &&
        me.user.role !== "scheduler" &&
        me.user.role !== "admin"
      ) {
        window.location.href = "/dashboard";
        return;
      }
      setEmail(me.user.email);
      setRole(me.user.role);
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
    if (!studentModal) return;
    const data = new FormData(event.currentTarget);
    const body: Record<string, unknown> = {};
    for (const [k, v] of data.entries()) body[k] = v;
    // Backend expects numbers; FormData yields strings.
    body.jobId = Number(data.get("jobId"));
    body.minHours = Number(data.get("minHours"));
    body.maxHours = Number(data.get("maxHours"));
    act(`/api/students/${studentModal.id}/jobs`, body);
  };

  const saveWorkerDetails = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!studentModal) return;
    const data = new FormData(event.currentTarget);
    const workerType = String(data.get("workerType"));
    let hourlyLimit = Number(data.get("hourlyLimit")) || 0;
    if (workerType !== "hourly") hourlyLimit = 0;
    act(
      `/api/users/${studentModal.id}/worker`,
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
    if (!studentModal) return;
    act(`/api/students/${studentModal.id}/jobs/${jobId}`, undefined, "DELETE");
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
    if (!studentModal) return;
    const data = new FormData(event.currentTarget);
    const body: Record<string, unknown> = {};
    for (const [k, v] of data.entries()) body[k] = v;
    body.jobId = Number(data.get("jobId"));
    body.dayOfWeek = Number(data.get("dayOfWeek"));
    act(`/api/students/${studentModal.id}/schedule`, body);
    event.currentTarget.reset();
  };

  const saveWorkqueue = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const body: Record<string, unknown> = {};
    for (const [k, v] of data.entries()) body[k] = v;
    body.departmentId = Number(data.get("departmentId"));
    act("/api/workqueue", body);
    setWorkqueueModal(false);
  };

  const saveJob = (data: JobInput) => {
    const body: Record<string, unknown> = {
      name: data.name,
      departmentId: data.departmentId,
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
        subtitle={
          <>
            Welcome, <span className="highlight">{email}</span>
          </>
        }
      >
        <a className="logout-link" href="/" onClick={logout}>
          Logout
        </a>
      </PageHeader>

      <div className="with-sidebar">
        <nav className="sidebar">
          <a href="#students">Workers</a>
          <a href="#jobs">Job Requirements</a>
          <a href="#workqueue">Workqueue</a>
          <a href="#requests">Requests</a>
        </nav>
        <div className="dashboard-card">
          <div className="user-list-section" id="students">
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
            <div className="table-scroll">
              <table className="user-table">
                <thead>
                  <tr>
                    <th
                      className="sortable"
                      onClick={() => toggleSort("email")}
                    >
                      Email
                      {sortBy === "email"
                        ? sortDir === "asc"
                          ? " ▲"
                          : " ▼"
                        : ""}
                    </th>
                    <th>Type</th>
                    <th>Hours</th>
                    <th>Jobs</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {students.length === 0 ? (
                    <tr>
                      <td colSpan={5}>No workers in your location.</td>
                    </tr>
                  ) : (
                    pageStudents.map((s) => (
                      <tr key={s.id}>
                        <td>{s.email}</td>
                        <td>{s.workerTypeLabel}</td>
                        <td>
                          {s.weekHoursUsed} / {s.weekHoursCap} hrs
                        </td>
                        <td>
                          {s.jobs.length === 0
                            ? "—"
                            : s.jobs.map((j) => j.jobName).join(", ")}
                        </td>
                        <td>
                          <button
                            type="button"
                            onClick={() => {
                              setStudentModal(s);
                              loadWorkerCalendar(s.id);
                              loadWorkerPrefs(s.id);
                            }}
                          >
                            Edit
                          </button>
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
            {pageCount > 1 && (
              <div className="user-pager">
                <button
                  type="button"
                  disabled={currentPage <= 1}
                  onClick={() => setPage(currentPage - 1)}
                >
                  Prev
                </button>
                <span>
                  Page {currentPage} of {pageCount}
                </span>
                <button
                  type="button"
                  disabled={currentPage >= pageCount}
                  onClick={() => setPage(currentPage + 1)}
                >
                  Next
                </button>
              </div>
            )}
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
                      departmentId: 0,
                      departmentName: "",
                      locationId: 0,
                      locationName: "",
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
              coverage (current / optimal) for each job in your location. Days
              with no hours are closed (weekend, holiday).
            </p>
            <div className="table-scroll">
              <table className="user-table">
                <thead>
                  <tr>
                    <th>Job</th>
                    <th>Department</th>
                    {DAYS.map((d) => (
                      <th key={d}>{d}</th>
                    ))}
                    <th>Weekly</th>
                    <th>Workers</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {jobs.length === 0 ? (
                    <tr>
                      <td colSpan={11}>No jobs in your location.</td>
                    </tr>
                  ) : (
                    jobs.map((j) => (
                      <tr key={j.id}>
                        <td>{j.name}</td>
                        <td>{j.departmentName}</td>
                        {DAYS.map((_, dow) => {
                          const s = j.schedules.find(
                            (x) => x.dayOfWeek === dow,
                          );
                          return (
                            <td key={dow}>
                              {s ? `${s.startTime}–${s.endTime}` : "—"}
                            </td>
                          );
                        })}
                        <td>{j.weeklyHours} hrs</td>
                        <td>
                          {j.currentWorkers} / {j.optimalWorkers}
                        </td>
                        <td>
                          {canManageJobs && (
                            <button
                              type="button"
                              onClick={() => setJobModal(j)}
                            >
                              Edit
                            </button>
                          )}
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
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
              {role === "scheduler"
                ? "All slots in your department."
                : "All slots in your location."}{" "}
              {canAssign
                ? "Assign a slot to a worker (or unassign it) to build the schedule."
                : "Open shifts students can pick from, or return missed shifts here."}
            </p>
            <div className="table-scroll">
              <table className="user-table">
                <thead>
                  <tr>
                    <th>Department</th>
                    <th>Date</th>
                    <th>Time</th>
                    <th>Status</th>
                    <th>Assigned Worker</th>
                    {canAssign && <th></th>}
                  </tr>
                </thead>
                <tbody>
                  {shifts.length === 0 ? (
                    <tr>
                      <td colSpan={canAssign ? 6 : 5}>No shifts yet.</td>
                    </tr>
                  ) : (
                    shifts.map((sh) => (
                      <tr key={sh.id}>
                        <td>{sh.departmentName}</td>
                        <td>{sh.date}</td>
                        <td>
                          {sh.startTime}–{sh.endTime}
                        </td>
                        <td>{sh.status}</td>
                        <td>{sh.assignedEmail ?? "—"}</td>
                        {canAssign && (
                          <td>
                            <form
                              className="schedule-row assign-form"
                              onSubmit={(e) => assignShift(e, sh.id)}
                            >
                              <select
                                name="userId"
                                defaultValue={sh.assignedUserId ?? ""}
                              >
                                <option value="">Unassigned</option>
                                {students
                                  .filter(
                                    (st) =>
                                      st.id === sh.assignedUserId ||
                                      st.weekHoursUsed +
                                        hoursBetween(
                                          sh.startTime,
                                          sh.endTime,
                                        ) <=
                                        st.weekHoursCap,
                                  )
                                  .map((st) => (
                                    <option key={st.id} value={st.id}>
                                      {st.email} ({st.weekHoursUsed}/
                                      {st.weekHoursCap}h)
                                    </option>
                                  ))}
                              </select>
                              <button type="submit" className="login-button">
                                Assign
                              </button>
                            </form>
                          </td>
                        )}
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>

          <div className="user-list-section" id="requests">
            <h2>Pending Requests</h2>
            <div className="table-scroll">
              <table className="user-table">
                <thead>
                  <tr>
                    <th>Worker</th>
                    <th>Date</th>
                    <th>Time</th>
                    <th>Type</th>
                    <th>Reason</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {requests.length === 0 ? (
                    <tr>
                      <td colSpan={6}>No pending requests.</td>
                    </tr>
                  ) : (
                    requests.map((r) => (
                      <tr key={r.id}>
                        <td>{r.email}</td>
                        <td>{r.date}</td>
                        <td>
                          {r.startTime}–{r.endTime}
                        </td>
                        <td>{r.type}</td>
                        <td>{r.reason ?? "—"}</td>
                        <td>
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
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </div>

      {studentModal && (
        <Modal
          title={`Edit ${studentModal.email}`}
          onClose={() => setStudentModal(null)}
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
                      {sh.date} · {sh.startTime}–{sh.endTime} ·{" "}
                      {sh.departmentName}
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
                {workerPrefs.map((p, i) => (
                  <li key={i}>
                    <span>
                      {DAYS[p.dayOfWeek]} · {p.startTime}–{p.endTime}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </div>

          <div className="modal-form">
            <h4>Current Jobs</h4>
            {studentModal.jobs.length === 0 ? (
              <p className="section-hint">No jobs assigned yet.</p>
            ) : (
              <ul className="job-list">
                {studentModal.jobs.map((j) => (
                  <li key={j.jobId}>
                    <span>
                      {j.jobName}{" "}
                      <em>
                        ({j.departmentName} · {j.minHours}–{j.maxHours}h)
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
            <select name="workerType" defaultValue={studentModal.workerType}>
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
                  studentModal.workerType === "hourly"
                    ? studentModal.hourlyLimit
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
                  {j.name} ({j.departmentName})
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
              {studentModal.jobs.map((j) => (
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
              onClick={() => setStudentModal(null)}
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
                  {j.name} ({j.departmentName})
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
            <select name="departmentId" required>
              {departments.map((d) => (
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
          departments={departments}
          onSave={saveJob}
          onClose={() => setJobModal(null)}
        />
      )}

      <PageFooter meta={<span>Manager</span>} />
    </div>
  );
}
