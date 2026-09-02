#!/usr/bin/env node
// Records the demo scenes for the HTT Scheduling promo video.
// Produces out/video/NN-<name>.webm (1440x900, one file per scene)
// plus out/shots/login.png and out/shots/calendar.png for the title cards.
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

async function login(page, email, password, { delay = 40, pause = 500 } = {}) {
  await page.goto(BASE + "/");
  await page.waitForLoadState("networkidle");
  await sleep(page, 2500);
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

async function twoFactor(page, { codePause = 500 } = {}) {
  await page
    .getByRole("textbox", { name: "Verification Code" })
    .waitFor({ timeout: 10000 });
  for (let i = 0; i < 4; i++) {
    await page.locator("#code-" + i).fill("1234"[i]);
    await sleep(page, codePause);
  }
  await page.waitForURL(/\/(student|manager|admin|dashboard|scheduler)/, {
    timeout: 20000,
  });
  await page.waitForLoadState("networkidle");
}

const browser = await chromium.launch({ args: ["--mute-audio"] });
mkdirSync(join(OUT, "video"), { recursive: true });
mkdirSync(join(OUT, "shots"), { recursive: true });

try {
  // ---------- Scene 01: login + device verification (student) ----------
  log("scene 01: login + 2FA");
  const ctx1 = await newCtx(browser, "01-login");
  const p1 = await ctx1.newPage();
  await login(p1, "student7@mail.edu", "Student1234!", {
    delay: 55,
    pause: 900,
  });
  await twoFactor(p1, { codePause: 700 });
  log("landed: " + p1.url());
  await sleep(p1, 6000);
  const authToken = await p1.evaluate(() => localStorage.getItem("auth_token"));
  await closeCtx(ctx1, "01-login");

  // ---------- Scene 02: student dashboard ----------
  log("scene 02: student");
  const ctx2 = await newCtx(browser, "02-student");
  const p2 = await ctx2.newPage();
  await ctx2.addInitScript(
    (t) => localStorage.setItem("auth_token", t),
    authToken,
  );
  await p2.goto(BASE + "/student");
  await p2.waitForLoadState("networkidle");
  await sleep(p2, 6000); // week at a glance

  await p2.getByRole("heading", { name: "Workqueue" }).scrollIntoViewIfNeeded();
  await sleep(p2, 1400);
  await p2
    .locator("tr")
    .filter({ hasText: "2026-09-02" })
    .first()
    .scrollIntoViewIfNeeded();
  await sleep(p2, 1000);
  await clickBtn(p2, { text: "Pick", rowText: "2026-09-02" });
  await sleep(p2, 3000); // shift picked

  await p2
    .getByRole("heading", { name: "Preferred Days & Times" })
    .scrollIntoViewIfNeeded();
  await sleep(p2, 1400);
  await p2.locator("select").first().selectOption({ label: "Wed" });
  await p2.locator("input[name=startTime]").fill("10:00");
  await p2.locator("input[name=endTime]").fill("14:00");
  await sleep(p2, 600);
  await clickBtn(p2, { text: "Add" });
  await sleep(p2, 2000); // preference added

  const missBtn = p2.getByRole("button", { name: "Request to miss" }).first();
  await missBtn.scrollIntoViewIfNeeded();
  await sleep(p2, 1000);
  await missBtn.click();
  const dlg2 = p2.getByRole("dialog", { name: "Request to miss shift" });
  await dlg2.waitFor({ timeout: 5000 });
  await sleep(p2, 1200);
  await dlg2.locator("select").selectOption({ label: "Family event" });
  await sleep(p2, 700);
  await clickBtn(p2, { text: "Submit", within: '[role="dialog"]' });
  await sleep(p2, 4000); // pending request appears
  await closeCtx(ctx2, "02-student");

  // ---------- Scene 03: manager (add shift + approve request) ----------
  log("scene 03: manager");
  const ctx3 = await newCtx(browser, "03-manager");
  const p3 = await ctx3.newPage();
  await login(p3, "manager@mail.edu", "Manager1234!", {
    delay: 30,
    pause: 600,
  });
  await twoFactor(p3, { codePause: 320 });
  log("landed: " + p3.url());
  if (!p3.url().includes("/manager")) await p3.goto(BASE + "/manager");
  await p3.waitForLoadState("networkidle");
  await sleep(p3, 2500);

  await p3.getByRole("heading", { name: "Workqueue" }).scrollIntoViewIfNeeded();
  await sleep(p3, 1200);
  await p3.getByRole("button", { name: "Add Shift" }).click();
  const dlg3 = p3.getByRole("dialog", { name: "Add Workqueue Shift" });
  await dlg3.waitFor({ timeout: 5000 });
  await sleep(p3, 1200);
  await dlg3
    .locator("select[name=departmentId]")
    .selectOption({ label: "dining" });
  await dlg3.locator("input[name=date]").fill("2026-08-31");
  await dlg3.locator("input[name=startTime]").fill("09:00");
  await dlg3.locator("input[name=endTime]").fill("13:00");
  await sleep(p3, 600);
  await clickBtn(p3, { text: "Add", within: '[role="dialog"]' });
  await sleep(p3, 2000); // new row appears

  await p3
    .getByRole("heading", { name: "Pending Requests" })
    .scrollIntoViewIfNeeded();
  await sleep(p3, 1600);
  await p3
    .locator("tr")
    .filter({ hasText: "student7@mail.edu" })
    .filter({ hasText: "Family event" })
    .first()
    .scrollIntoViewIfNeeded();
  await sleep(p3, 900);
  // George's fresh request targets his 09-02 pick; the seed request for the
  // same worker is for 09-04, so pin both the worker and the date.
  await clickBtn(p3, {
    text: "Approve",
    rowText: ["student7@mail.edu", "2026-09-02"],
  });
  await sleep(p3, 2500); // approved
  await closeCtx(ctx3, "03-manager");

  // ---------- Scene 04: scheduler (coverage calendar assign) ----------
  log("scene 04: scheduler");
  const ctx4 = await newCtx(browser, "04-scheduler");
  const p4 = await ctx4.newPage();
  await login(p4, "scheduler@mail.edu", "Scheduler1234!", {
    delay: 30,
    pause: 600,
  });
  await twoFactor(p4, { codePause: 320 });
  log("landed: " + p4.url());
  if (!p4.url().includes("/scheduler/calendar")) {
    await p4.goto(BASE + "/scheduler/calendar");
  }
  await p4.waitForLoadState("networkidle");
  await sleep(p4, 4000); // the whole week, color coded

  await p4.evaluate(() => {
    const btn = Array.from(
      document.querySelectorAll("button.cal-shift-open"),
    ).find((b) => b.textContent.includes("09:00 AM–01:00 PM"));
    if (btn) {
      btn.scrollIntoView({ block: "center", inline: "center" });
      const r = btn.getBoundingClientRect();
      btn.dispatchEvent(
        new PointerEvent("pointerdown", {
          bubbles: true,
          clientX: r.x + r.width / 2,
          clientY: r.y + r.height / 2,
        }),
      );
      btn.click();
    }
  });
  const adlg = p4.getByRole("dialog", { name: "Assign Shift" });
  await adlg.waitFor({ timeout: 5000 });
  await sleep(p4, 1400); // modal with recent audit activity
  const aopt = adlg
    .locator("select option")
    .filter({ hasText: "Priya Patel" })
    .first();
  await adlg.locator("select").selectOption(await aopt.getAttribute("value"));
  await sleep(p4, 900);
  await clickBtn(p4, { text: "Assign", within: '[role="dialog"]' });
  await sleep(p4, 2500); // assigned, calendar re-renders
  await p4.screenshot({ path: join(OUT, "shots", "calendar.png") });
  await sleep(p4, 2000);
  await closeCtx(ctx4, "04-scheduler");

  // ---------- Scene 05: admin ----------
  log("scene 05: admin");
  const ctx5 = await newCtx(browser, "05-admin");
  const p5 = await ctx5.newPage();
  await login(p5, "admin@mail.edu", "Password1234!", { delay: 28, pause: 550 });
  await twoFactor(p5, { codePause: 300 });
  log("landed: " + p5.url());
  if (!p5.url().includes("/admin")) await p5.goto(BASE + "/admin");
  await p5.waitForLoadState("networkidle");
  await sleep(p5, 4000);

  for (const h of ["Departments", "Jobs"]) {
    await p5
      .getByRole("heading", { name: h, exact: true })
      .first()
      .scrollIntoViewIfNeeded();
    await sleep(p5, 2000);
  }
  await p5
    .getByRole("heading", { name: "Access Control" })
    .scrollIntoViewIfNeeded();
  await sleep(p5, 2000);
  const search = p5.getByRole("searchbox", {
    name: "Search email, UID, or role",
  });
  await search.click();
  await search.pressSequentially("staff@mail", { delay: 70 });
  await sleep(p5, 2400); // filtered to the staff account

  // RBAC on camera: open the editor and walk the role ladder up one notch.
  // (Scope to the Access Control row - the Locations table above also has
  // Edit buttons, and DOM order would otherwise win.)
  const staffRow = p5
    .locator("tr")
    .filter({ hasText: "staff@mail.edu" })
    .first();
  await staffRow.getByRole("button", { name: "Edit" }).click();
  const udg = p5.getByRole("dialog", { name: "Edit User" });
  await udg.waitFor({ timeout: 5000 });
  await sleep(p5, 2000); // role picker: student..admin
  await udg.locator("select").first().selectOption("manager");
  await sleep(p5, 1200);
  await clickBtn(p5, { text: "Save", within: '[role="dialog"]' });
  await sleep(p5, 2200); // row now reads manager
  await search.fill("");
  await sleep(p5, 900);

  await p5.getByRole("link", { name: "Audit Report" }).first().click();
  await p5.waitForURL(/audit/, { timeout: 10000 });
  await p5.waitForLoadState("networkidle");
  await sleep(p5, 6000); // audit trail is the closing beat
  await closeCtx(ctx5, "05-admin");

  // ---------- Title-card stills (logged out) ----------
  log("still: login page");
  const ctx6 = await newCtx(browser, "06-still");
  const p6 = await ctx6.newPage();
  await p6.goto(BASE + "/");
  await p6.waitForLoadState("networkidle");
  await sleep(p6, 1500);
  await p6.screenshot({ path: join(OUT, "shots", "login.png") });
  await closeCtx(ctx6, "06-still");

  log("done");
} finally {
  await browser.close();
}
