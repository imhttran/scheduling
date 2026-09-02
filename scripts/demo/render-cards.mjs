#!/usr/bin/env node
// Renders the intro/outro title cards (1440x900 PNGs) for the demo video,
// using the real HTT brand tile from brand/tiles/htt-tiers-duet.svg.
//
// Usage: node scripts/demo/render-cards.mjs

import { chromium } from "playwright";
import { mkdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const HERE = dirname(fileURLToPath(import.meta.url));
const OUT = join(HERE, "out");
const brandTile = (file) =>
  readFileSync(join(HERE, "..", "..", "brand", "tiles", file), "utf8")
    .replace(/<\?xml[^>]*\?>/, "")
    .trim();
const TILE = brandTile("htt-tiers-duet.svg"); // white tile, for the title cards
const TILE_NAVY = brandTile("htt-tiers-navy.svg"); // navy tile watermark
mkdirSync(join(OUT, "shots"), { recursive: true });

const card = (sub, footer) => `<!doctype html>
<html><head><style>
  body { margin:0; width:1440px; height:900px; background:#001020;
         display:flex; align-items:center; justify-content:center;
         font-family:-apple-system,'Helvetica Neue',Helvetica,Arial,sans-serif; }
  .card { display:flex; flex-direction:column; align-items:center; text-align:center; }
  img   { width:150px; height:150px; margin-bottom:46px; }
  h1    { color:#FAFAF7; font-size:96px; font-weight:700; letter-spacing:-1px; margin:0 0 36px; }
  .bar  { width:260px; height:10px; background:#BF5700; border-radius:5px; margin-bottom:38px; }
  .sub  { color:#D8E0E8; font-size:38px; }
  .foot { position:fixed; bottom:46px; left:0; right:0; text-align:center; color:#93A5B4; font-size:26px; }
</style></head>
<body>
  <div class="card">
    <div style="width:150px;height:150px;margin-bottom:46px">${TILE}</div>
    <h1>HTT Scheduling</h1>
    <div class="bar"></div>
    <div class="sub">${sub}</div>
  </div>
  <div class="foot">${footer}</div>
</body></html>`;

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });

await page.setContent(
  card(
    "Student work scheduling for campus teams",
    "Next.js   ·   Go API   ·   PostgreSQL",
  ),
);
await page.screenshot({ path: join(OUT, "shots", "card-title.png") });

await page.setContent(
  card(
    "Roles · Shift picking · Time off · Audits",
    "Built with Next.js, Go, and PostgreSQL",
  ),
);
await page.screenshot({ path: join(OUT, "shots", "card-outro.png") });

// Small brand badge for the scene watermark (navy tile, transparent corners).
await page.setViewportSize({ width: 144, height: 144 });
await page.setContent(
  `<body style="margin:0;background:transparent"><div style="width:144px;height:144px">${TILE_NAVY}</div></body>`,
);
await page.screenshot({
  path: join(OUT, "shots", "logo-badge.png"),
  omitBackground: true,
});

await browser.close();
console.log("cards rendered");
