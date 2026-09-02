#!/usr/bin/env node
// Records the full-application demo scenes for the HTT Scheduling promo video.
// Produces out/video/NN-<name>.webm (1440x900, one file per scene)
// plus out/shots/login.png and out/shots/calendar.png for the title cards.
//
// Scenes:
//   01-signup     self-serve signup on the login page
//   02-login      student7 login + device verification (code 1234)
//   03-student    student actions: pick, request-to-miss, cancel, preferences
//   04-workers    full-time + part-time (hourly) staff views
//   05-admin      admin: org sections, add user, edit user, sort + paging
//   06-onboard    admin-created account: temp password -> profile -> dashboard
//   07-manager    department manager: org chart, add shift, approve, calendar
//   08-scheduler  team-scoped manager: deny, calendar assign/unassign/reassign
//   09-audit      department manager opens the scoped audit report
//
// Usage: node scripts/demo/record.mjs   (app must be running on :3000)

import { chromium } from "playwright";
import { mkdirSync, readdirSync, renameSync, rmSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const HERE = dirname(fileURLToPath(import.meta.url));
const OUT = join(HERE, "out");
const BASE = "http://localhost:3000";

const t0 = Date.now();
const log = (m) =>
  console.log(`[+${((Date.now() - t0) / 1000).toFixed(1)}s] ${m}`);
const sleep = (page, ms) => page.waitForTimeout(ms);

// Injected into every page: hide the Next.js dev-tools button, slim scrollbars,
// and draw an orange ripple wherever the "mouse" clicks so viewers can follow.
const INIT = `
(() => {
  const inject = () => {
    if (document.getElementById('demo-style')) return;
    const st = document.createElement('style');
    st.id = 'demo-style';
    st.textContent = [
      'nextjs-portal{display:none!important}',
      '::-webkit-scrollbar{width:10px;height:10px}',
      '::-webkit-scrollbar-thumb{background:rgba(128,128,128,.4);border-radius:5px}',
      '::-webkit-scrollbar-track{background:transparent}',
      '.demo-ripple{position:fixed;z-index:2147483647;border:3px solid #BF5700;border-radius:9999px;pointer-events:none;transform:translate(-50%,-50%);animation:demoRipple .55s ease-out forwards}',
      '@keyframes demoRipple{0%{width:16px;height:16px;opacity:.95}100%{width:64px;height:64px;opacity:0}}'
    ].join(' ');
    (document.head || document.documentElement).appendChild(st);
  };
  inject();
  document.addEventListener('readystatechange', inject);
  document.addEventListener('pointerdown', (e) => {
    const d = document.createElement('div');
    d.className = 'demo-ripple';
    d.style.left = e.clientX + 'px';
    d.style.top = e.clientY + 'px';
    (document.body || document.documentElement).appendChild(d);
    setTimeout(() => d.remove(), 650);
  }, true);
})();
`;

// Some actions (Pick, Approve, dialog submits) re-render their own row or
// dialog the instant they succeed. Playwright's click retry loop then loses
// the detached element and throws a 30s timeout even though the action
// landed. Dispatch these clicks the way the browser would instead, with a
// synthetic pointerdown so the click ripple still shows on camera.
async function clickBtn(page, { text, within, rowText }) {
  const res = await page.evaluate(
    ({ text, within, rowText }) => {
      const root = within ? document.querySelector(within) : document;
      if (!root) return "no root: " + within;
      const needles =
        rowText == null ? [] : Array.isArray(rowText) ? rowText : [rowText];
      const btn = Array.from(root.querySelectorAll("button")).find((b) => {
        if (b.textContent.trim() !== text) return false;
        if (needles.length === 0) return true;
        const scope = b.closest("tr") || b.parentElement;
        return scope
          ? needles.every((n) => scope.textContent.includes(n))
          : false;
      });
      if (!btn) return "not found: " + text;
      const r = btn.getBoundingClientRect();
      btn.dispatchEvent(
        new PointerEvent("pointerdown", {
          bubbles: true,
          clientX: r.x + r.width / 2,
          clientY: r.y + r.height / 2,
        }),
      );
      btn.click();
      return "ok";
    },
    { text, within, rowText },
  );
  if (res !== "ok") throw new Error("clickBtn failed: " + res);
}

// Sidebar link by exact visible text — the "move around the page" beat.
async function sidebar(page, text) {
  const link = page.locator("nav.sidebar a", { hasText: text }).first();
  await link.scrollIntoViewIfNeeded();
  await sleep(page, 350);
  await link.click();
  await sleep(page, 1200);
}

// Click an "Open" chip on the resource calendar by day label + time text.
// The Open row holds every unassigned shift; each day header label ("Thu 3")
// carries a left/width that tells us which day a chip sits on.
// Click an "Open" chip on the resource calendar by day label. Returns the
// chip's time text (e.g. "09:00 AM–11:00 AM") so follow-up clicks can target
// the same slot after it moves to a worker's row. Picks the FIRST open chip
// in that day column.
async function clickOpenChip(page, dayLabel) {
  const res = await page.evaluate(
    ({ dayLabel }) => {
      const rows = Array.from(document.querySelectorAll(".res-row"));
      const openRow = rows.find(
        (r) => r.querySelector(".res-label")?.textContent.trim() === "Open",
      );
      if (!openRow) return "no Open row";
      const header = rows.find((r) => r.querySelector(".res-daylabel"));
      const days = header
        ? Array.from(header.querySelectorAll(".res-daylabel")).map((d) => ({
            text: d.textContent.trim(),
            left: parseFloat(d.style.left) || 0,
            width: parseFloat(d.style.width) || 0,
          }))
        : [];
      const btns = Array.from(openRow.querySelectorAll("button"));
      const hit = btns.find((b) => {
        const center = parseFloat(b.style.left) + b.offsetWidth / 2;
        const day = days.find(
          (d) => center >= d.left && center < d.left + d.width,
        );
        return day && day.text === dayLabel;
      });
      if (!hit)
        return `no open chip: ${dayLabel} (have ${btns.map((b) => b.textContent.trim()).join(" | ")})`;
      const timeText =
        hit.querySelector(".cal-shift-time")?.textContent.trim() ?? "";
      const r = hit.getBoundingClientRect();
      hit.scrollIntoView({ block: "center", inline: "center" });
      hit.dispatchEvent(
        new PointerEvent("pointerdown", {
          bubbles: true,
          clientX: r.x + r.width / 2,
          clientY: r.y + r.height / 2,
        }),
      );
      hit.click();
      return JSON.stringify({ ok: true, timeText });
    },
    { dayLabel },
  );
  if (!res.startsWith("{")) throw new Error("clickOpenChip failed: " + res);
  return JSON.parse(res).timeText;
}

// Same, but for an assigned chip inside a named worker's row.
// Click an assigned chip inside a named worker's row. Without timeText,
// clicks the row's first chip.
async function clickWorkerChip(page, workerName, timeText) {
  const res = await page.evaluate(
    ({ workerName, timeText }) => {
      const rows = Array.from(document.querySelectorAll(".res-row"));
      const row = rows.find(
        (r) => r.querySelector(".res-label")?.textContent.trim() === workerName,
      );
      if (!row) return "no row for " + workerName;
      const btns = Array.from(row.querySelectorAll("button"));
      const btn = timeText
        ? btns.find((b) => b.textContent.includes(timeText))
        : btns[0];
      if (!btn) return "no chip in " + workerName;
      const r = btn.getBoundingClientRect();
      btn.scrollIntoView({ block: "center", inline: "center" });
      btn.dispatchEvent(
        new PointerEvent("pointerdown", {
          bubbles: true,
          clientX: r.x + r.width / 2,
          clientY: r.y + r.height / 2,
        }),
      );
      btn.click();
      return "ok";
    },
    { workerName, timeText },
  );
  if (res !== "ok") throw new Error("clickWorkerChip failed: " + res);
}

// Sortable-column header inside a section (th text includes the label).
async function clickSort(page, sectionId, label) {
  const th = page
    .locator(`#${sectionId} th.sortable`, { hasText: label })
    .first();
  await th.scrollIntoViewIfNeeded();
  await sleep(page, 300);
  await th.click();
  await sleep(page, 900);
}

async function newCtx(browser, name) {
  const dir = join(OUT, "video", name);
  mkdirSync(dir, { recursive: true });
  // Drop stale partials from previous runs so the newest webm is unambiguous.
  for (const f of readdirSync(dir))
    if (f.endsWith(".webm")) rmSync(join(dir, f));
  const ctx = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    recordVideo: { dir, size: { width: 1440, height: 900 } },
    deviceScaleFactor: 1,
  });
  await ctx.addInitScript(INIT);
  return ctx;
}

