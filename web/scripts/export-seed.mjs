/**
 * Export the frontend mock dataset as seed/seed.json for donezod --seed.
 *
 * mockData.ts only imports types (erased by type stripping), so it loads
 * directly under Node's TypeScript type stripping:
 *
 *   cd web && node --experimental-strip-types scripts/export-seed.mjs
 *
 * (or `make seed-json` from the repo root). The output is committed so
 * donezod can seed without Node installed; regenerate after editing
 * src/domain/mockData.ts.
 */
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import {
  ACTIVITIES,
  INBOX_ITEMS,
  NOTES,
  PROJECTS,
  REMINDERS,
  TASKS,
} from "../src/domain/mockData.ts";

const here = dirname(fileURLToPath(import.meta.url));
const outFile = join(here, "..", "..", "seed", "seed.json");

const dataset = {
  projects: PROJECTS,
  activities: ACTIVITIES,
  tasks: TASKS,
  notes: NOTES,
  reminders: REMINDERS,
  inbox: INBOX_ITEMS,
};

mkdirSync(dirname(outFile), { recursive: true });
writeFileSync(outFile, `${JSON.stringify(dataset, null, 2)}\n`);

const counts = Object.entries(dataset)
  .map(([name, rows]) => `${name}=${rows.length}`)
  .join(" ");
console.log(`wrote ${outFile} (${counts})`);
