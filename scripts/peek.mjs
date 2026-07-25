/**
 * Quick visual check of the running dev server with Playwright.
 *
 * Usage:
 *   node scripts/peek.mjs [hashPath] [outFile] [options]
 *
 *   hashPath   view hash, e.g. "#/timeline" or "#/focus" (default "#/timeline")
 *   outFile    screenshot path (default .peek/peek.png)
 *
 * Options:
 *   --width N     viewport width  (default 1600)
 *   --height N    viewport height (default 950)
 *   --full        full-page screenshot
 *   --theme ID    set donezo theme before load (console | slate)
 *   --wait MS     extra settle time after load (default 400)
 */
import { chromium } from "playwright";
import { mkdirSync } from "node:fs";
import { dirname } from "node:path";

const positional = [];
const opts = { width: 1600, height: 950, full: false, theme: null, wait: 400 };
const argv = process.argv.slice(2);
for (let i = 0; i < argv.length; i++) {
  const a = argv[i];
  if (a === "--width") opts.width = Number(argv[++i]);
  else if (a === "--height") opts.height = Number(argv[++i]);
  else if (a === "--full") opts.full = true;
  else if (a === "--theme") opts.theme = argv[++i];
  else if (a === "--wait") opts.wait = Number(argv[++i]);
  else positional.push(a);
}

const hashPath = positional[0] ?? "#/timeline";
const outFile = positional[1] ?? ".peek/peek.png";
const url = `http://localhost:5173/${hashPath.startsWith("#") ? hashPath : `#${hashPath}`}`;

mkdirSync(dirname(outFile), { recursive: true });

const browser = await chromium.launch();
const context = await browser.newContext({
  viewport: { width: opts.width, height: opts.height },
  colorScheme: "dark",
});
if (opts.theme) {
  await context.addInitScript(
    (theme) => window.localStorage.setItem("donezo.theme", theme),
    opts.theme
  );
}
const page = await context.newPage();
const errors = [];
page.on("console", (msg) => {
  if (msg.type() === "error") errors.push(msg.text());
});
page.on("pageerror", (err) => errors.push(String(err)));

await page.goto(url, { waitUntil: "networkidle" });
await page.waitForTimeout(opts.wait);
await page.screenshot({ path: outFile, fullPage: opts.full });
await browser.close();

console.log(`saved ${outFile} (${opts.width}x${opts.height}) for ${url}`);
if (errors.length) {
  console.log("console/page errors:");
  for (const e of errors) console.log(`  ${e}`);
  process.exitCode = 2;
}