async function closeCtx(ctx, name) {
  for (const p of ctx.pages()) await p.close();
  await ctx.close();
  const dir = join(OUT, "video", name);
  const files = readdirSync(dir)
    .filter((f) => f.endsWith(".webm"))
    .map((f) => ({ f, m: statSync(join(dir, f)).mtimeMs }))
    .sort((a, b) => b.m - a.m);
  const newest = files[0];
  if (!newest || statSync(join(dir, newest.f)).size === 0) {
    throw new Error(`no valid video produced for ${name}`);
  }
  renameSync(join(dir, newest.f), join(OUT, "video", `${name}.webm`));
  log(`scene saved: ${name}.webm`);
}

// Watch for alert()/confirm() dialogs so they never block the recording;
// log the text for debugging (the dialog itself is browser chrome and does
// not show on camera — narration carries the meaning instead).
function watchDialogs(page) {
  page.on("dialog", (d) => {
    console.log(`    dialog[${d.type()}]: ${d.message()}`);
    void d.dismiss();
  });
}

async function login(page, email, password, { delay = 40, pause = 500 } = {}) {
  await page.goto(BASE + "/");
  await page.waitForLoadState("networkidle");
  await sleep(page, 2200);
  const emailBox = page.getByRole("textbox", { name: "Email or UID" });
  await emailBox.click();
  await emailBox.pressSequentially(email, { delay });
  await sleep(page, pause);
  const pwBox = page.getByRole("textbox", { name: "Password" });
  await pwBox.click();
  await pwBox.pressSequentially(password, { delay });
  await sleep(page, pause);
  await page.getByRole("button", { name: "Sign In" }).click();
  await sleep(page, 1500);
}

