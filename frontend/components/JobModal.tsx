"use client";

import { useState, type FormEvent } from "react";
import { Modal } from "./Modal";

export type JobScheduleInput = {
  dayOfWeek: number;
  startTime: string;
  endTime: string;
  hours: number;
};
export type JobHolidayInput = {
  date: string;
  reason?: string | null;
};
export type JobInput = {
  id?: number;
  name: string;
  teamId: number;
  optimalWorkers: number;
  schedules: JobScheduleInput[];
  holidays: JobHolidayInput[];
};

const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

// Default operating hours for a new job: weekdays 9am-5pm (8h, 40h/wk),
// weekends 10h (20h/wk). Mirrors defaultJobSchedules() in the backend.
const DEFAULT_SCHEDULES: Record<
  number,
  { startTime: string; endTime: string }
> = {
  1: { startTime: "09:00", endTime: "17:00" },
  2: { startTime: "09:00", endTime: "17:00" },
  3: { startTime: "09:00", endTime: "17:00" },
  4: { startTime: "09:00", endTime: "17:00" },
  5: { startTime: "09:00", endTime: "17:00" },
  6: { startTime: "10:00", endTime: "20:00" },
  0: { startTime: "10:00", endTime: "20:00" },
};

// Create/edit form for a job: basic info, optimal staff, per-day operating
// hours, and one-off holiday closures. Stateful so the dynamic schedule and
// holiday lists can be added/removed before submitting.
export function JobModal({
  job,
  teams,
  onSave,
  onClose,
}: {
  job: JobInput | null;
  teams: { id: number; name: string; departmentName: string }[];
  onSave: (data: JobInput) => void;
  onClose: () => void;
}) {
  const [name, setName] = useState(job?.name ?? "");
  const [teamId, setTeamId] = useState<number>(job?.teamId ?? 0);
  const [optimalWorkers, setOptimalWorkers] = useState(
    job?.optimalWorkers ?? 1,
  );
  const [schedules, setSchedules] = useState<(JobScheduleInput | null)[]>(() =>
    DAYS.map((_, dow) => {
      const existing = job?.schedules.find((s) => s.dayOfWeek === dow);
      if (existing) return existing;
      if (job) return null; // editing: leave blank days closed
      const def = DEFAULT_SCHEDULES[dow];
      return def ? { dayOfWeek: dow, ...def, hours: 0 } : null;
    }),
  );
  const [holidays, setHolidays] = useState<JobHolidayInput[]>(
    job?.holidays ?? [],
  );
  const [holidayDate, setHolidayDate] = useState("");
  const [holidayReason, setHolidayReason] = useState("");

  const updateSchedule = (dow: number, changes: Partial<JobScheduleInput>) => {
    setSchedules((prev) => {
      const copy = [...prev];
      const cur = copy[dow] ?? {
        dayOfWeek: dow,
        startTime: "",
        endTime: "",
        hours: 0,
      };
      const merged = { ...cur, ...changes };
      copy[dow] = merged.startTime || merged.endTime ? merged : null;
      return copy;
    });
  };

  const addHoliday = () => {
    if (!holidayDate || holidays.some((h) => h.date === holidayDate)) return;
    setHolidays([
      ...holidays,
      { date: holidayDate, reason: holidayReason.trim() || null },
    ]);
    setHolidayDate("");
    setHolidayReason("");
  };

  const removeHoliday = (date: string) =>
    setHolidays(holidays.filter((h) => h.date !== date));

  const submit = (e: FormEvent) => {
    e.preventDefault();
    onSave({
      ...(job?.id ? { id: job.id } : {}),
      name: name.trim(),
      teamId,
      optimalWorkers: optimalWorkers || 1,
      schedules: schedules.filter((s): s is JobScheduleInput => s !== null),
      holidays,
    });
  };

  return (
    <Modal title={job ? "Edit Job" : "Add Job"} onClose={onClose}>
      <form className="modal-form" onSubmit={submit}>
        <input
          type="text"
          name="name"
          placeholder="Job name"
          required
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <select
          name="teamId"
          required
          value={teamId}
          onChange={(e) => setTeamId(Number(e.target.value))}
        >
          <option value={0} disabled>
            Select team
          </option>
          {teams.map((d) => (
            <option key={d.id} value={d.id}>
              {d.name} ({d.departmentName})
            </option>
          ))}
        </select>
        <label className="modal-label">
          Optimal workers
          <input
            type="number"
            name="optimalWorkers"
            min={1}
            value={optimalWorkers}
            onChange={(e) => setOptimalWorkers(Number(e.target.value))}
          />
        </label>
        <fieldset className="modal-fieldset">
          <legend>Daily operating hours</legend>
          <p className="section-hint">
            Leave a day blank to mark it closed (weekend, holiday, etc.).
          </p>
          {DAYS.map((day, dow) => {
            const s = schedules[dow];
            return (
              <div className="schedule-row" key={dow}>
                <span className="schedule-day">{day}</span>
                <input
                  type="time"
                  value={s?.startTime ?? ""}
                  onChange={(e) =>
                    updateSchedule(dow, { startTime: e.target.value })
                  }
                />
                <span>to</span>
                <input
                  type="time"
                  value={s?.endTime ?? ""}
                  onChange={(e) =>
                    updateSchedule(dow, { endTime: e.target.value })
                  }
                />
              </div>
            );
          })}
        </fieldset>
        <fieldset className="modal-fieldset">
          <legend>Holiday closures</legend>
          <p className="section-hint">
            Date-specific closings that override the weekly hours above.
          </p>
          {holidays.length > 0 && (
            <ul className="holiday-list">
              {holidays.map((h) => (
                <li key={h.date}>
                  <span>
                    {h.date}
                    {h.reason ? ` — ${h.reason}` : ""}
                  </span>
                  <button
                    type="button"
                    className="holiday-remove"
                    onClick={() => removeHoliday(h.date)}
                  >
                    ×
                  </button>
                </li>
              ))}
            </ul>
          )}
          <div className="schedule-row">
            <input
              type="date"
              value={holidayDate}
              onChange={(e) => setHolidayDate(e.target.value)}
            />
            <input
              type="text"
              placeholder="Reason (optional)"
              value={holidayReason}
              onChange={(e) => setHolidayReason(e.target.value)}
            />
            <button
              type="button"
              className="login-button holiday-add"
              onClick={addHoliday}
            >
              Add
            </button>
          </div>
        </fieldset>
        <div className="modal-actions">
          <button type="button" className="cancel-button" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="login-button">
            Save
          </button>
        </div>
      </form>
    </Modal>
  );
}
