/**
 * Regenerates the opt-in form screenshot embedded on the public /sms-opt-in
 * page (internal/api/optin_screenshot.go).
 *
 * A carrier reviewer required "a hosted screenshot link showing your exact
 * phone number collection form and its compliance disclosures". This captures
 * the real Settings → Reminders SMS form — the number field with the consent
 * disclosure and policy links beneath it — compresses it, and writes the Go
 * source with the data: URI embedded.
 *
 * Prereqs: a running donezod with SMS configured and an owner account, plus
 * Python with Pillow for the compression step. Usage:
 *
 *   node web/scripts/optin-screenshot.mjs <baseURL> <username> <password>
 *
 * e.g. node web/scripts/optin-screenshot.mjs http://localhost:8821 ben pass
 */
import { createRequire } from "node:module";
import { execFileSync } from "node:child_process";
import { writeFileSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const require = createRequire(import.meta.url);
const { chromium } = require("playwright");

const [base = "http://localhost:8821", user = "ben", pass = "adminpass123"] = process.argv.slice(2);
const dir = mkdtempSync(join(tmpdir(), "optin-"));
const rawPng = join(dir, "form.png");

const browser = await chromium.launch();
const ctx = await browser.newContext({
  viewport: { width: 720, height: 1000 },
  deviceScaleFactor: 2,
  colorScheme: "light",
});
const page = await ctx.newPage();
await page.goto(`${base}/#/focus`, { waitUntil: "networkidle" });
await page.waitForTimeout(500);
await page.getByLabel(/username/i).fill(user);
await page.getByLabel(/password/i).fill(pass);
await page.getByRole("button", { name: /sign in|log in/i }).click();
await page.waitForTimeout(1500);
const welcome = page.getByRole("button", { name: "Just start" });
if (await welcome.count()) {
  await welcome.click();
  await page.waitForTimeout(300);
}
await page.goto(`${base}/#/settings/reminders`, { waitUntil: "networkidle" });
await page.waitForTimeout(900);
await page.getByLabel("Delivery channel").selectOption("sms");
await page.waitForTimeout(400);
await page.getByLabel("Address or number").fill("+1 555 010 4477");
await page.waitForTimeout(300);
if ((await page.getByText(/By entering your number you agree/).count()) === 0) {
  throw new Error("consent disclosure not visible on the form — aborting");
}
const box = await page.locator("main").boundingBox();
await page.screenshot({
  path: rawPng,
  clip: { x: box.x, y: box.y, width: Math.min(box.width, 720), height: Math.min(box.height, 620) },
});
await browser.close();

// Compress (downscale + palette) and emit the Go source. Kept in Python
// because Pillow is already a project dev dependency for image work.
const py = `
from PIL import Image
import base64, io
im = Image.open(${JSON.stringify(rawPng)}).convert("RGB")
w,h = im.size
if w > 1000:
    im = im.resize((1000, int(h*1000/w)), Image.LANCZOS)
buf = io.BytesIO()
im.convert("P", palette=Image.ADAPTIVE, colors=64).save(buf, format="PNG", optimize=True)
print("data:image/png;base64," + base64.b64encode(buf.getvalue()).decode())
`;
const dataURI = execFileSync("python3", ["-c", py]).toString().trim();

const header = `package api

// optInScreenshotDataURI is a PNG of the app's Settings → Reminders phone
// number collection form — the "Text message" channel selected, the number
// field, and the consent disclosure (frequency, rates, STOP/HELP, plus the
// Privacy Policy and Terms links) shown directly beneath it — embedded as a
// data: URI so the public /sms-opt-in evidence page is self-contained.
//
// A carrier reviewer required "a hosted screenshot link showing your exact
// phone number collection form and its compliance disclosures", so this is
// captured from the running app and must match what a user actually sees.
// Regenerate with web/scripts/optin-screenshot.mjs when the opt-in form changes.
const optInScreenshotDataURI = \``;

writeFileSync("internal/api/optin_screenshot.go", header + dataURI + "`\n");
console.log(`wrote internal/api/optin_screenshot.go (${dataURI.length} chars)`);
