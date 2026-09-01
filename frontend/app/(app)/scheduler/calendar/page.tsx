"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type FormEvent,
} from "react";
import { callApi } from "@/lib/api";
import { hasRole } from "@/lib/roles";
import { PageHeader } from "@/components/PageHeader";
import { PageFooter } from "@/components/PageFooter";
import { PageTitle } from "@/components/PageTitle";
import { Modal } from "@/components/Modal";
import {
  ResourceCalendar,
  type ResourceShift,
} from "@/components/ResourceCalendar";
import { dateStr, fmtTime, mondayOf, toMin } from "@/components/WeekCalendar";

type Shift = ResourceShift & {
  departmentName: string;
  status: string;
  parentShiftId: number;
};

// A 2-hour slice of a shift, as shown in the assign modal. `taken` means it's
// already covered by an assigned worker and can't be picked again.
type Block = {
  start: string;
  end: string;
  taken: boolean;
};

type Student = {
  id: number;
  email: string;
  name: string;
  workerType: string;
  weekHoursCap: number;
  weekHoursUsed: number;
};

// Deterministic per-worker colors so a worker keeps the same color across the
// week. Open shifts get their own hatched style instead.
const WORKER_COLORS = [
  "#1f6feb",
  "#2ea043",
  "#bf8700",
  "#8250df",
  "#cf222e",
  "#0a7c8a",
  "#953800",
  "#6e40c9",
];
const colorFor = (id: number) => WORKER_COLORS[id % WORKER_COLORS.length];

const hoursBetween = (start: string, end: string) => {
  const [sh, sm] = start.split(":").map(Number);
  const [eh, em] = end.split(":").map(Number);
  return (eh * 60 + em - (sh * 60 + sm)) / 60;
};

const addHours = (t: string, hours: number) => {
  const [h, m] = t.split(":").map(Number);
  const total = h * 60 + m + hours * 60;
  const nh = Math.floor(total / 60);
  const nm = total % 60;
  return `${String(nh).padStart(2, "0")}:${String(nm).padStart(2, "0")}`;
};

// Split a shift (a group of workqueue rows that came from one original shift)
// into 2-hour blocks. A block is taken when an assigned row overlaps it.
const buildBlocks = (group: Shift[]): Block[] => {
  if (group.length === 0) return [];
  let gStart = group[0].startTime;
  let gEnd = group[0].endTime;
  for (const s of group) {
    if (s.startTime < gStart) gStart = s.startTime;
    if (s.endTime > gEnd) gEnd = s.endTime;
  }
  const blocks: Block[] = [];
  for (let t = gStart; toMin(t) < toMin(gEnd); t = addHours(t, 2)) {
    const end = toMin(addHours(t, 2)) > toMin(gEnd) ? gEnd : addHours(t, 2);
    const taken = group.some(
      (s) =>
        s.assignedUserId !== 0 &&
        toMin(s.startTime) < toMin(end) &&
        toMin(t) < toMin(s.endTime),
    );
    blocks.push({ start: t, end, taken });
  }
  return blocks;
};

