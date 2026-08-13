/**
 * Regenerates the opt-in form screenshot embedded on the public /sms-opt-in
 * page (internal/api/optin_screenshot.go).
 *
 * A carrier reviewer required "a hosted screenshot link showing your exact
 * phone number collection form and its compliance disclosures". Rather than
 * screenshot the in-app settings screen (which carries app chrome), this
 * renders a clean, standalone representation of the SMS opt-in form — a phone
 * field and a dedicated SMS-consent checkbox, unchecked, with the full
 * disclosure — screenshots it, compresses it, and writes the Go source with
 * the data: URI embedded.
 *
 * Prereqs: Python with Pillow for the compression step. Usage:
 *
 *   node web/scripts/optin-screenshot.mjs
 */
import { createRequire } from "node:module";
import { execFileSync } from "node:child_process";
import { writeFileSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const require = createRequire(import.meta.url);
const { chromium } = require("playwright");

// The standalone opt-in form. This is the reference the reviewer sees; keep
// the consent language in sync with the disclosures on /privacy and /terms.
const FORM_HTML = `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
  body { margin:0; background:#f4f6f9; font:16px/1.55 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif; color:#16191d; }
  .card { max-width:520px; margin:24px auto; background:#fff; border:1px solid #dfe3e8; border-radius:10px; padding:22px 22px 26px; box-shadow:0 1px 3px rgba(16,24,40,.06); }
  .brand { font-weight:700; font-size:1.05rem; margin:0 0 2px; }
  .sub { color:#6b7580; font-size:.85rem; margin:0 0 18px; }
  label.field { display:block; font-weight:600; font-size:.9rem; margin:0 0 6px; }
  input[type=tel] { width:100%; box-sizing:border-box; padding:.6rem .7rem; font-size:1rem; border:1px solid #c4ccd6; border-radius:6px; }
  label.consent { display:flex; gap:.6rem; align-items:flex-start; margin:16px 0; font-size:.82rem; color:#333; }
  label.consent input { margin:.15rem 0 0; width:18px; height:18px; flex:0 0 auto; }
  button { width:100%; padding:.7rem 1rem; font-size:1rem; font-weight:600; border:0; border-radius:6px; background:#9db8f0; color:#fff; cursor:not-allowed; }
  a { color:#2f6feb; }
</style></head><body>
<div class="card">
  <p class="brand">donezo Reminders &mdash; SMS sign-up</p>
  <p class="sub">Grewell Tech</p>
  <label class="field" for="p">Mobile number</label>
  <input type="tel" id="p" value="+1 555 010 4477">
  <label class="consent" for="c">
    <input type="checkbox" id="c">
    <span>I agree to receive recurring automated text messages from donezo Reminders (Grewell Tech) at the number provided: a one-time verification code and the reminder texts I schedule. Consent is not a condition of purchase or of using donezo. Message frequency varies. Message and data rates may apply. Reply STOP to cancel, HELP for help. We do not share, sell, or provide my mobile phone number or messaging consent data to third parties or affiliates for marketing or promotional purposes. See our <a href="#">Privacy Policy</a> and <a href="#">Terms</a>.</span>
  </label>
  <button disabled>Sign up for SMS reminders</button>
</div>
</body></html>`;

const dir = mkdtempSync(join(tmpdir(), "optin-"));
const rawPng = join(dir, "form.png");

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 560, height: 640 }, deviceScaleFactor: 2 });
await page.setContent(FORM_HTML, { waitUntil: "networkidle" });
await page.waitForTimeout(150);
const box = await page.locator(".card").boundingBox();
await page.screenshot({
  path: rawPng,
  clip: { x: box.x - 16, y: box.y - 16, width: box.width + 32, height: box.height + 32 },
});
await browser.close();

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

// optInScreenshotDataURI is a PNG of the SMS opt-in form — a mobile number
// field and a dedicated SMS-consent checkbox that is unchecked by default,
// with the full consent disclosure — embedded as a data: URI so the public
// /sms-opt-in page is self-contained.
//
// A carrier reviewer required "a hosted screenshot link showing your exact
// phone number collection form and its compliance disclosures". This is
// captured from a standalone rendering of the opt-in form (see
// web/scripts/optin-screenshot.mjs); regenerate it if the form changes.
const optInScreenshotDataURI = \``;

writeFileSync("internal/api/optin_screenshot.go", header + dataURI + "`\n");
console.log(`wrote internal/api/optin_screenshot.go (${dataURI.length} chars)`);
