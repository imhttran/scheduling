"use client";

import { useCallback, useEffect, useState, type ReactNode } from "react";
import { callApi } from "@/lib/api";
import { PageHeader } from "@/components/PageHeader";
import { PageFooter } from "@/components/PageFooter";
import { PageTitle } from "@/components/PageTitle";
import { MissRequestModal } from "@/components/MissRequestModal";
import { DataGrid, type Column } from "@/components/DataGrid";
import { fmtTime } from "@/components/WeekCalendar";
import { useSortablePage } from "@/lib/pagination";
import type { Request, Shift } from "@/lib/types";

const hoursBetween = (start: string, end: string) => {
  const [sh, sm] = start.split(":").map(Number);
  const [eh, em] = end.split(":").map(Number);
  return (eh * 60 + em - (sh * 60 + sm)) / 60;
};

export default function StaffPage() {
  const [token, setToken] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [weekHoursCap, setWeekHoursCap] = useState(20);
  const [calendar, setCalendar] = useState<Shift[]>([]);
  const [workqueue, setWorkqueue] = useState<Shift[]>([]);
  const [requests, setRequests] = useState<Request[]>([]);
  const [missShift, setMissShift] = useState<Shift | null>(null);

  const load = useCallback(async (authToken: string) => {
    const [cal, wq, req] = await Promise.all([
      callApi<{ calendar: Shift[] }>(
        authToken,
        "/api/me/calendar",
        "GET",
        undefined,
        false,
      ),
      callApi<{ workqueue: Shift[] }>(
        authToken,
        "/api/workqueue",
        "GET",
        undefined,
        false,
      ),
      callApi<{ requests: Request[] }>(
        authToken,
        "/api/me/requests",
        "GET",
        undefined,
        false,
      ),
    ]);
    if (cal) setCalendar(cal.calendar ?? []);
    if (wq) setWorkqueue(wq.workqueue ?? []);
    if (req) setRequests(req.requests ?? []);
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
        user: { email: string; role: string; weekHoursCap?: number };
      }>(stored, "/api/me", "GET", undefined, false);
      if (!me) {
        localStorage.removeItem("auth_token");
        window.location.href = "/";
        return;
      }
      if (me.user.role !== "staff") {
        window.location.href = "/dashboard";
        return;
      }
      setEmail(me.user.email);
      if (me.user.weekHoursCap) setWeekHoursCap(me.user.weekHoursCap);
      await load(stored);
    })();
  }, [load]);

  const act = (path: string, body?: unknown) => {
    if (!token) return;
    void (async () => {
      const result = await callApi(token, path, "POST", body);
      if (result) await load(token);
    })();
  };

  const logout = (event: { preventDefault: () => void }) => {
    event.preventDefault();
    localStorage.removeItem("auth_token");
    window.location.href = "/";
  };

  const weeklyHours = calendar.reduce(
    (sum, s) => sum + hoursBetween(s.startTime, s.endTime),
    0,
  );

  const calendarGrid = useSortablePage<Shift>(
    calendar,
    (s, key) => s[key as keyof Shift],
    "date",
  );
  const workqueueGrid = useSortablePage<Shift>(
    workqueue,
    (s, key) => s[key as keyof Shift],
    "date",
  );
  const requestsGrid = useSortablePage<Request>(
    requests,
    (r, key) => r[key as keyof Request],
    "date",
  );

  const shiftColumns = (action: (s: Shift) => ReactNode): Column<Shift>[] => [
    { key: "date", label: "Date", sortable: true },
    {
      key: "startTime",
      label: "Start",
      sortable: true,
      render: (s) => fmtTime(s.startTime),
    },
    {
      key: "endTime",
      label: "End",
      sortable: true,
      render: (s) => fmtTime(s.endTime),
    },
    { key: "departmentName", label: "Department", sortable: true },
    { label: "", render: action },
  ];

  const myCalendarColumns = shiftColumns((s) => (
    <button type="button" onClick={() => setMissShift(s)}>
      Request to miss
    </button>
  ));
  const workqueueColumns = shiftColumns((s) => (
    <button type="button" onClick={() => act(`/api/workqueue/${s.id}/pick`)}>
      Pick
    </button>
  ));
  const requestColumns: Column<Request>[] = [
    { key: "date", label: "Date", sortable: true },
    {
      key: "startTime",
      label: "Start",
      sortable: true,
      render: (r) => fmtTime(r.startTime),
    },
    {
      key: "endTime",
      label: "End",
      sortable: true,
      render: (r) => fmtTime(r.endTime),
    },
    { key: "type", label: "Type", sortable: true },
    { key: "status", label: "Status", sortable: true },
    { key: "reason", label: "Reason", sortable: true },
    {
      label: "",
      render: (r) =>
        r.status === "pending" ? (
          <button
            type="button"
            onClick={() => act(`/api/me/requests/${r.id}/cancel`)}
          >
            Cancel
          </button>
        ) : null,
    },
  ];

  return (
    <div className="dashboard-container wide">
      <PageTitle title="My Work Schedule" />
      <PageHeader
        title="My Work Schedule"
        right={
          <>
            Welcome, <span className="highlight">{email}</span>
          </>
        }
      />

      <div className="with-sidebar">
        <nav className="sidebar">
          <a className="logout-link" href="/" onClick={logout}>
            Logout
          </a>
        </nav>
        <div className="dashboard-card">
          <div className="user-list-section">
            <h2>
              My Calendar{" "}
              <span className="highlight">
                ({weeklyHours} / {weekHoursCap} hrs this week)
              </span>
            </h2>
            <DataGrid
              grid={calendarGrid}
              columns={myCalendarColumns}
              getRowKey={(s) => s.id}
              emptyText="No shifts scheduled this week."
            />
          </div>

          <div className="user-list-section">
            <h2>Workqueue</h2>
            <DataGrid
              grid={workqueueGrid}
              columns={workqueueColumns}
              getRowKey={(s) => s.id}
              emptyText="No open shifts in your department."
            />
          </div>

          <div className="user-list-section">
            <h2>My Requests</h2>
            <DataGrid
              grid={requestsGrid}
              columns={requestColumns}
              getRowKey={(r) => r.id}
              emptyText="No requests yet."
            />
          </div>
        </div>
      </div>

      <PageFooter meta={<span>Staff schedule</span>} />

      <MissRequestModal
        shift={missShift}
        onClose={() => setMissShift(null)}
        onSubmit={(reason) => {
          if (missShift)
            act("/api/me/requests", {
              workqueueId: missShift.id,
              type: "miss",
              reason,
            });
          setMissShift(null);
        }}
      />
    </div>
  );
}
