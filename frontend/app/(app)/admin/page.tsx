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
import { useSortablePage } from "@/lib/pagination";
import { DataGrid, type Column } from "@/components/DataGrid";
import type { Department, Job } from "@/lib/types";

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

function userValue(u: User, key: string) {
  return u[key as keyof User];
}
function locValue(l: Location, key: string) {
  return l[key as keyof Location];
}
function deptValue(d: Department, key: string) {
  return d[key as keyof Department];
}
function jobValue(j: Job, key: string) {
  if (key === "workers") return j.currentWorkers;
  if (key === "holidays") return j.holidays.length;
  return j[key as keyof Job];
}

export default function AdminPage() {
  const [token, setToken] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [locations, setLocations] = useState<Location[]>([]);
  const [departments, setDepartments] = useState<Department[]>([]);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [search, setSearch] = useState("");
  const [locationModal, setLocationModal] = useState<LocationModal>(null);
  const [departmentModal, setDepartmentModal] = useState<DepartmentModal>(null);
  const [jobModal, setJobModal] = useState<JobModal>(null);
  const [userModal, setUserModal] = useState<UserModal>(null);
  const passwordRef = useRef<HTMLInputElement>(null);

  const q = search.trim().toLowerCase();
  const filteredUsers = q
    ? users.filter(
        (u) =>
          (u.email ?? "").toLowerCase().includes(q) ||
          (u.uid ?? "").toLowerCase().includes(q) ||
          (u.role ?? "").toLowerCase().includes(q),
      )
    : users;

  const usersGrid = useSortablePage<User>(filteredUsers, userValue, "email");
  const locationsGrid = useSortablePage<Location>(locations, locValue, "name");
  const departmentsGrid = useSortablePage<Department>(
    departments,
    deptValue,
    "name",
  );
  const jobsGrid = useSortablePage<Job>(jobs, jobValue, "name");

  const locationColumns: Column<Location>[] = [
    { key: "name", label: "Name", sortable: true },
    {
      key: "abbreviation",
      label: "Abbr",
      sortable: true,
      render: (l) => l.abbreviation ?? "—",
    },
    {
      key: "address",
      label: "Address",
      sortable: true,
      render: (l) => l.address ?? "—",
    },
    {
      key: "city",
      label: "City",
      sortable: true,
      render: (l) => l.city ?? "—",
    },
    {
      key: "state",
      label: "State",
      sortable: true,
      render: (l) => l.state ?? "—",
    },
    { key: "zip", label: "Zip", sortable: true, render: (l) => l.zip ?? "—" },
    {
      label: "Manager",
      render: (l) => (
        <select
          value={l.managerId ?? ""}
          onChange={(e) => {
            const managerId = e.target.value;
            if (managerId) {
              act(`/api/managers/${managerId}/assign`, "POST", {
                locationId: l.id,
              });
            }
          }}
        >
          <option value="">—</option>
          {users
            .filter((u) => u.role === "manager" || u.role === "scheduler")
            .map((m) => (
              <option key={m.id} value={m.id}>
                {m.email}
              </option>
            ))}
        </select>
      ),
    },
    {
      label: "",
      render: (l) => (
        <button
          type="button"
          onClick={() => setLocationModal({ mode: "edit", loc: l })}
        >
          Edit
        </button>
      ),
    },
  ];

  const departmentColumns: Column<Department>[] = [
    { key: "name", label: "Name", sortable: true },
    {
      key: "departmentCode",
      label: "Code",
      sortable: true,
      render: (d) => d.departmentCode ?? "—",
    },
    { key: "locationName", label: "Location", sortable: true },
    {
      label: "",
      render: (d) => (
        <button
          type="button"
          onClick={() => setDepartmentModal({ mode: "edit", dept: d })}
        >
          Edit
        </button>
      ),
    },
  ];

  const adminJobColumns: Column<Job>[] = [
    { key: "name", label: "Name", sortable: true },
    { key: "departmentName", label: "Department", sortable: true },
    { key: "locationName", label: "Location", sortable: true },
    {
      key: "workers",
      label: "Workers",
      sortable: true,
      render: (j) => `${j.currentWorkers} / ${j.optimalWorkers}`,
    },
    {
      key: "weeklyHours",
      label: "Weekly Hours",
      sortable: true,
      render: (j) => `${j.weeklyHours} hrs`,
    },
    {
      key: "holidays",
      label: "Holidays",
      sortable: true,
      render: (j) => (j.holidays.length === 0 ? "—" : j.holidays.length),
    },
    {
      label: "",
      render: (j) => (
        <button
          type="button"
          onClick={() => setJobModal({ mode: "edit", job: j })}
        >
          Edit
        </button>
      ),
    },
  ];

  const userColumns: Column<User>[] = [
    { key: "email", label: "Email", sortable: true },
    { key: "uid", label: "UID", render: (u) => u.uid ?? "—" },
    { key: "role", label: "Role", sortable: true },
    {
      key: "disabled",
      label: "Status",
      sortable: true,
      render: (u) => (u.disabled ? "Disabled" : "Active"),
    },
    {
      label: "",
      render: (u) => (
        <>
          <button
            type="button"
            onClick={() => setUserModal({ mode: "edit", user: u })}
          >
            Edit
          </button>
          <button type="button" onClick={() => generatePassword(u.id)}>
            Reset Password
          </button>
          {u.disabled ? (
            <button
              type="button"
              onClick={() => act(`/api/users/${u.id}/enable`, "POST")}
            >
              Enable
            </button>
          ) : (
            <button
              type="button"
              onClick={() => act(`/api/users/${u.id}/disable`, "POST")}
            >
              Disable
            </button>
          )}
        </>
      ),
    },
  ];

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
        right={
          <>
            Welcome, <span className="highlight">{email}</span>
          </>
        }
      />

      <div className="with-sidebar">
        <nav className="sidebar">
          <a href="#locations">Locations</a>
          <a href="#departments">Departments</a>
          <a href="#jobs">Jobs</a>
          <a href="#access-control">Access Control</a>
          <a className="logout-link" href="/" onClick={logout}>
            Logout
          </a>
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
            <DataGrid
              grid={locationsGrid}
              columns={locationColumns}
              getRowKey={(l) => l.id}
              emptyText="No locations yet."
            />
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
            <DataGrid
              grid={departmentsGrid}
              columns={departmentColumns}
              getRowKey={(d) => d.id}
              emptyText="No departments yet."
            />
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
            <DataGrid
              grid={jobsGrid}
              columns={adminJobColumns}
              getRowKey={(j) => j.id}
              emptyText="No jobs yet."
            />
          </div>

          <div className="user-list-section" id="access-control">
            <div className="section-title-row">
              <h2>Access Control</h2>
              <input
                type="search"
                className="search-input"
                placeholder="Search email, UID, or role"
                value={search}
                onChange={(e) => {
                  setSearch(e.target.value);
                  usersGrid.setPage(1);
                }}
              />
              <button
                type="button"
                className="login-button add-button"
                onClick={() => setUserModal({ mode: "create" })}
              >
                Add User
              </button>
            </div>
            <DataGrid
              grid={usersGrid}
              columns={userColumns}
              getRowKey={(u) => u.id}
              emptyText="No users."
            />
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
