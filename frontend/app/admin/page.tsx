"use client";

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { API_BASE, callApi } from "@/lib/api";
import { ROLES } from "@/lib/roles";
import { PageHeader } from "@/components/PageHeader";
import { PageFooter } from "@/components/PageFooter";
import { PageTitle } from "@/components/PageTitle";
import { Modal } from "@/components/Modal";
import { JobModal, type JobInput } from "@/components/JobModal";

type Location = {
  id: number;
  name: string;
  abbreviation?: string | null;
  address?: string | null;
  address2?: string | null;
  city?: string | null;
  state?: string | null;
  zip?: string | null;
  country?: string | null;
  managerId?: number | null;
  managerEmail?: string | null;
};
type Department = {
  id: number;
  name: string;
  departmentCode?: string | null;
  locationId: number;
  locationName: string;
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
type User = {
  id: number;
  email: string;
  role: string;
  disabled?: boolean;
  uid?: string | null;
  firstName?: string | null;
  lastName?: string | null;
  address?: string | null;
  address2?: string | null;
  city?: string | null;
  state?: string | null;
  zip?: string | null;
  country?: string | null;
  phone?: string | null;
  communicationPreference?: string | null;
};

type LocationModal =
  { mode: "create" } | { mode: "edit"; loc: Location } | null;
type DepartmentModal =
  { mode: "create" } | { mode: "edit"; dept: Department } | null;
type JobModal = { mode: "create" } | { mode: "edit"; job: Job } | null;
type UserModal = { mode: "create" } | { mode: "edit"; user: User } | null;

export default function AdminPage() {
  const [token, setToken] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [locations, setLocations] = useState<Location[]>([]);
  const [departments, setDepartments] = useState<Department[]>([]);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [sortBy, setSortBy] = useState<"email" | "role" | "disabled">("email");
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc");
  const [page, setPage] = useState(1);
  const [locationModal, setLocationModal] = useState<LocationModal>(null);
  const [departmentModal, setDepartmentModal] = useState<DepartmentModal>(null);
  const [jobModal, setJobModal] = useState<JobModal>(null);
  const [userModal, setUserModal] = useState<UserModal>(null);
  const passwordRef = useRef<HTMLInputElement>(null);

  const USERS_PER_PAGE = 10;

  const sortedUsers = [...users].sort((a, b) => {
    const av = String(a[sortBy]);
    const bv = String(b[sortBy]);
    const cmp = av.localeCompare(bv, undefined, { numeric: true });
    return sortDir === "asc" ? cmp : -cmp;
  });
  const pageCount = Math.max(1, Math.ceil(sortedUsers.length / USERS_PER_PAGE));
  const currentPage = Math.min(page, pageCount);
  const pageUsers = sortedUsers.slice(
    (currentPage - 1) * USERS_PER_PAGE,
    currentPage * USERS_PER_PAGE,
  );

  const toggleSort = (key: "email" | "role" | "disabled") => {
    if (sortBy === key) {
      setSortDir(sortDir === "asc" ? "desc" : "asc");
    } else {
      setSortBy(key);
      setSortDir("asc");
    }
    setPage(1);
  };

  const load = useCallback(async (authToken: string) => {
    const [l, d, j, u] = await Promise.all([
      callApi<{ locations: Location[] }>(
        authToken,
        "/api/locations",
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
      callApi<{ jobs: Job[] }>(authToken, "/api/jobs", "GET", undefined, false),
      callApi<{ users: User[] }>(
        authToken,
        "/api/users",
        "GET",
        undefined,
        false,
      ),
    ]);
    if (l) setLocations(l.locations ?? []);
    if (d) setDepartments(d.departments ?? []);
    if (j) setJobs(j.jobs ?? []);
    if (u) setUsers(u.users ?? []);
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
      if (me.user.role !== "admin") {
        window.location.href = "/dashboard";
        return;
      }
      setEmail(me.user.email);
      await load(stored);
    })();
  }, [load]);

  const act = (path: string, method: string, body?: unknown) => {
    if (!token) return;
    void (async () => {
      const result = await callApi(token, path, method, body);
      if (result) await load(token);
    })();
  };

  const saveLocation = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const body: Record<string, unknown> = {};
    for (const [k, v] of data.entries()) body[k] = v;
    if (locationModal?.mode === "edit") {
      act(`/api/locations/${locationModal.loc.id}`, "PATCH", body);
    } else {
      act("/api/locations", "POST", body);
    }
    setLocationModal(null);
  };

  const saveDepartment = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const body: Record<string, unknown> = {};
    for (const [k, v] of data.entries()) body[k] = v;
    // Backend expects a number; FormData yields a string.
    body.locationId = Number(data.get("locationId"));
    if (departmentModal?.mode === "edit") {
      act(`/api/departments/${departmentModal.dept.id}`, "PATCH", body);
    } else {
      act("/api/departments", "POST", body);
    }
    setDepartmentModal(null);
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
      act(`/api/jobs/${data.id}`, "PATCH", body);
    } else {
      act("/api/jobs", "POST", body);
    }
    setJobModal(null);
  };

  const saveUser = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const body: Record<string, unknown> = {};
    for (const [k, v] of data.entries()) body[k] = v;
    if (userModal?.mode === "edit") {
      act(`/api/users/${userModal.user.id}/role`, "PATCH", {
        role: body.role,
        uid: body.uid,
      });
      act(`/api/users/${userModal.user.id}/profile`, "PATCH", {
        firstName: body.firstName,
        lastName: body.lastName,
        address: body.address,
        address2: body.address2,
        city: body.city,
        state: body.state,
        zip: body.zip,
        country: body.country,
        phone: body.phone,
        communicationPreference: body.communicationPreference,
      });
    } else {
      act("/api/users", "POST", body);
    }
    setUserModal(null);
  };

  const generatePassword = (userId: number) => {
    if (!token) return;
    void (async () => {
      const result = await callApi<{ message?: string }>(
        token,
        `/api/users/${userId}/generate-password`,
        "POST",
      );
      if (result) {
        alert(result.message ?? "Password generated and emailed");
        await load(token);
      }
    })();
  };

  // Client-side 15-char password for the Add User form (always satisfies the
  // password rules: uppercase, digit, special).
  const generateTempPassword = () => {
    const upper = "ABCDEFGHJKLMNPQRSTUVWXYZ";
    const digits = "23456789";
    const special = "!@#$%^&*";
    const all = upper + "abcdefghijkmnpqrstuvwxyz" + digits + special;
    const rand = (n: number) => Math.floor(Math.random() * n);
    const chars = [
      upper[rand(upper.length)],
      digits[rand(digits.length)],
      special[rand(special.length)],
    ];
    for (let i = 3; i < 15; i++) chars.push(all[rand(all.length)]);
    for (let i = chars.length - 1; i > 0; i--) {
      const j = rand(i + 1);
      [chars[i], chars[j]] = [chars[j], chars[i]];
    }
    return chars.join("");
  };

  const handleGeneratePassword = () => {
    const pw = generateTempPassword();
    if (passwordRef.current) passwordRef.current.value = pw;
  };

  const logout = (event: { preventDefault: () => void }) => {
    event.preventDefault();
    localStorage.removeItem("auth_token");
    window.location.href = "/";
  };

  return (
    <div className="dashboard-container wide">
      <PageTitle title="Admin" />
      <PageHeader
        title="Admin"
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
          <a href="#locations">Locations</a>
          <a href="#departments">Departments</a>
          <a href="#jobs">Jobs</a>
          <a href="#access-control">Access Control</a>
        </nav>
        <div className="dashboard-card">
          <div className="user-list-section" id="locations">
            <div className="section-title-row">
              <h2>Locations</h2>
              <button
                type="button"
                className="login-button add-button"
                onClick={() => setLocationModal({ mode: "create" })}
              >
                Add Location
              </button>
            </div>
            <div className="table-scroll">
              <table className="user-table">
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Abbr</th>
                    <th>Address</th>
                    <th>City</th>
                    <th>State</th>
                    <th>Zip</th>
                    <th>Manager</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {locations.length === 0 ? (
                    <tr>
                      <td colSpan={8}>No locations yet.</td>
                    </tr>
                  ) : (
                    locations.map((l) => (
                      <tr key={l.id}>
                        <td>{l.name}</td>
                        <td>{l.abbreviation ?? "—"}</td>
                        <td>{l.address ?? "—"}</td>
                        <td>{l.city ?? "—"}</td>
                        <td>{l.state ?? "—"}</td>
                        <td>{l.zip ?? "—"}</td>
                        <td>
                          <select
                            value={l.managerId ?? ""}
                            onChange={(e) => {
                              const managerId = e.target.value;
                              if (managerId) {
                                act(
                                  `/api/managers/${managerId}/assign`,
                                  "POST",
                                  {
                                    locationId: l.id,
                                  },
                                );
                              }
                            }}
                          >
                            <option value="">—</option>
                            {users
                              .filter(
                                (u) =>
                                  u.role === "manager" ||
                                  u.role === "scheduler",
                              )
                              .map((m) => (
                                <option key={m.id} value={m.id}>
                                  {m.email}
                                </option>
                              ))}
                          </select>
                        </td>
                        <td>
                          <button
                            type="button"
                            onClick={() =>
                              setLocationModal({ mode: "edit", loc: l })
                            }
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
          </div>

          <div className="user-list-section" id="departments">
            <div className="section-title-row">
              <h2>Departments</h2>
              <button
                type="button"
                className="login-button add-button"
                onClick={() => setDepartmentModal({ mode: "create" })}
              >
                Add Department
              </button>
            </div>
            <div className="table-scroll">
              <table className="user-table">
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Code</th>
                    <th>Location</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {departments.length === 0 ? (
                    <tr>
                      <td colSpan={4}>No departments yet.</td>
                    </tr>
                  ) : (
                    departments.map((d) => (
                      <tr key={d.id}>
                        <td>{d.name}</td>
                        <td>{d.departmentCode ?? "—"}</td>
                        <td>{d.locationName}</td>
                        <td>
                          <button
                            type="button"
                            onClick={() =>
                              setDepartmentModal({ mode: "edit", dept: d })
                            }
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
          </div>

          <div className="user-list-section" id="jobs">
            <div className="section-title-row">
              <h2>Jobs</h2>
              <button
                type="button"
                className="login-button add-button"
                onClick={() => setJobModal({ mode: "create" })}
              >
                Add Job
              </button>
            </div>
            <p className="section-hint">
              A job is a position a worker/student is qualified to do, within a
              department at a location.
            </p>
            <div className="table-scroll">
              <table className="user-table">
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Department</th>
                    <th>Location</th>
                    <th>Workers</th>
                    <th>Weekly Hours</th>
                    <th>Holidays</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {jobs.length === 0 ? (
                    <tr>
                      <td colSpan={7}>No jobs yet.</td>
                    </tr>
                  ) : (
                    jobs.map((j) => (
                      <tr key={j.id}>
                        <td>{j.name}</td>
                        <td>{j.departmentName}</td>
                        <td>{j.locationName}</td>
                        <td>
                          {j.currentWorkers} / {j.optimalWorkers}
                        </td>
                        <td>{j.weeklyHours} hrs</td>
                        <td>
                          {j.holidays.length === 0 ? "—" : j.holidays.length}
                        </td>
                        <td>
                          <button
                            type="button"
                            onClick={() =>
                              setJobModal({ mode: "edit", job: j })
                            }
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
          </div>

          <div className="user-list-section" id="access-control">
            <div className="section-title-row">
              <h2>Access Control</h2>
              <button
                type="button"
                className="login-button add-button"
                onClick={() => setUserModal({ mode: "create" })}
              >
                Add User
              </button>
            </div>
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
                    <th>UID</th>
                    <th className="sortable" onClick={() => toggleSort("role")}>
                      Role
                      {sortBy === "role"
                        ? sortDir === "asc"
                          ? " ▲"
                          : " ▼"
                        : ""}
                    </th>
                    <th
                      className="sortable"
                      onClick={() => toggleSort("disabled")}
                    >
                      Status
                      {sortBy === "disabled"
                        ? sortDir === "asc"
                          ? " ▲"
                          : " ▼"
                        : ""}
                    </th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {users.length === 0 ? (
                    <tr>
                      <td colSpan={5}>No users.</td>
                    </tr>
                  ) : (
                    pageUsers.map((u) => (
                      <tr key={u.id}>
                        <td>{u.email}</td>
                        <td>{u.uid ?? "—"}</td>
                        <td>{u.role}</td>
                        <td>{u.disabled ? "Disabled" : "Active"}</td>
                        <td>
                          <button
                            type="button"
                            onClick={() =>
                              setUserModal({ mode: "edit", user: u })
                            }
                          >
                            Edit
                          </button>
                          <button
                            type="button"
                            onClick={() => generatePassword(u.id)}
                          >
                            Reset Password
                          </button>
                          {u.disabled ? (
                            <button
                              type="button"
                              onClick={() =>
                                act(`/api/users/${u.id}/enable`, "POST")
                              }
                            >
                              Enable
                            </button>
                          ) : (
                            <button
                              type="button"
                              onClick={() =>
                                act(`/api/users/${u.id}/disable`, "POST")
                              }
                            >
                              Disable
                            </button>
                          )}
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
        </div>
      </div>

      {locationModal && (
        <Modal
          title={
            locationModal.mode === "edit" ? "Edit Location" : "Add Location"
          }
          onClose={() => setLocationModal(null)}
        >
          <form className="modal-form" onSubmit={saveLocation}>
            <input
              type="text"
              name="name"
              placeholder="Name"
              required
              defaultValue={
                locationModal.mode === "edit" ? locationModal.loc.name : ""
              }
            />
            <input
              type="text"
              name="abbreviation"
              placeholder="Abbreviation"
              defaultValue={
                locationModal.mode === "edit"
                  ? (locationModal.loc.abbreviation ?? "")
                  : ""
              }
            />
            <input
              type="text"
              name="address"
              placeholder="Address"
              defaultValue={
                locationModal.mode === "edit"
                  ? (locationModal.loc.address ?? "")
                  : ""
              }
            />
            <input
              type="text"
              name="address2"
              placeholder="Address 2"
              defaultValue={
                locationModal.mode === "edit"
                  ? (locationModal.loc.address2 ?? "")
                  : ""
              }
            />
            <input
              type="text"
              name="city"
              placeholder="City"
              defaultValue={
                locationModal.mode === "edit"
                  ? (locationModal.loc.city ?? "")
                  : ""
              }
            />
            <input
              type="text"
              name="state"
              placeholder="State"
              defaultValue={
                locationModal.mode === "edit"
                  ? (locationModal.loc.state ?? "")
                  : ""
              }
            />
            <input
              type="text"
              name="zip"
              placeholder="Zip"
              defaultValue={
                locationModal.mode === "edit"
                  ? (locationModal.loc.zip ?? "")
                  : ""
              }
            />
            <input
              type="text"
              name="country"
              placeholder="Country"
              defaultValue={
                locationModal.mode === "edit"
                  ? (locationModal.loc.country ?? "")
                  : ""
              }
            />
            <div className="modal-actions">
              <button
                type="button"
                className="cancel-button"
                onClick={() => setLocationModal(null)}
              >
                Cancel
              </button>
              <button type="submit" className="login-button">
                Save
              </button>
            </div>
          </form>
        </Modal>
      )}

      {departmentModal && (
        <Modal
          title={
            departmentModal.mode === "edit"
              ? "Edit Department"
              : "Add Department"
          }
          onClose={() => setDepartmentModal(null)}
        >
          <form className="modal-form" onSubmit={saveDepartment}>
            <input
              type="text"
              name="name"
              placeholder="Department name"
              required
              defaultValue={
                departmentModal.mode === "edit" ? departmentModal.dept.name : ""
              }
            />
            <input
              type="text"
              name="departmentCode"
              placeholder="Code (max 20)"
              maxLength={20}
              defaultValue={
                departmentModal.mode === "edit"
                  ? (departmentModal.dept.departmentCode ?? "")
                  : ""
              }
            />
            <select
              name="locationId"
              required
              defaultValue={
                departmentModal.mode === "edit"
                  ? departmentModal.dept.locationId
                  : ""
              }
            >
              {locations.map((l) => (
                <option key={l.id} value={l.id}>
                  {l.name}
                </option>
              ))}
            </select>
            <div className="modal-actions">
              <button
                type="button"
                className="cancel-button"
                onClick={() => setDepartmentModal(null)}
              >
                Cancel
              </button>
              <button type="submit" className="login-button">
                Save
              </button>
            </div>
          </form>
        </Modal>
      )}

      {jobModal && (
        <JobModal
          job={jobModal.mode === "edit" ? jobModal.job : null}
          departments={departments}
          onSave={saveJob}
          onClose={() => setJobModal(null)}
        />
      )}

      {userModal && (
        <Modal
          title={userModal.mode === "edit" ? "Edit User" : "Add User"}
          onClose={() => setUserModal(null)}
        >
          <form className="modal-form" onSubmit={saveUser}>
            {userModal.mode === "edit" ? (
              <>
                <p className="section-hint">{userModal.user.email}</p>
                <select name="role" required defaultValue={userModal.user.role}>
                  {ROLES.map((r) => (
                    <option key={r} value={r}>
                      {r}
                    </option>
                  ))}
                </select>
                <input
                  type="text"
                  name="uid"
                  placeholder="UID (max 20)"
                  maxLength={20}
                  defaultValue={userModal.user.uid ?? ""}
                />
                <input
                  type="text"
                  name="firstName"
                  placeholder="First name"
                  required
                  defaultValue={userModal.user.firstName ?? ""}
                />
                <input
                  type="text"
                  name="lastName"
                  placeholder="Last name"
                  required
                  defaultValue={userModal.user.lastName ?? ""}
                />
                <input
                  type="text"
                  name="address"
                  placeholder="Address"
                  required
                  defaultValue={userModal.user.address ?? ""}
                />
                <input
                  type="text"
                  name="address2"
                  placeholder="Address 2"
                  defaultValue={userModal.user.address2 ?? ""}
                />
                <input
                  type="text"
                  name="city"
                  placeholder="City"
                  defaultValue={userModal.user.city ?? ""}
                />
                <input
                  type="text"
                  name="state"
                  placeholder="State"
                  required
                  defaultValue={userModal.user.state ?? ""}
                />
                <input
                  type="text"
                  name="zip"
                  placeholder="Zip"
                  required
                  defaultValue={userModal.user.zip ?? ""}
                />
                <input
                  type="text"
                  name="country"
                  placeholder="Country"
                  defaultValue={userModal.user.country ?? ""}
                />
                <input
                  type="text"
                  name="phone"
                  placeholder="Phone"
                  required
                  defaultValue={userModal.user.phone ?? ""}
                />
                <select
                  name="communicationPreference"
                  required
                  defaultValue={
                    userModal.user.communicationPreference ?? "email"
                  }
                >
                  <option value="email">Email</option>
                  <option value="text">Text</option>
                  <option value="phone">Phone</option>
                </select>
              </>
            ) : (
              <>
                <input type="email" name="email" placeholder="Email" required />
                <input
                  type="text"
                  name="uid"
                  placeholder="UID (max 20)"
                  maxLength={20}
                />
                <input
                  ref={passwordRef}
                  type="password"
                  name="password"
                  placeholder="Temporary password"
                  required
                />
                <div className="modal-actions">
                  <button
                    type="button"
                    className="cancel-button"
                    onClick={handleGeneratePassword}
                  >
                    Generate Password
                  </button>
                </div>
              </>
            )}
            <div className="modal-actions">
              <button
                type="button"
                className="cancel-button"
                onClick={() => setUserModal(null)}
              >
                Cancel
              </button>
              <button type="submit" className="login-button">
                Save
              </button>
            </div>
          </form>
        </Modal>
      )}

      <PageFooter meta={<span>Admin</span>} />
    </div>
  );
}
