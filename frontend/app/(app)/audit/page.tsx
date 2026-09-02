"use client";

import { useCallback, useEffect, useState } from "react";
import { API_BASE, callApi } from "@/lib/api";
import { PageHeader } from "@/components/PageHeader";
import { PageFooter } from "@/components/PageFooter";
import { PageTitle } from "@/components/PageTitle";
import { DataGrid, type Column } from "@/components/DataGrid";
import { usePager } from "@/lib/pagination";
import type { AuditEntry } from "@/lib/types";

// Action groups for the filter. Values are the backend's action prefixes.
const FILTERS = [
  { value: "", label: "All activity" },
  { value: "request", label: "Requests" },
  { value: "shift", label: "Shifts" },
  { value: "job", label: "Jobs" },
];

const actionLabel = (action: string) =>
  action
    .split(".")
    .slice(1)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(" ");

// Compact one-line rendering of the row's JSON payload, e.g.
// "type: overflow, workerId: 12, reason: extra hours".
const fmtDetails = (details: AuditEntry["details"]) => {
  if (!details) return "—";
  return Object.entries(details)
    .map(([key, value]) => {
      const text =
        typeof value === "object" && value !== null
          ? JSON.stringify(value)
          : String(value);
      return `${key}: ${text}`;
    })
    .join(", ");
};

export default function AuditPage() {
  const [token, setToken] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState("");
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [filter, setFilter] = useState("");

  const grid = usePager<AuditEntry>(entries, { min: 4 });

  const load = useCallback(async (authToken: string, prefix: string) => {
    const res = await callApi<{ entries: AuditEntry[] }>(
      authToken,
      `/api/audit${prefix ? `?action=${prefix}` : ""}`,
      "GET",
      undefined,
      false,
    );
    if (res) setEntries(res.entries ?? []);
  }, []);

  useEffect(() => {
    (async () => {
      const stored = localStorage.getItem("auth_token");
      if (!stored) {
        window.location.href = "/";
        return;
      }
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
      // Managers and admins get a scoped report (backend enforces the same
      // scoping); everyone else goes back to the dashboard.
      if (me.user.role !== "manager" && me.user.role !== "admin") {
        window.location.href = "/dashboard";
        return;
      }
      setToken(stored);
      setEmail(me.user.email);
      setRole(me.user.role);
      await load(stored, "");
    })();
  }, [load]);

  const changeFilter = (prefix: string) => {
    setFilter(prefix);
    if (token) void load(token, prefix);
  };

  // Downloads the scoped report (same 15-day window + filter) as CSV. Auth is
  // header-based, so a plain link won't do — fetch to a blob and save it.
  const exportCSV = async () => {
    if (!token) return;
    try {
      const response = await fetch(
        `${API_BASE}/api/audit/export${filter ? `?action=${filter}` : ""}`,
        { headers: { Authorization: `Bearer ${token}` } },
      );
      if (!response.ok) return;
      const blob = await response.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "audit-report.csv";
      a.click();
      URL.revokeObjectURL(url);
    } catch {
      alert("Export failed. Is the backend running?");
    }
  };

  const logout = (event: { preventDefault: () => void }) => {
    event.preventDefault();
    localStorage.removeItem("auth_token");
    window.location.href = "/";
  };

  const columns: Column<AuditEntry>[] = [
    {
      label: "When",
      render: (e) => new Date(e.createdAt).toLocaleString(),
    },
    { label: "Who", render: (e) => e.actorEmail ?? "—" },
    { label: "Action", render: (e) => actionLabel(e.action) },
    { label: "Team", render: (e) => e.teamName ?? "—" },
    {
      label: "Entity",
      render: (e) =>
        e.entityId != null ? `${e.entityType} #${e.entityId}` : e.entityType,
    },
    { label: "Details", render: (e) => fmtDetails(e.details) },
  ];

  return (
    <div className="dashboard-container wide">
      <PageTitle title="Audit Report" />
      <PageHeader
        title="Audit Report"
        subtitle="Request, shift, and job actions from the last 15 days, scoped to what you manage."
        right={
          <>
            Welcome, <span className="highlight">{email}</span>
          </>
        }
      />

      <div className="with-sidebar">
        <nav className="sidebar">
          <a href={role === "admin" ? "/admin" : "/manager"}>
            Back to {role === "admin" ? "Admin" : "Manager"}
          </a>
          <a className="logout-link" href="/" onClick={logout}>
            Logout
          </a>
        </nav>

        <div className="dashboard-card">
          <div className="user-list-section">
            <div className="section-title-row">
              <h2>Audit Report</h2>
              <div
                style={{ display: "flex", gap: "8px", alignItems: "center" }}
              >
                <select
                  value={filter}
                  onChange={(event) => changeFilter(event.target.value)}
                >
                  {FILTERS.map((f) => (
                    <option key={f.value} value={f.value}>
                      {f.label}
                    </option>
                  ))}
                </select>
                <button type="button" onClick={() => void exportCSV()}>
                  Export CSV
                </button>
              </div>
            </div>
            <p className="section-hint">
              Managers see every team in their department; admins see
              everything. Older entries drop off after 15 days.
            </p>
            <DataGrid
              grid={grid}
              columns={columns}
              getRowKey={(e) => e.id}
              emptyText="No audit activity in the last 15 days."
            />
          </div>
        </div>
      </div>

      <PageFooter />
    </div>
  );
}
