#!/usr/bin/env node
// Resets demo-mutated state via the API and re-adds the one piece of prep
// data the student scene needs, so record.mjs can be re-run on a live DB.
//
// Cleans (all off-camera):
//   - deletes the demo accounts newuser@mail.edu and student4@mail.com
//   - cancels the miss requests the student scene created (Family event,
//     Exam preparation on 08-31) and recreates the pre-existing Class
//     conflict request if a previous run denied it
//   - un-assigns the shifts the demo picks/assigns (student7's 2h block,
//     Alex's dining pick, Dana's business-service pick, Tom's Thursday slot)
//
// Preps:
//   - adds the 2h dining workqueue shift on 2026-09-04 11:00-13:00 that
//     student7 picks on camera (the 8h slots are all above his 20h cap)
//
// Usage: node scripts/demo/reset-demo.mjs   (backend on :8080)

const BASE = "http://localhost:8080";

async function api(token, path, method = "GET", body) {
  const res = await fetch(BASE + path, {
    method,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok)
    throw new Error(
      `${method} ${path} -> ${res.status}: ${JSON.stringify(data)}`,
    );
  return data;
}

// Full login: password -> 2FA (dev code is always 1234).
async function login(email, password) {
  const first = await api(null, "/api/login", "POST", {
    email,
    password,
    deviceId: "demo-reset",
  });
  if (!first.twoFactorRequired) return first.token;
  const verified = await api(null, "/api/login/verify", "POST", {
    token: first.token,
    code: "1234",
    deviceId: "demo-reset",
  });
  return verified.token;
}

const log = (...m) => console.log("[reset]", ...m);

// ---- 1. delete demo accounts ----
const admin = await login("admin@mail.edu", "Password1234!");
const users = (await api(admin, "/api/users")).users ?? [];
for (const email of ["newuser@mail.edu", "student4@mail.com"]) {
  const u = users.find((u) => u.email === email);
  if (u) {
    await api(admin, `/api/users/${u.id}`, "DELETE");
    log("deleted user", email);
  } else {
    log("user absent (ok)", email);
  }
}

// ---- 2. student7's requests ----
const s7 = await login("student7@mail.edu", "Student1234!");
const mine = (await api(s7, "/api/me/requests")).requests ?? [];
for (const r of mine) {
  const isOurs =
    r.status === "pending" &&
    (r.reason === "Family event" ||
      (r.reason === "Exam preparation" && r.date === "2026-08-31"));
  if (isOurs) {
    await api(s7, `/api/me/requests/${r.id}/cancel`, "POST");
    log("cancelled request", r.id, r.reason, r.date);
  }
}
// The scheduler scene denies this one; recreate it so the demo can re-run.
const hasClassConflict = mine.some(
  (r) =>
    r.date === "2026-09-02" &&
    r.reason === "Class conflict" &&
    r.status === "pending",
);
if (!hasClassConflict) {
  await api(s7, "/api/me/requests", "POST", {
    workqueueId: 5,
    type: "miss",
    reason: "Class conflict",
  });
  log("recreated Class conflict request on 2026-09-02");
} else {
  log("Class conflict request already pending");
}

// ---- 3. un-assign the shifts the demo assigns ----
// Maya's scope covers dining; admin covers everything (Dana is in another
// department's team).
const manager = await login("manager@mail.edu", "Manager1234!");
const slots = (await api(manager, "/api/staff/workqueue")).shifts ?? [];
const unassign = async (token, row, why) => {
  await api(manager, `/api/staff/workqueue/${row.id}/assign`, "POST", {
    userId: 0,
  });
  log(
    "unassigned",
    row.id,
    row.date,
    row.startTime,
    row.teamName,
    `(${row.assignedEmail})`,
  );
};

for (const s of slots) {
  if (s.assignedUserId === 0) continue;
  if (
    s.assignedEmail === "student7@mail.edu" &&
    s.date === "2026-09-04" &&
    s.startTime.startsWith("11:")
  ) {
    await unassign(manager, s, "george pick");
  }
  if (s.assignedEmail === "fulltime1@mail.edu" && s.date === "2026-09-02") {
    await unassign(manager, s, "alex pick");
  }
  if (s.assignedEmail === "student20@mail.edu" && s.date === "2026-09-03") {
    await unassign(manager, s, "tom reassign");
  }
  // Block assignment splits the slot into 2h rows — unassign every row the
  // demo left on Priya for 09-03.
  if (s.assignedEmail === "student16@mail.edu" && s.date === "2026-09-03") {
    await unassign(manager, s, "priya assign");
  }
}

// Dana (hourly4) works in another department's team — use the admin token.
const allSlots = (await api(admin, "/api/staff/workqueue")).shifts ?? [];
for (const s of allSlots) {
  if (s.assignedEmail === "hourly4@mail.edu" && s.date === "2026-09-02") {
    await api(admin, `/api/staff/workqueue/${s.id}/assign`, "POST", {
      userId: 0,
    });
    log("unassigned (admin)", s.id, s.date, s.teamName, "(dana pick)");
  }
}

// ---- 4. prep: the 2h dining slot student7 picks on camera ----
const hasPrep = slots.some(
  (s) =>
    s.date === "2026-09-04" &&
    s.startTime.startsWith("11:") &&
    s.teamName === "dining",
);
if (!hasPrep) {
  await api(manager, "/api/workqueue", "POST", {
    teamId: 1,
    date: "2026-09-04",
    startTime: "11:00",
    endTime: "13:00",
  });
  log("added prep shift 2026-09-04 11:00-13:00 dining");
} else {
  log("prep shift already present");
}

log("done");
