"use client";

import { useCallback, useEffect, useState, type FormEvent } from "react";
import { API_BASE, callApi } from "@/lib/api";
import { PageHeader } from "@/components/PageHeader";
import { PageFooter } from "@/components/PageFooter";
import { PageTitle } from "@/components/PageTitle";

type Shift = {
  id: number;
  date: string;
  startTime: string;
  endTime: string;
  departmentName: string;
};

const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

export default function StudentPage() {
  const [token, setToken] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [calendar, setCalendar] = useState<Shift[]>([]);
  const [workqueue, setWorkqueue] = useState<Shift[]>([]);

  const load = useCallback(async (authToken: string) => {
    const [cal, wq] = await Promise.all([
      callApi<{ calendar: Shift[] }>(authToken, "/api/me/calendar", "GET", undefined, false),
      callApi<{ workqueue: Shift[] }>(authToken, "/api/workqueue", "GET", undefined, false),
    ]);
    if (cal) setCalendar(cal.calendar ?? []);
    if (wq) setWorkqueue(wq.workqueue ?? []);
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
      if (me.user.role !== "student") {
        window.location.href = "/dashboard";
        return;
      }
      setEmail(me.user.email);
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

  const handlePreference = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    act("/api/me/preferences", {
      dayOfWeek: Number(data.get("dayOfWeek")),
      startTime: data.get("startTime"),
      endTime: data.get("endTime"),
    });
    event.currentTarget.reset();
  };

  const logout = (event: { preventDefault: () => void }) => {
    event.preventDefault();
    localStorage.removeItem("auth_token");
    window.location.href = "/";
  };

  return (
    <div className="dashboard-container wide">
      <PageTitle title="My Schedule" />
      <PageHeader title="My Schedule" subtitle={<>Welcome, <span className="highlight">{email}</span></>}>
        <a className="logout-link" href="/" onClick={logout}>Logout</a>
      </PageHeader>

      <div className="dashboard-card">
        <div className="user-list-section">
          <h2>My Calendar</h2>
          <div className="table-scroll">
            <table className="user-table">
              <thead>
                <tr><th>Date</th><th>Start</th><th>End</th><th>Department</th><th></th></tr>
              </thead>
              <tbody>
                {calendar.length === 0 ? (
                  <tr><td colSpan={5}>No shifts scheduled this week.</td></tr>
                ) : (
                  calendar.map((s) => (
                    <tr key={s.id}>
                      <td>{s.date}</td>
                      <td>{s.startTime}</td>
                      <td>{s.endTime}</td>
                      <td>{s.departmentName}</td>
                      <td>
                        <button type="button" onClick={() => act("/api/me/requests", { workqueueId: s.id, type: "miss", reason: "Unavailable" })}>
                          Request to miss
                        </button>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </div>

        <div className="user-list-section">
          <h2>Workqueue</h2>
          <div className="table-scroll">
            <table className="user-table">
              <thead>
                <tr><th>Date</th><th>Start</th><th>End</th><th>Department</th><th></th></tr>
              </thead>
              <tbody>
                {workqueue.length === 0 ? (
                  <tr><td colSpan={5}>No open shifts in your department.</td></tr>
                ) : (
                  workqueue.map((s) => (
                    <tr key={s.id}>
                      <td>{s.date}</td>
                      <td>{s.startTime}</td>
                      <td>{s.endTime}</td>
                      <td>{s.departmentName}</td>
                      <td>
                        <button type="button" onClick={() => act(`/api/workqueue/${s.id}/pick`)}>Pick</button>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </div>

        <div className="user-list-section">
          <h2>Preferred Days &amp; Times</h2>
          <form className="add-user-form" onSubmit={handlePreference}>
            <select name="dayOfWeek" defaultValue="1">
              {DAYS.map((d, i) => (
                <option key={i} value={i}>{d}</option>
              ))}
            </select>
            <input type="time" name="startTime" required />
            <input type="time" name="endTime" required />
            <button type="submit" className="login-button">Add</button>
          </form>
        </div>
      </div>

      <PageFooter meta={<span>Student schedule</span>} />
    </div>
  );
}