async function twoFactor(page, urlRe, { codePause = 450 } = {}) {
  await page
    .getByRole("textbox", { name: "Verification Code" })
    .waitFor({ timeout: 10000 });
  await sleep(page, 900);
  for (let i = 0; i < 4; i++) {
    await page.locator("#code-" + i).fill("1234"[i]);
    await sleep(page, codePause);
  }
  await page.waitForURL(urlRe, { timeout: 30000 });
  await page.waitForLoadState("networkidle");
}

const browser = await chromium.launch({ args: ["--mute-audio"] });
mkdirSync(join(OUT, "video"), { recursive: true });
mkdirSync(join(OUT, "shots"), { recursive: true });

try {
  // ---------- Scene 01: self-serve signup ----------
  log("scene 01: signup");
  const ctx1 = await newCtx(browser, "01-signup");
  const p1 = await ctx1.newPage();
  watchDialogs(p1);
  await p1.goto(BASE + "/");
  await p1.waitForLoadState("networkidle");
  await sleep(p1, 2500);
  await p1.locator("a", { hasText: "Sign up" }).first().click();
  await p1.getByRole("heading", { name: "Create Account" }).waitFor();
  await sleep(p1, 1200);
  await p1.locator("#signup-email").pressSequentially("newuser@mail.edu", {
    delay: 45,
  });
  await sleep(p1, 500);
  await p1.locator("#signup-password").pressSequentially("User1234!", {
    delay: 40,
  });
  await sleep(p1, 400);
  await p1
    .locator("#signup-confirm-password")
    .pressSequentially("User1234!", { delay: 40 });
  await sleep(p1, 800);
  await p1.getByRole("button", { name: "Register" }).click();
  // The success alert is auto-dismissed; the form flips back to login.
  await p1.getByRole("heading", { name: "Welcome Back" }).waitFor({
    timeout: 10000,
  });
  await sleep(p1, 2500);
  await closeCtx(ctx1, "01-signup");

  // ---------- Scene 02: student login + device verification ----------
  log("scene 02: login student7 + 2FA");
  const ctx2 = await newCtx(browser, "02-login");
  const p2 = await ctx2.newPage();
  watchDialogs(p2);
  await login(p2, "student7@mail.edu", "Student1234!", {
    delay: 55,
    pause: 900,
  });
  await twoFactor(p2, /\/(student|dashboard)/);
  await p2.waitForURL(/\/student/, { timeout: 15000 });
  log("landed: " + p2.url());
  await sleep(p2, 6500); // week at a glance
  const authToken = await p2.evaluate(() => localStorage.getItem("auth_token"));
  await closeCtx(ctx2, "02-login");

  // ---------- Scene 03: student actions (token reused) ----------
  log("scene 03: student actions");
  const ctx3 = await newCtx(browser, "03-student");
  const p3 = await ctx3.newPage();
  watchDialogs(p3);
  await ctx3.addInitScript(
    (t) => localStorage.setItem("auth_token", t),
    authToken,
  );
  await p3.goto(BASE + "/student");
  await p3.waitForLoadState("networkidle");
  await sleep(p3, 4000);

  await p3.getByRole("heading", { name: "Workqueue" }).scrollIntoViewIfNeeded();
  await sleep(p3, 1800); // the queue: only shifts that fit his cap
  await clickBtn(p3, { text: "Pick", rowText: "2026-09-04" });
  await sleep(p3, 3000); // picked: calendar now 18/20

  await p3
    .getByRole("heading", { name: "My Calendar" })
    .scrollIntoViewIfNeeded();
  await sleep(p3, 1600);
  await clickBtn(p3, { text: "Request to miss", rowText: "2026-09-04" });
  const miss1 = p3.getByRole("dialog", { name: "Request to miss shift" });
  await miss1.waitFor({ timeout: 5000 });
  await sleep(p3, 1200);
  await miss1.locator("select").selectOption({ label: "Family event" });
  await sleep(p3, 700);
  await clickBtn(p3, { text: "Submit", within: '[role="dialog"]' });
  await sleep(p3, 3000); // pending request appears

  await p3
    .getByRole("heading", { name: "My Requests" })
    .scrollIntoViewIfNeeded();
  await sleep(p3, 1500);
  await clickBtn(p3, { text: "Request to miss", rowText: "2026-08-31" });
  const miss2 = p3.getByRole("dialog", { name: "Request to miss shift" });
  await miss2.waitFor({ timeout: 5000 });
  await sleep(p3, 900);
  await miss2.locator("select").selectOption({ label: "Exam preparation" });
  await sleep(p3, 600);
  await clickBtn(p3, { text: "Submit", within: '[role="dialog"]' });
  await sleep(p3, 3000); // second pending row

  await clickBtn(p3, {
    text: "Cancel",
    rowText: ["2026-08-31", "Exam preparation"],
  });
  await sleep(p3, 2500); // status flips to cancelled

  await p3
    .getByRole("heading", { name: "Preferred Days & Times" })
    .scrollIntoViewIfNeeded();
  await sleep(p3, 1400);
  await p3.locator("select").first().selectOption({ label: "Wed" });
  await p3.locator("input[name=startTime]").fill("10:00");
  await p3.locator("input[name=endTime]").fill("14:00");
  await sleep(p3, 600);
  await clickBtn(p3, { text: "Add" });
  await sleep(p3, 2200); // preference added

  await sidebar(p3, "Calendar view");
  await sleep(p3, 5000); // student week calendar
  await sidebar(p3, "Back to schedule");
  await sleep(p3, 2500);
  await closeCtx(ctx3, "03-student");

  // ---------- Scene 04: full-time + part-time worker views ----------
  log("scene 04: full-time worker");
  const ctx4 = await newCtx(browser, "04-workers");
  const p4 = await ctx4.newPage();
  watchDialogs(p4);
  await login(p4, "fulltime1@mail.edu", "Fulltime1234!", {
    delay: 30,
    pause: 600,
  });
  await twoFactor(p4, /\/(staff|dashboard)/);
  await p4.waitForURL(/\/staff/, { timeout: 15000 });
  log("landed: " + p4.url());
  await sleep(p4, 5000); // 8 / 60 hrs, full-time staff

  await p4.getByRole("heading", { name: "Workqueue" }).scrollIntoViewIfNeeded();
  await sleep(p4, 1600);
  await clickBtn(p4, { text: "Pick", rowText: "2026-09-02" });
  await sleep(p4, 3000); // 16 / 60 hrs

  await p4
    .getByRole("heading", { name: "My Requests" })
    .scrollIntoViewIfNeeded();
  await sleep(p4, 1800);

  await sidebar(p4, "Logout");
  await sleep(p4, 2200);
  log("scene 04: hourly worker");
  await login(p4, "hourly4@mail.edu", "Hourly1234!", { delay: 30, pause: 600 });
  await twoFactor(p4, /\/(staff|dashboard)/);
  await p4.waitForURL(/\/staff/, { timeout: 15000 });
  await sleep(p4, 5000); // 10 / 60 hrs, hourly staff
  await p4.getByRole("heading", { name: "Workqueue" }).scrollIntoViewIfNeeded();
  await sleep(p4, 1600);
  await clickBtn(p4, { text: "Pick", rowText: "2026-09-02" });
  await sleep(p4, 3000); // 18 / 60 hrs
  await p4
    .getByRole("heading", { name: "My Calendar" })
    .scrollIntoViewIfNeeded();
  await sleep(p4, 3500);
  await closeCtx(ctx4, "04-workers");

  // ---------- Scene 05: admin — org, users, sort + paging, RBAC ----------
  log("scene 05: admin");
  const ctx5 = await newCtx(browser, "05-admin");
  const p5 = await ctx5.newPage();
  watchDialogs(p5);
  await login(p5, "admin@mail.edu", "Password1234!", { delay: 28, pause: 550 });
  await twoFactor(p5, /\/(admin|dashboard)/);
  await p5.waitForURL(/\/admin/, { timeout: 15000 });
  log("landed: " + p5.url());
  await sleep(p5, 4500);

  await sidebar(p5, "Departments");
  await sleep(p5, 2600);
  await sidebar(p5, "Teams");
  await sleep(p5, 2200);
  await sidebar(p5, "Jobs");
  await sleep(p5, 2600);
  await sidebar(p5, "Access Control");
  await sleep(p5, 2200);

  const search = p5.getByRole("searchbox", {
    name: "Search email, UID, or role",
  });
  await search.click();
  await search.pressSequentially("staff@mail", { delay: 70 });
  await sleep(p5, 2400); // filtered to one row
  await search.fill("");
  await sleep(p5, 1200);

  // Create a user: email + UID + one-click temp password.
  await clickBtn(p5, { text: "Add User" });
  const adg = p5.getByRole("dialog", { name: "Add User" });
  await adg.waitFor({ timeout: 5000 });
  await sleep(p5, 1000);
  await adg
    .locator("input[name=email]")
    .pressSequentially("student4@mail.com", {
      delay: 35,
    });
  await sleep(p5, 400);
  await adg
    .locator("input[name=uid]")
    .pressSequentially("S90004", { delay: 45 });
  await sleep(p5, 500);
  await clickBtn(p5, { text: "Generate Password", within: '[role="dialog"]' });
  await sleep(p5, 900);
  const tempPw = await adg.locator("input[name=password]").inputValue();
  log("temp password captured: " + tempPw);
  await sleep(p5, 700);
  await clickBtn(p5, { text: "Save", within: '[role="dialog"]' });
  await sleep(p5, 2500); // row appears

  // Fill in all the information (and show the role ladder while we're here).
  await search.click();
  await search.pressSequentially("student4", { delay: 70 });
  await sleep(p5, 2200);
  const s4row = p5
    .locator("tr")
    .filter({ hasText: "student4@mail.com" })
    .first();
  await s4row.getByRole("button", { name: "Edit" }).click();
  const edg = p5.getByRole("dialog", { name: "Edit User" });
  await edg.waitFor({ timeout: 5000 });
  await sleep(p5, 2000); // role picker on camera
  const staffToggle = edg.locator(".role-picker label", { hasText: "staff" });
  await staffToggle.click();
  await sleep(p5, 1100);
  await staffToggle.click();
  await sleep(p5, 1100); // multi-role demo, student stays checked
  await edg.locator("input[name=firstName]").fill("Sasha");
  await edg.locator("input[name=lastName]").fill("Carter");
  await edg.locator("input[name=address]").fill("1200 Speedway");
  await edg.locator("input[name=city]").fill("Austin");
  await edg.locator("input[name=state]").fill("TX");
  await edg.locator("input[name=zip]").fill("78712");
  await edg.locator("input[name=country]").fill("US");
  await edg.locator("input[name=phone]").fill("(512) 555-0199");
  await edg
    .locator("select[name=communicationPreference]")
    .selectOption({ label: "Text" });
  await sleep(p5, 900);
  await clickBtn(p5, { text: "Save", within: '[role="dialog"]' });
  await sleep(p5, 2400); // profile saved

  await search.fill("");
  await sleep(p5, 1000);
  // Sorting + paging on the directory.
  await clickSort(p5, "access-control", "Email");
  await sleep(p5, 1400);
  await clickSort(p5, "access-control", "Email");
  await clickSort(p5, "access-control", "Roles");
  await sleep(p5, 1400);
  await p5
    .locator("#access-control .user-pager button", { hasText: "Next" })
    .click();
  await sleep(p5, 1600); // page 2 of N
  await p5
    .locator("#access-control .user-pager button", { hasText: "Prev" })
    .click();
  await sleep(p5, 2000);
  await closeCtx(ctx5, "05-admin");

  // ---------- Scene 06: finish the workflow (temp password onboarding) ----------
  log("scene 06: onboarding student4");
  const ctx6 = await newCtx(browser, "06-onboard");
  const p6 = await ctx6.newPage();
  watchDialogs(p6);
  await login(p6, "student4@mail.com", tempPw, { delay: 32, pause: 600 });
  await twoFactor(p6, /\/(change-password|dashboard|profile)/);
  await p6.waitForURL(/change-password/, { timeout: 20000 });
  log("forced change-password");
  await sleep(p6, 2500);
  await p6
    .locator("input[name=currentPassword]")
    .pressSequentially(tempPw, { delay: 25 });
  await sleep(p6, 400);
  await p6
    .locator("#new-password")
    .pressSequentially("Student4pass!", { delay: 30 });
  await sleep(p6, 300);
  await p6
    .locator("input[name=confirmPassword]")
    .pressSequentially("Student4pass!", { delay: 30 });
  await sleep(p6, 700);
  await p6.getByRole("button", { name: "Change Password" }).click();
  // The admin already filled in the profile, so the dashboard routes straight
  // to the role page — the forced change was the only remaining step.
  await p6.waitForURL(/\/(student|staff|manager|admin)/, { timeout: 30000 });
  log("landed on role dashboard: " + p6.url());
  await sleep(p6, 5000); // empty-but-ready student view
  await closeCtx(ctx6, "06-onboard");

  // ---------- Scene 07: department manager ----------
  log("scene 07: department manager");
  const ctx7 = await newCtx(browser, "07-manager");
  const p7 = await ctx7.newPage();
  watchDialogs(p7);
  await login(p7, "manager@mail.edu", "Manager1234!", {
    delay: 30,
    pause: 600,
  });
  await twoFactor(p7, /\/(manager|dashboard)/);
  await p7.waitForURL(/\/manager/, { timeout: 15000 });
  log("landed: " + p7.url());
  if (!p7.url().includes("/manager")) await p7.goto(BASE + "/manager");
  await p7.waitForLoadState("networkidle");
  await sleep(p7, 5500); // org chart: scope + You badges

  await sidebar(p7, "Workers");
  await sleep(p7, 3500); // worker types: Full-time staff / Student
  await sidebar(p7, "Job Requirements");
  await sleep(p7, 3000);

  await sidebar(p7, "Workqueue");
  await sleep(p7, 1500);
  await p7.getByRole("button", { name: "Add Shift" }).click();
  const wdlg = p7.getByRole("dialog", { name: "Add Workqueue Shift" });
  await wdlg.waitFor({ timeout: 5000 });
  await sleep(p7, 1000);
  await wdlg.locator("select[name=teamId]").selectOption({ label: "dining" });
  await wdlg.locator("input[name=date]").fill("2026-09-08");
  await wdlg.locator("input[name=startTime]").fill("09:00");
  await wdlg.locator("input[name=endTime]").fill("13:00");
  await sleep(p7, 600);
  await clickBtn(p7, { text: "Add", within: '[role="dialog"]' });
  await sleep(p7, 2500); // new row appears

  await sidebar(p7, "Requests");
  await sleep(p7, 2000);
  await clickBtn(p7, {
    text: "Approve",
    rowText: ["student7@mail.edu", "2026-09-04"],
  });
  await sleep(p7, 3000); // approved — shift returns to the queue

  await sidebar(p7, "Calendar view");
  await sleep(p7, 5000); // coverage calendar, color coded
  await clickBtn(p7, { text: "Open" });
  await sleep(p7, 2200); // open lane hidden
  await clickBtn(p7, { text: "Open" });
  await sleep(p7, 1800); // back on
  await p7.getByRole("button", { name: "Next week" }).click();
  await sleep(p7, 2600); // next week incl. the added Monday slot
  await p7.getByRole("button", { name: "Today" }).click();
  await sleep(p7, 2200);
  await sidebar(p7, "Back to schedule");
  await sleep(p7, 2200);
  await closeCtx(ctx7, "07-manager");

  // ---------- Scene 08: team-scoped scheduler manager ----------
  log("scene 08: scheduler manager");
  const ctx8 = await newCtx(browser, "08-scheduler");
  const p8 = await ctx8.newPage();
  watchDialogs(p8);
  await login(p8, "scheduler@mail.edu", "Scheduler1234!", {
    delay: 30,
    pause: 600,
  });
  await twoFactor(p8, /\/(manager|dashboard)/);
  await p8.waitForURL(/\/manager/, { timeout: 15000 });
  log("landed: " + p8.url());
  if (!p8.url().includes("/manager")) await p8.goto(BASE + "/manager");
  await p8.waitForLoadState("networkidle");
  await sleep(p8, 5000); // team scope: dining only

  await sidebar(p8, "Requests");
  await sleep(p8, 1800);
  await clickBtn(p8, {
    text: "Deny",
    rowText: ["student7@mail.edu", "2026-09-02", "Class conflict"],
  });
  await sleep(p8, 2800); // denied

  await sidebar(p8, "Calendar view");
  await sleep(p8, 5000); // dining week, color coded

  // Assign: open Thursday slot -> Priya Patel, first coverage block (09-11);
  // the rest of the slot stays open.
  const chipTime = await clickOpenChip(p8, "Thu 3");
  log("assigned-slot chip: " + chipTime);
  const adlg = p8.getByRole("dialog", { name: "Assign Shift" });
  await adlg.waitFor({ timeout: 5000 });
  await sleep(p8, 1500); // blocks + recent activity
  const aopt = adlg
    .locator("select option")
    .filter({ hasText: "Priya Patel" })
    .first();
  await adlg.locator("select").selectOption(await aopt.getAttribute("value"));
  await sleep(p8, 900);
  await clickBtn(p8, { text: "Assign", within: '[role="dialog"]' });
  await sleep(p8, 3000); // assigned, calendar re-renders
  await p8.screenshot({ path: join(OUT, "shots", "calendar.png") });
  await sleep(p8, 1800);

  // Unassign: open the assigned chip, pick "Unassigned".
  await clickWorkerChip(p8, "Priya Patel");
  const rdlg = p8.getByRole("dialog", { name: "Reassign Shift" });
  await rdlg.waitFor({ timeout: 5000 });
  await sleep(p8, 1300);
  await rdlg.locator("select").selectOption("");
  await sleep(p8, 700);
  await clickBtn(p8, { text: "Reassign", within: '[role="dialog"]' });
  await sleep(p8, 3000); // back to the Open row

  // Reassign to someone else: same slot, now to Tom Tran.
  await clickOpenChip(p8, "Thu 3");
  const adlg2 = p8.getByRole("dialog", { name: "Assign Shift" });
  await adlg2.waitFor({ timeout: 5000 });
  await sleep(p8, 1200);
  const topt = adlg2
    .locator("select option")
    .filter({ hasText: "Tom Tran" })
    .first();
  await adlg2.locator("select").selectOption(await topt.getAttribute("value"));
  await sleep(p8, 800);
  await clickBtn(p8, { text: "Assign", within: '[role="dialog"]' });
  await sleep(p8, 3000); // Tom owns the block
  await sidebar(p8, "Back to schedule");
  await sleep(p8, 2200);
  await closeCtx(ctx8, "08-scheduler");

  // ---------- Scene 09: department manager audit report ----------
  log("scene 09: audit report");
  const ctx9 = await newCtx(browser, "09-audit");
  const p9 = await ctx9.newPage();
  watchDialogs(p9);
  await login(p9, "manager@mail.edu", "Manager1234!", {
    delay: 30,
    pause: 600,
  });
  await twoFactor(p9, /\/(manager|dashboard)/);
  await p9.waitForURL(/\/manager/, { timeout: 15000 });
  if (!p9.url().includes("/manager")) await p9.goto(BASE + "/manager");
  await p9.waitForLoadState("networkidle");
  await sleep(p9, 2500);
  await sidebar(p9, "Audit Report");
  await p9.waitForURL(/audit/, { timeout: 10000 });
  await p9.waitForLoadState("networkidle");
  await sleep(p9, 5000); // scoped trail
  await p9.locator("select").first().selectOption({ label: "Requests" });
  await sleep(p9, 2600);
  await p9.locator("select").first().selectOption({ label: "Shifts" });
  await sleep(p9, 2600);
  const nextBtn = p9.locator(".user-pager button", { hasText: "Next" });
  if (await nextBtn.isVisible().catch(() => false)) {
    await nextBtn.click();
    await sleep(p9, 1600);
  }
  await sleep(p9, 1800);
  await sidebar(p9, "Back to Manager");
  await sleep(p9, 2200);
  await closeCtx(ctx9, "09-audit");

  // ---------- Title-card stills (logged out) ----------
  log("still: login page");
  const ctxA = await newCtx(browser, "10-still");
  const pA = await ctxA.newPage();
  watchDialogs(pA);
  await pA.goto(BASE + "/");
  await pA.waitForLoadState("networkidle");
  await sleep(pA, 1500);
  await pA.screenshot({ path: join(OUT, "shots", "login.png") });
  await closeCtx(ctxA, "10-still");

  log("done");
} finally {
  await browser.close();
}
