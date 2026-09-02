"use client";

import type { CSSProperties, ReactNode } from "react";

// A shift as the calendar grid needs it. Callers may extend this type with
// extra fields (e.g. departmentName) and read them inside renderShift.
export type CalendarShift = {
  id: number;
  date: string;
  startTime: string;
  endTime: string;
};

const DAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const HOUR_H = 48; // px per hour on the grid

export const toMin = (t: string) => {
  const [h, m] = t.split(":").map(Number);
  return h * 60 + m;
};

// 12-hour clock (HH:MM AM/PM); tolerates raw Postgres "HH:MM:SS(.ffffff)" input.
export const fmtTime = (t: string) => {
  const [h, m] = t.split(":");
  const hour = Number(h);
  return `${String(hour % 12 || 12).padStart(2, "0")}:${m} ${
    hour >= 12 ? "PM" : "AM"
  }`;
};

export const dateStr = (d: Date) =>
  `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(
    d.getDate(),
  ).padStart(2, "0")}`;

export const mondayOf = (dateStr: string) => {
  const d = new Date(`${dateStr}T00:00:00`);
  d.setDate(d.getDate() - ((d.getDay() + 6) % 7));
  return d;
};

const fmtDay = (d: Date) =>
  d.toLocaleDateString(undefined, { month: "short", day: "numeric" });

// For a day's shifts, split overlapping ones into side-by-side columns so they
// don't stack on top of each other. Returns a map of shift id → {left, width}
// (percentages) only for shifts that actually share time with another; isolated
// shifts get no entry and keep the full-width default styling.
function layoutDay(
  shifts: CalendarShift[],
): Map<number, { left: number; width: number }> {
  const sorted = [...shifts].sort(
    (a, b) =>
      toMin(a.startTime) - toMin(b.startTime) ||
      toMin(b.endTime) - toMin(a.endTime),
  );
  const layout = new Map<number, { left: number; width: number }>();

  let i = 0;
  while (i < sorted.length) {
    // Grow a cluster of transitively-overlapping shifts.
    const cluster = [sorted[i]];
    let maxEnd = toMin(sorted[i].endTime);
    let j = i + 1;
    while (j < sorted.length && toMin(sorted[j].startTime) < maxEnd) {
      cluster.push(sorted[j]);
      maxEnd = Math.max(maxEnd, toMin(sorted[j].endTime));
      j++;
    }

    if (cluster.length > 1) {
      // Assign each shift to the first free column, then size all columns
      // evenly from the final column count.
      const colEnds: number[] = [];
      const colOf = new Map<number, number>();
      for (const s of cluster) {
        const start = toMin(s.startTime);
        let col = colEnds.findIndex((end) => end <= start);
        if (col === -1) {
          col = colEnds.length;
          colEnds.push(0);
        }
        colEnds[col] = toMin(s.endTime);
        colOf.set(s.id, col);
      }
      const cols = colEnds.length;
      for (const s of cluster) {
        const col = colOf.get(s.id)!;
        layout.set(s.id, { left: (col / cols) * 100, width: 100 / cols });
      }
    }
    i = j;
  }
  return layout;
}

// Shared week-grid calendar. The time axis, day columns, and gridlines are
// rendered here; each shift block is delegated to renderShift so callers can
// style it differently (e.g. a student's own shifts vs. a scheduler's
// department coverage view).
export function WeekCalendar({
  shifts,
  anchor,
  onMove,
  onToday,
  renderShift,
  emptyText,
}: {
  shifts: CalendarShift[];
  anchor: string;
  onMove: (days: number) => void;
  onToday: () => void;
  renderShift: (shift: CalendarShift, style: CSSProperties) => ReactNode;
  emptyText: string;
}) {
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

      <div className="cal-scroll">
        <div className="cal-week">
          <div className="cal-time-axis" style={{ height: totalH }}>
            {hours.map((h) => (
              <span key={h} style={{ top: ((h - range.start) / 60) * HOUR_H }}>
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
                  {(() => {
                    const dayLayout = layoutDay(dayShifts(d));
                    return dayShifts(d).map((s) => {
                      const pos = dayLayout.get(s.id);
                      const style: CSSProperties = {
                        top: ((toMin(s.startTime) - range.start) / 60) * HOUR_H,
                        height:
                          ((toMin(s.endTime) - toMin(s.startTime)) / 60) *
                          HOUR_H,
                      };
                      if (pos) {
                        style.left = `calc(${pos.left}% + 2px)`;
                        style.width = `calc(${pos.width}% - 4px)`;
                      }
                      return renderShift(s, style);
                    });
                  })()}
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      {shifts.length === 0 && <p className="cal-empty">{emptyText}</p>}
    </div>
  );
}
