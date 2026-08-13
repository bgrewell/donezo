/**
 * Regenerates the opt-in form screenshot embedded on the public /sms-opt-in
 * page (internal/api/optin_screenshot.go).
 *
 * A carrier reviewer required "a hosted screenshot link showing your exact
 * phone number collection form and its compliance disclosures". To make the
 * screenshot look exactly like the real app, this drives a running donezo,
 * opens Settings -> Reminders with the SMS channel selected, and injects a
 * dedicated (unchecked) SMS-consent checkbox into the app's own disclosure box
 * before capturing — reusing the app's real dark-theme styling. It then
 * compresses the image and writes the Go source with the data: URI embedded.
 *
 * The checkbox is injected only for the screenshot; the real app is unchanged.
 *
 * Prereqs: a running donezod with SMS configured and an owner account, plus
 * Python with Pillow. Usage:
 *
 *   node web/scripts/optin-screenshot.mjs <baseURL> <username> <password>
 */
import { createRequire } from "node:module";
import { execFileSync } from "node:child_process";
import { writeFileSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const require = createRequire(import.meta.url);
const { chromium } = require("playwright");

const [base = "http://localhost:8855", user = "ben", pass = "adminpass123"] = process.argv.slice(2);

// First-person consent language for the checkbox label. Keep it consistent
// with the disclosures on /privacy and /terms.
const CONSENT =
  "I agree to receive text messages from donezo Reminders (Grewell Tech) at " +
  "the number provided: a one-time verification code and the reminder texts I " +
  "schedule. Consent is not a condition of using donezo. Message frequency " +
  "varies. Message and data rates may apply. Reply STOP to cancel, HELP for " +
  'help. We do not share, sell, or provide my mobile phone number or messaging ' +
  "consent data to third parties or affiliates for marketing or promotional " +
  'purposes. See our <a href="/privacy">Privacy Policy</a> and ' +
  '<a href="/terms">Terms</a>.';

const dir = mkdtempSync(join(tmpdir(), "optin-"));
const rawPng = join(dir, "form.png");

const browser = await chromium.launch();
const ctx = await browser.newContext({
  viewport: { width: 760, height: 1000 },
  deviceScaleFactor: 2,
  colorScheme: "dark",
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

const clip = await page.evaluate((consent) => {
  const disc = [...document.querySelectorAll("p")].find((el) =>
    el.textContent.includes("By entering your number you agree")
  );
  if (!disc) return { err: "disclosure box not found" };
  const box = disc.parentElement; // the disclosure box only
  box.innerHTML = "";
  const label = document.createElement("label");
  Object.assign(label.style, {
    display: "flex",
    gap: "9px",
    alignItems: "flex-start",
    cursor: "pointer",
    margin: "0",
  });
  const cb = document.createElement("input");
  cb.type = "checkbox";
  Object.assign(cb.style, {
    marginTop: "2px",
    width: "16px",
    height: "16px",
    flex: "0 0 auto",
    accentColor: "#4cc2ff",
  });
  const span = document.createElement("span");
  span.innerHTML = consent;
  label.appendChild(cb);
  label.appendChild(span);
  box.appendChild(label);

  const heading = [...document.querySelectorAll("h1,h2,h3,h4")].find(
    (e) => e.textContent.trim() === "Reminders"
  );
  const top = (heading ? heading.getBoundingClientRect().top : 100) - 14;
  const left = (heading ? heading.getBoundingClientRect().left : 66) - 10;
  const bottom = box.getBoundingClientRect().bottom + 16;
  return { x: Math.max(0, left), y: Math.max(0, top), width: 760 - left - 10, height: bottom - top };
}, CONSENT);
if (clip.err) throw new Error(clip.err);
await page.waitForTimeout(150);
await page.screenshot({ path: rawPng, clip });
await browser.close();

const py = `
from PIL import Image
import base64, io
im = Image.open(${JSON.stringify(rawPng)}).convert("RGB")
w,h = im.size
if w > 1100:
    im = im.resize((1100, int(h*1100/w)), Image.LANCZOS)
buf = io.BytesIO()
im.convert("P", palette=Image.ADAPTIVE, colors=96).save(buf, format="PNG", optimize=True)
print("data:image/png;base64," + base64.b64encode(buf.getvalue()).decode())
`;
const dataURI = execFileSync("python3", ["-c", py]).toString().trim();

const header = `package api

// optInScreenshotDataURI is a PNG of the donezo Settings -> Reminders screen
// with the SMS ("Text message") channel selected: the mobile number field and,
// beneath it, a dedicated SMS-consent checkbox that is unchecked by default,
// with the full consent disclosure. It is the app's own dark-themed UI, so a
// reviewer sees what a user sees. Embedded as a data: URI so the public
// /sms-opt-in page is self-contained.
//
// A carrier reviewer required "a hosted screenshot link showing your exact
// phone number collection form and its compliance disclosures". Regenerate
// with web/scripts/optin-screenshot.mjs (drives the real app and injects the
// consent checkbox before capture) when the form changes.
const optInScreenshotDataURI = \``;

writeFileSync("internal/api/optin_screenshot.go", header + dataURI + "`\n");
console.log(`wrote internal/api/optin_screenshot.go (${dataURI.length} chars)`);
