"use client";

import { useCallback, useEffect, useState } from "react";
import { callApi } from "@/lib/api";
import { PageHeader } from "@/components/PageHeader";
import { PageFooter } from "@/components/PageFooter";
import { PageTitle } from "@/components/PageTitle";
import {
  WeekCalendar,
  dateStr,
  fmtTime,
  mondayOf,
  type CalendarShift,
} from "@/components/WeekCalendar";

type Shift = CalendarShift & {
  departmentName: string;
};

export default function CalendarPage() {
  const [token, setToken] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [shifts, setShifts] = useState<Shift[]>([]);
  const [anchor, setAnchor] = useState(() => dateStr(new Date()));

  const load = useCallback(async (authToken: string, date: string) => {
    const res = await callApi<{ calendar: Shift[] }>(
      authToken,
      `/api/me/calendar?date=${date}`,
      "GET",
      undefined,
      false,
    );
    if (res) setShifts(res.calendar ?? []);
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
      await load(stored, anchor);
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [load]);

  const move = (days: number) => {
    const monday = mondayOf(anchor);
    monday.setDate(monday.getDate() + days);
    const next = dateStr(monday);
    setAnchor(next);
    if (token) void load(token, next);
  };

  const goToday = () => {
    const today = dateStr(new Date());
    setAnchor(today);
    if (token) void load(token, today);
  };

  const logout = (event: { preventDefault: () => void }) => {
    event.preventDefault();
    localStorage.removeItem("auth_token");
    window.location.href = "/";
  };

  return (
    <div className="dashboard-container wide">
      <PageTitle title="My Calendar" />
      <PageHeader
        title="My Calendar"
        right={
          <>
            Welcome, <span className="highlight">{email}</span>
          </>
        }
      />

      <div className="with-sidebar">
        <nav className="sidebar">
          <a href="/student">Back to schedule</a>
          <a className="logout-link" href="/" onClick={logout}>
            Logout
          </a>
        </nav>
        <div className="cal-content">
          <WeekCalendar
            shifts={shifts}
            anchor={anchor}
            onMove={move}
            onToday={goToday}
            emptyText="No shifts scheduled this week."
            renderShift={(s, style) => {
              const shift = s as Shift;
              return (
                <div key={shift.id} className="cal-shift" style={style}>
                  <span className="cal-shift-time">
                    {fmtTime(shift.startTime)}–{fmtTime(shift.endTime)}
                  </span>
                  <span className="cal-shift-dept">{shift.departmentName}</span>
                </div>
              );
            }}
          />
        </div>
      </div>

      <PageFooter meta={<span>Student calendar</span>} />
    </div>
  );
}