export default function SchedulerCalendarPage() {
  const [token, setToken] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [shifts, setShifts] = useState<Shift[]>([]);
  const [students, setStudents] = useState<Student[]>([]);
  const [anchor, setAnchor] = useState(() => dateStr(new Date()));
  // Workers (by id) currently hidden from the view; open shifts toggle separately.
  const [hiddenWorkers, setHiddenWorkers] = useState<Set<number>>(new Set());
  const [hideOpen, setHideOpen] = useState(false);
  // The open shift currently being assigned, if any.
  const [assigning, setAssigning] = useState<Shift | null>(null);
  // Selected worker in the assign modal (0 = unassigned). For hourly workers,
  // the scheduler checks one or more 2-hour blocks to assign.
  const [selectedId, setSelectedId] = useState(0);
  const [selectedBlocks, setSelectedBlocks] = useState<Set<string>>(new Set());

  const load = useCallback(async (authToken: string, date: string) => {
    const res = await callApi<{ shifts: Shift[] }>(
      authToken,
      `/api/scheduler/calendar?date=${date}`,
      "GET",
      undefined,
      false,
    );
    if (res) setShifts(res.shifts ?? []);
  }, []);

  const loadStudents = useCallback(async (authToken: string) => {
    const res = await callApi<{ students: Student[] }>(
      authToken,
      "/api/students",
      "GET",
      undefined,
      false,
    );
    if (res) setStudents(res.students ?? []);
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
      if (!hasRole(me.user.role, "manager")) {
        window.location.href = "/dashboard";
        return;
      }
      setEmail(me.user.email);
      await Promise.all([load(stored, anchor), loadStudents(stored)]);
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [load, loadStudents]);

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

  // All workers with shifts this week, for the filter chips.
  const workers = useMemo(() => {
    const map = new Map<number, string>();
    for (const s of shifts) {
      if (s.assignedUserId !== 0 && !map.has(s.assignedUserId)) {
        map.set(s.assignedUserId, s.assignedName);
      }
    }
    return Array.from(map.entries()).sort((a, b) => a[1].localeCompare(b[1]));
  }, [shifts]);

  const toggleWorker = (id: number) => {
    setHiddenWorkers((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const visibleShifts = useMemo(
    () =>
      shifts.filter((s) =>
        s.assignedUserId === 0
          ? !hideOpen
          : !hiddenWorkers.has(s.assignedUserId),
      ),
    [shifts, hiddenWorkers, hideOpen],
  );

  const submitAssign = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!assigning || !token) return;
    const data = new FormData(event.currentTarget);
    const userId = Number(data.get("userId")) || 0;
    const body: Record<string, unknown> = { userId };
    // Send the selected 2-hour blocks; the backend assigns each to the open
    // workqueue row in this shift's group that covers it. With no blocks
    // selected (e.g. reassigning an assigned shift), only userId is sent and the
    // whole shift is assigned.
    if (userId > 0 && selectedBlocks.size > 0) {
      body.blocks = Array.from(selectedBlocks).map((start) => {
        const block = blocks.find((b) => b.start === start)!;
        return { startTime: block.start, endTime: block.end };
      });
    }
    const res = await callApi(
      token,
      `/api/staff/workqueue/${assigning.id}/assign`,
      "POST",
      body,
    );
    if (res) {
      setAssigning(null);
      await Promise.all([load(token, anchor), loadStudents(token)]);
    }
  };

  const openAssign = (shift: Shift) => {
    setAssigning(shift);
    setSelectedId(shift.assignedUserId || 0);
    // Open shifts default to selecting the first open block (the gap after the
    // last assigned block); assigned shifts default to none (whole-shift
    // reassign).
    setSelectedBlocks(
      new Set(shift.assignedUserId === 0 ? [shift.startTime] : []),
    );
  };

  // The full shift the modal is operating on: the clicked row plus any rows
  // split from the same original shift, so taken blocks can be shown.
  const assigningGroup = useMemo(() => {
    if (!assigning) return [];
    const key = assigning.parentShiftId || assigning.id;
    return shifts.filter((s) => (s.parentShiftId || s.id) === key);
  }, [assigning, shifts]);

  const blocks = useMemo(() => buildBlocks(assigningGroup), [assigningGroup]);

  const openBlocks = blocks.filter((b) => !b.taken);

  const toggleBlock = (start: string) => {
    setSelectedBlocks((prev) => {
      const next = new Set(prev);
      if (next.has(start)) next.delete(start);
      else next.add(start);
      return next;
    });
  };

  // Hours the assignment will cover, for the weekly-cap check.
  const assignHours = assigning
    ? Array.from(selectedBlocks).reduce((sum, start) => {
        const b = blocks.find((x) => x.start === start);
        return sum + (b ? hoursBetween(b.start, b.end) : 0);
      }, 0)
    : 0;

  const eligibleStudents = assigning
    ? students.filter(
        (st) =>
          st.id === assigning.assignedUserId || // always keep the current assignee selectable
          st.weekHoursUsed + assignHours <= st.weekHoursCap,
      )
    : [];

  // Open shifts need at least one block selected; reassigning an assigned
  // shift needs none (it takes the whole shift).
  const canSubmit = assigning
    ? assigning.assignedUserId !== 0 || selectedBlocks.size > 0
    : false;

  return (
    <div className="dashboard-container wide">
      <PageTitle title="Coverage Calendar" />
      <PageHeader
        title="Coverage Calendar"
        subtitle={
          <>
            Welcome, <span className="highlight">{email}</span>
          </>
        }
      >
        <a className="page-nav-link" href="/manager">
          Back to schedule
        </a>
        <a className="logout-link" href="/" onClick={logout}>
          Logout
        </a>
      </PageHeader>

      <div className="res-filter">
        {workers.map(([id, name]) => {
          const active = !hiddenWorkers.has(id);
          return (
            <button
              key={id}
              type="button"
              className={`res-filter-chip${active ? "" : " off"}`}
              onClick={() => toggleWorker(id)}
              aria-pressed={active}
            >
              <span
                className="res-filter-swatch"
                style={{ background: colorFor(id) }}
              />
              {name}
            </button>
          );
        })}
        <button
          type="button"
          className={`res-filter-chip${hideOpen ? " off" : ""}`}
          onClick={() => setHideOpen((v) => !v)}
          aria-pressed={!hideOpen}
        >
          <span className="res-filter-swatch res-filter-open" />
          Open
        </button>
      </div>

      <ResourceCalendar
        shifts={visibleShifts}
        anchor={anchor}
        onMove={move}
        onToday={goToday}
        emptyText="No shifts scheduled this week."
        renderShift={(s, style) => {
          const shift = s as Shift;
          const open = shift.assignedUserId === 0;
          const color = colorFor(shift.assignedUserId);
          const content = (
            <>
              <span className="cal-shift-time">
                {fmtTime(shift.startTime)}–{fmtTime(shift.endTime)}
              </span>
              {open && <span className="cal-shift-dept">Assign…</span>}
            </>
          );
          if (open) {
            return (
              <button
                key={shift.id}
                type="button"
                className="cal-shift cal-shift-open"
                style={style}
                onClick={() => openAssign(shift)}
                title="Assign a worker to this shift"
              >
                {content}
              </button>
            );
          }
          return (
            <button
              key={shift.id}
              type="button"
              className="cal-shift"
              style={{
                ...style,
                background: `color-mix(in srgb, ${color} 12%, transparent)`,
                borderColor: `color-mix(in srgb, ${color} 40%, transparent)`,
                borderLeftColor: color,
              }}
              onClick={() => openAssign(shift)}
              title={`${shift.assignedName} — click to reassign`}
            >
              {content}
            </button>
          );
        }}
      />

      {assigning && (
        <Modal
          title={assigning.assignedUserId ? "Reassign Shift" : "Assign Shift"}
          onClose={() => setAssigning(null)}
        >
          <form className="modal-form" onSubmit={submitAssign}>
            <p className="section-hint">
              {assigning.date} · {assigning.startTime}–{assigning.endTime} ·{" "}
              {assigning.departmentName}
            </p>
            {eligibleStudents.length === 0 && (
              <p className="section-hint">
                No other workers available (all at their weekly hour cap).
              </p>
            )}
            <select
              name="userId"
              value={selectedId}
              onChange={(e) => setSelectedId(Number(e.target.value))}
            >
              <option value="">Unassigned</option>
              {eligibleStudents.map((st) => (
                <option key={st.id} value={st.id}>
                  {st.name || st.email} ({st.weekHoursUsed}/{st.weekHoursCap}h)
                </option>
              ))}
            </select>
            {selectedId > 0 && assigning.assignedUserId === 0 && (
              <>
                <div className="modal-label">Coverage blocks</div>
                <div className="block-grid">
                  {blocks.map((b) => {
                    const checked = selectedBlocks.has(b.start);
                    return (
                      <label
                        key={b.start}
                        className={`block-btn${b.taken ? " taken" : ""}${!b.taken && checked ? " selected" : ""}`}
                        title={
                          b.taken ? "Already assigned" : "Assign this block"
                        }
                      >
                        <input
                          type="checkbox"
                          disabled={b.taken}
                          checked={checked}
                          onChange={() => toggleBlock(b.start)}
                        />
                        <span>
                          {fmtTime(b.start)}–{fmtTime(b.end)}
                        </span>
                        {b.taken && <span className="block-taken">taken</span>}
                      </label>
                    );
                  })}
                </div>
                <button
                  type="button"
                  className="block-btn whole"
                  onClick={() =>
                    setSelectedBlocks(new Set(openBlocks.map((b) => b.start)))
                  }
                >
                  Take whole shift
                </button>
                <span className="section-hint">
                  {selectedBlocks.size > 0
                    ? `Covers ${selectedBlocks.size} block${selectedBlocks.size > 1 ? "s" : ""} (${assignHours}h); the rest stays open.`
                    : "Pick one or more blocks, or take the whole shift."}
                </span>
              </>
            )}
            {selectedId > 0 && assigning.assignedUserId !== 0 && (
              <p className="section-hint">
                Reassigns the entire shift to the selected worker.
              </p>
            )}
            <button
              type="submit"
              className="login-button"
              disabled={!canSubmit}
            >
              {assigning.assignedUserId ? "Reassign" : "Assign"}
            </button>
          </form>
        </Modal>
      )}

      <PageFooter meta={<span>Scheduler calendar</span>} />
    </div>
  );
}
