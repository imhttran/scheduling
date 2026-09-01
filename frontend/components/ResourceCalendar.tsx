"use client";

import { useMemo, type CSSProperties, type ReactNode } from "react";
import { fmtTime, mondayOf, toMin } from "./WeekCalendar";

const DAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const HOUR_W = 60; // px per hour across the top
const ROW_H = 40; // px per worker row

// A shift as the resource calendar needs it. Callers may extend this type with
// extra fields (e.g. departmentName) and read them inside renderShift.
export type ResourceShift = {
  id: number;
  date: string;
  startTime: string;
  endTime: string;
  assignedUserId: number;
  assignedName: string;
};

const fmtDay = (d: Date) =>
  d.toLocaleDateString(undefined, { month: "short", day: "numeric" });

// Coverage view: one row per worker (plus an "Open" row), time running across
// the top. Because every worker has their own row, overlapping shifts never
// stack — the view scales to any number of people.
export function ResourceCalendar({
  shifts,
  anchor,
  onMove,
  onToday,
  renderShift,
  emptyText,
}: {
  shifts: ResourceShift[];
  anchor: string;
  onMove: (days: number) => void;
  onToday: () => void;
  renderShift: (shift: ResourceShift, style: CSSProperties) => ReactNode;
  emptyText: string;
}) {
  const monday = mondayOf(anchor);

  // One time range across the whole week, tight around the shifts.
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
  const numHours = (range.end - range.start) / 60;
  const totalW = 7 * numHours * HOUR_W;

  const hours: number[] = [];
  for (let h = range.start; h < range.end; h += 60) hours.push(h);

  const days = Array.from({ length: 7 }, (_, i) => {
    const d = new Date(monday);
    d.setDate(monday.getDate() + i);
    return d;
  });

  const dayIndex = (date: string) => {
    const d = new Date(`${date}T00:00:00`);
    return Math.round((d.getTime() - monday.getTime()) / 86400000);
  };

  const pos = (s: ResourceShift) => ({
    left:
      (dayIndex(s.date) * numHours + (toMin(s.startTime) - range.start) / 60) *
      HOUR_W,
    width: ((toMin(s.endTime) - toMin(s.startTime)) / 60) * HOUR_W,
  });

  // Rows: one per assigned worker, sorted by name.
  const workers = useMemo(() => {
    const map = new Map<number, string>();
    for (const s of shifts) {
      if (s.assignedUserId !== 0 && !map.has(s.assignedUserId)) {
        map.set(s.assignedUserId, s.assignedName);
      }
    }
    return Array.from(map.entries()).sort((a, b) => a[1].localeCompare(b[1]));
  }, [shifts]);

  const openShifts = shifts.filter((s) => s.assignedUserId === 0);

  const trackStyle = (s: ResourceShift): CSSProperties => ({
    ...pos(s),
    top: 2,
    height: ROW_H - 4,
  });

  return (
    <div className="dashboard-card cal-card">
      <div className="cal-toolbar">
        <h2>
          {fmtDay(days[0])} – {fmtDay(days[6])}
        </h2>
        <div className="cal-nav">
          <button
            type="button"
            onClick={() => onMove(-7)}
            aria-label="Previous week"
          >
            ‹
          </button>
          <button type="button" onClick={onToday}>
            Today
          </button>
          <button
            type="button"
            onClick={() => onMove(7)}
            aria-label="Next week"
          >
            ›
          </button>
        </div>
      </div>

      <div className="res-scroll">
        <div className="res-grid">
          {/* Day header */}
          <div className="res-row">
            <div className="res-label">Worker</div>
            <div
              className="res-track res-track-header"
              style={{ width: totalW }}
            >
              {days.map((d, i) => (
                <span
                  key={i}
                  className="res-daylabel"
                  style={{
                    left: i * numHours * HOUR_W,
                    width: numHours * HOUR_W,
                  }}
                >
                  {DAYS[i]} {d.getDate()}
                </span>
              ))}
            </div>
          </div>

          {/* Hour header */}
          <div className="res-row">
            <div className="res-label" />
            <div
              className="res-track res-track-header"
              style={{ width: totalW }}
            >
              {hours.map((h) => (
                <span
                  key={h}
                  className="res-hour"
                  style={{ left: ((h - range.start) / 60) * HOUR_W }}
                >
                  {fmtTime(`${String(h / 60).padStart(2, "0")}:00`)}
                </span>
              ))}
            </div>
          </div>

          {/* Worker rows */}
          {workers.map(([id, name]) => (
            <div className="res-row" key={id}>
              <div className="res-label" title={name}>
                {name}
              </div>
              <div className="res-track" style={{ width: totalW }}>
                {hours.map((h) => (
                  <div
                    key={h}
                    className="res-gridline"
                    style={{ left: ((h - range.start) / 60) * HOUR_W }}
                  />
                ))}
                {days.map((d, i) => (
                  <div
                    key={i}
                    className="res-dayline"
                    style={{ left: i * numHours * HOUR_W }}
                  />
                ))}
                {shifts
                  .filter((s) => s.assignedUserId === id)
                  .map((s) => renderShift(s, trackStyle(s)))}
              </div>
            </div>
          ))}

          {/* Open row */}
          {openShifts.length > 0 && (
            <div className="res-row">
              <div className="res-label">Open</div>
              <div className="res-track" style={{ width: totalW }}>
                {hours.map((h) => (
                  <div
                    key={h}
                    className="res-gridline"
                    style={{ left: ((h - range.start) / 60) * HOUR_W }}
                  />
                ))}
                {days.map((d, i) => (
                  <div
                    key={i}
                    className="res-dayline"
                    style={{ left: i * numHours * HOUR_W }}
                  />
                ))}
                {openShifts.map((s) => renderShift(s, trackStyle(s)))}
              </div>
            </div>
          )}
        </div>
      </div>

      {shifts.length === 0 && <p className="cal-empty">{emptyText}</p>}
    </div>
  );
}
