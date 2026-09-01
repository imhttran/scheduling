"use client";

import { useCallback, useEffect, useState } from "react";
import { callApi } from "@/lib/api";
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

const DAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const HOUR_H = 48; // px per hour on the grid

const toMin = (t: string) => {
  const [h, m] = t.split(":").map(Number);
  return h * 60 + m;
};

const fmtTime = (t: string) => {
  const [h, m] = t.split(":").map(Number);
  const hh = h % 12 || 12;
  return `${hh}:${String(m).padStart(2, "0")} ${h >= 12 ? "PM" : "AM"}`;
};

const dateStr = (d: Date) =>
  `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(
    d.getDate(),
  ).padStart(2, "0")}`;

const mondayOf = (dateStr: string) => {
  const d = new Date(`${dateStr}T00:00:00`);
  d.setDate(d.getDate() - ((d.getDay() + 6) % 7));
  return d;
};

const fmtDay = (d: Date) =>
  d.toLocaleDateString(undefined, { month: "short", day: "numeric" });

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

  const monday = mondayOf(anchor);
  const days = Array.from({ length: 7 }, (_, i) => {
    const d = new Date(monday);
    d.setDate(monday.getDate() + i);
    return d;
  });
  const todayStr = dateStr(new Date());

  // Time range for the grid: tight around the shifts, defaulting to 6am–midnight.
  const range = (() => {
    if (shifts.length === 0) return { start: 6 * 60, end: 24 * 60 };
    let start = Infinity;
    let end = -Infinity;
    for (const s of shifts) {
      start = Math.min(start, toMin(s.startTime));
      end = Math.max(end, toMin(s.endTime));
    }
    return {
      start: Math.max(0, Math.floor(start / 60) * 60 - 60),
      end: Math.min(24 * 60, Math.ceil(end / 60) * 60 + 60),
    };
  })();
  const hours: number[] = [];
  for (let h = range.start; h < range.end; h += 60) hours.push(h);
  const totalH = ((range.end - range.start) / 60) * HOUR_H;

  const dayShifts = (d: Date) => shifts.filter((s) => s.date === dateStr(d));

  return (
    <div className="dashboard-container wide">
      <PageTitle title="My Calendar" />
      <PageHeader
        title="My Calendar"
        subtitle={
          <>
            Welcome, <span className="highlight">{email}</span>
          </>
        }
      >
        <a className="page-nav-link" href="/student">
          Back to schedule
        </a>
        <a className="logout-link" href="/" onClick={logout}>
          Logout
        </a>
      </PageHeader>

      <div className="dashboard-card cal-card">
        <div className="cal-toolbar">
          <h2>
            {fmtDay(days[0])} – {fmtDay(days[6])}
          </h2>
          <div className="cal-nav">
            <button
              type="button"
              onClick={() => move(-7)}
              aria-label="Previous week"
            >
              ‹
            </button>
            <button type="button" onClick={goToday}>
              Today
            </button>
            <button
              type="button"
              onClick={() => move(7)}
              aria-label="Next week"
            >
              ›
            </button>
          </div>
        </div>

        <div className="cal-scroll">
          <div className="cal-week">
            <div className="cal-time-axis" style={{ height: totalH }}>
              {hours.map((h) => (
                <span
                  key={h}
                  style={{ top: ((h - range.start) / 60) * HOUR_H }}
                >
                  {fmtTime(`${String(h / 60).padStart(2, "0")}:00`)}
                </span>
              ))}
            </div>
            <div className="cal-days">
              {days.map((d, i) => (
                <div
                  key={i}
                  className={`cal-day${dateStr(d) === todayStr ? " today" : ""}`}
                >
                  <div className="cal-day-header">
                    <span className="cal-day-name">{DAYS[i]}</span>
                    <span className="cal-day-num">{d.getDate()}</span>
                  </div>
                  <div className="cal-day-body" style={{ height: totalH }}>
                    {hours.map((h) => (
                      <div
                        key={h}
                        className="cal-gridline"
                        style={{ top: ((h - range.start) / 60) * HOUR_H }}
                      />
                    ))}
                    {dayShifts(d).map((s) => (
                      <div
                        key={s.id}
                        className="cal-shift"
                        style={{
                          top:
                            ((toMin(s.startTime) - range.start) / 60) * HOUR_H,
                          height:
                            ((toMin(s.endTime) - toMin(s.startTime)) / 60) *
                            HOUR_H,
                        }}
                      >
                        <span className="cal-shift-time">
                          {fmtTime(s.startTime)}–{fmtTime(s.endTime)}
                        </span>
                        <span className="cal-shift-dept">
                          {s.departmentName}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>

        {shifts.length === 0 && (
          <p className="cal-empty">No shifts scheduled this week.</p>
        )}
      </div>

      <PageFooter meta={<span>Student calendar</span>} />
    </div>
  );
}
