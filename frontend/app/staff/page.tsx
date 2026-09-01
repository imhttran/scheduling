"use client";

import { useCallback, useEffect, useState } from "react";
import { callApi } from "@/lib/api";
import { PageHeader } from "@/components/PageHeader";
import { PageFooter } from "@/components/PageFooter";
import { PageTitle } from "@/components/PageTitle";
import { MissRequestModal } from "@/components/MissRequestModal";
import { Pager, usePager } from "@/lib/pagination";

type Shift = {
  id: number;
  date: string;
  startTime: string;
  endTime: string;
  departmentName: string;
};

type Request = {
  id: number;
  workqueueId: number;
  date: string;
  startTime: string;
  endTime: string;
  type: string;
  status: string;
  reason: string | null;
};

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

  const calendarPager = usePager<Shift>(calendar);
  const workqueuePager = usePager<Shift>(workqueue);
  const requestsPager = usePager<Request>(requests);

  return (
    <div className="dashboard-container wide">
      <PageTitle title="My Work Schedule" />
      <PageHeader
        title="My Work Schedule"
        subtitle={
          <>
            Welcome, <span className="highlight">{email}</span>
          </>
        }
      >
        <a className="logout-link" href="/" onClick={logout}>
          Logout
        </a>
      </PageHeader>

      <div className="dashboard-card">
        <div className="user-list-section">
          <h2>
            My Calendar{" "}
            <span className="highlight">
              ({weeklyHours} / {weekHoursCap} hrs this week)
            </span>
          </h2>
          <div className="table-scroll">
            <table className="user-table">
              <thead>
                <tr>
                  <th>Date</th>
                  <th>Start</th>
                  <th>End</th>
                  <th>Department</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {calendar.length === 0 ? (
                  <tr>
                    <td colSpan={5}>No shifts scheduled this week.</td>
                  </tr>
                ) : (
                  calendarPager.pageItems.map((s) => (
                    <tr key={s.id}>
                      <td>{s.date}</td>
                      <td>{s.startTime}</td>
                      <td>{s.endTime}</td>
                      <td>{s.departmentName}</td>
                      <td>
                        <button type="button" onClick={() => setMissShift(s)}>
                          Request to miss
                        </button>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
          <Pager
            pageCount={calendarPager.pageCount}
            currentPage={calendarPager.currentPage}
            onPrev={() => calendarPager.setPage(calendarPager.currentPage - 1)}
            onNext={() => calendarPager.setPage(calendarPager.currentPage + 1)}
          />
        </div>

        <div className="user-list-section">
          <h2>Workqueue</h2>
          <div className="table-scroll">
            <table className="user-table">
              <thead>
                <tr>
                  <th>Date</th>
                  <th>Start</th>
                  <th>End</th>
                  <th>Department</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {workqueue.length === 0 ? (
                  <tr>
                    <td colSpan={5}>No open shifts in your department.</td>
                  </tr>
                ) : (
                  workqueuePager.pageItems.map((s) => (
                    <tr key={s.id}>
                      <td>{s.date}</td>
                      <td>{s.startTime}</td>
                      <td>{s.endTime}</td>
                      <td>{s.departmentName}</td>
                      <td>
                        <button
                          type="button"
                          onClick={() => act(`/api/workqueue/${s.id}/pick`)}
                        >
                          Pick
                        </button>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
          <Pager
            pageCount={workqueuePager.pageCount}
            currentPage={workqueuePager.currentPage}
            onPrev={() =>
              workqueuePager.setPage(workqueuePager.currentPage - 1)
            }
            onNext={() =>
              workqueuePager.setPage(workqueuePager.currentPage + 1)
            }
          />
        </div>

        <div className="user-list-section">
          <h2>My Requests</h2>
          <div className="table-scroll">
            <table className="user-table">
              <thead>
                <tr>
                  <th>Date</th>
                  <th>Start</th>
                  <th>End</th>
                  <th>Type</th>
                  <th>Status</th>
                  <th>Reason</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {requests.length === 0 ? (
                  <tr>
                    <td colSpan={7}>No requests yet.</td>
                  </tr>
                ) : (
                  requestsPager.pageItems.map((r) => (
                    <tr key={r.id}>
                      <td>{r.date}</td>
                      <td>{r.startTime}</td>
                      <td>{r.endTime}</td>
                      <td>{r.type}</td>
                      <td>{r.status}</td>
                      <td>{r.reason ?? ""}</td>
                      <td>
                        {r.status === "pending" && (
                          <button
                            type="button"
                            onClick={() =>
                              act(`/api/me/requests/${r.id}/cancel`)
                            }
                          >
                            Cancel
                          </button>
                        )}
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
          <Pager
            pageCount={requestsPager.pageCount}
            currentPage={requestsPager.currentPage}
            onPrev={() => requestsPager.setPage(requestsPager.currentPage - 1)}
            onNext={() => requestsPager.setPage(requestsPager.currentPage + 1)}
          />
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
