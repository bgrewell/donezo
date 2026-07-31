/**
 * Layout regression check for project-detail task rows.
 *
 * Guards the bug where a long task title ran through the due-date chip and
 * off the right edge of the screen. The row itself was always correct — the
 * cause was upstream, in @grewelltech/console's Checkbox: its label wrapper
 * was a flex item without `min-w-0`, so it could not shrink below its text
 * width and the `truncate` the row asks for never took effect.
 *
 * The check supplies its own fixture: it overwrites a real task title with a
 * long string before measuring, so it cannot pass vacuously just because the
 * space it ran against happens to hold only short titles.
 *
 * Requires a running dev server (see scripts/peek.mjs) that is already past
 * the sign-in screen for the profile Playwright starts with.
 *
 * Usage:
 *   node scripts/check-task-row-layout.mjs [options]
 *
 * Options:
 *   --url ORIGIN   dev server origin (default http://localhost:5173)
 *   --width N      viewport width (default 1200) — narrower makes the row
 *                  tighter, which is where the overflow showed up first
 *   --keep-open    leave the browser open on failure for inspection
 */
import { chromium } from "playwright";

const opts = { url: "http://localhost:5173", width: 1200, keepOpen: false };
const argv = process.argv.slice(2);
for (let i = 0; i < argv.length; i++) {
  const a = argv[i];
  if (a === "--url") opts.url = argv[++i];
  else if (a === "--width") opts.width = Number(argv[++i]);
  else if (a === "--keep-open") opts.keepOpen = true;
  else {
    console.error(`unknown option: ${a}`);
    process.exit(64);
  }
}

/** Long enough to overflow any realistic row width. */
const LONG_TITLE =
  "Investigate why the nightly ingestion pipeline intermittently drops the " +
  "last batch of records when the upstream connector renegotiates its TLS " +
  "session mid-transfer and the retry budget is already exhausted";

const browser = await chromium.launch();
const context = await browser.newContext({
  viewport: { width: opts.width, height: 950 },
  colorScheme: "dark",
});
// Skip the first-run welcome dialog; it covers the rows we measure.
await context.addInitScript(() =>
  window.localStorage.setItem(
    "donezo.onboarding.v1",
    JSON.stringify({ welcomed: true, tourDone: true, dismissedHints: [] })
  )
);
const page = await context.newPage();

const fail = async (msg) => {
  console.error(`FAIL ${msg}`);
  if (!opts.keepOpen) await browser.close();
  process.exit(1);
};

await page.goto(`${opts.url}/#/projects`, { waitUntil: "networkidle" });
await page.waitForTimeout(400);

if (await page.locator('input[type="password"]').count()) {
  await fail(
    "dev server is showing the sign-in screen — this check needs a session " +
      "(start a dev backend with --dev-auto-login, or sign in first)"
  );
}

// Open the first project that actually has open tasks to measure.
const rows = page.locator("tbody tr");
const projectCount = await rows.count();
if (projectCount === 0) await fail("no projects listed at #/projects");

let measured = null;
for (let i = 0; i < projectCount; i++) {
  await page.goto(`${opts.url}/#/projects`, { waitUntil: "networkidle" });
  await page.waitForTimeout(250);
  await page.locator("tbody tr").nth(i).click();
  await page.waitForTimeout(400);

  measured = await page.evaluate((longTitle) => {
    // A task row is the parent of a <label> wrapping a checkbox; the due chip,
    // when present, is the row's other child whose text starts with "due".
    const label = Array.from(document.querySelectorAll("label")).find((l) =>
      l.querySelector('input[type="checkbox"]')
    );
    if (!label) return null;
    const row = label.parentElement;
    const wrapper = label.querySelector(":scope > span:last-child");
    if (!row || !wrapper) return null;
    // The design system supplies the wrapper; the row nests its own truncating
    // span inside it. Write the fixture into that inner span rather than onto
    // the wrapper, which would replace the element under test.
    const title = wrapper.firstElementChild ?? wrapper;

    // Force the fixture in place of whatever this space happens to hold.
    // React will not reconcile before the synchronous measurement below.
    title.textContent = longTitle;

    const chip = Array.from(row.children).find(
      (c) => c !== label && /^due\b/i.test((c.textContent || "").trim())
    );
    const t = title.getBoundingClientRect();
    const r = row.getBoundingClientRect();
    return {
      overflowsRow: Math.round(t.right - r.right),
      overlapsChip: chip
        ? Math.round(t.right - chip.getBoundingClientRect().left)
        : null,
      clipped: title.scrollWidth > title.clientWidth,
      titleWidth: Math.round(t.width),
    };
  }, LONG_TITLE);

  if (measured) break;
}

if (!measured) await fail("found no project with an open-task row to measure");

// 1px of slack absorbs sub-pixel rounding; the real bug overflowed by ~118px.
const SLACK = 1;
const problems = [];
if (measured.overflowsRow > SLACK) {
  problems.push(`title runs ${measured.overflowsRow}px past the row's right edge`);
}
if (measured.overlapsChip !== null && measured.overlapsChip > SLACK) {
  problems.push(`title overlaps the due chip by ${measured.overlapsChip}px`);
}
// If the row never clipped, the title was allowed to size to its content —
// which is the defect even when the row happens to be wide enough to hide it.
if (!measured.clipped) {
  problems.push(
    `title did not clip (${measured.titleWidth}px wide) — its flex parent is ` +
      "sizing to the text instead of truncating"
  );
}

if (problems.length) {
  await fail(`long task title is not contained:\n  - ${problems.join("\n  - ")}`);
}

console.log(
  `ok  long task title truncates: ${measured.overflowsRow}px inside the row, ` +
    `${measured.overlapsChip === null ? "no" : measured.overlapsChip + "px"} ` +
    "clearance to the due chip"
);
await browser.close();
