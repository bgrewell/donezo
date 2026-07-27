import { Input, cn } from "@grewelltech/console";

import type { Project, ProjectColor } from "@/domain/types";
import { projectColorVar } from "@/lib/projectColors";
import { QuietLabel } from "./chips";

/** The --dz-pj-* ramp in picker order. */
export const COLOR_RAMP: ProjectColor[] = [
  "blue",
  "green",
  "tan",
  "violet",
  "rose",
  "orange",
  "steel",
];

/** First ramp color not used by an existing project (falls back to cycling
 *  the ramp when every color is taken). */
export function firstUnusedColor(projects: Project[]): ProjectColor {
  const used = new Set(projects.map((p) => p.color));
  return COLOR_RAMP.find((c) => !used.has(c)) ?? COLOR_RAMP[projects.length % COLOR_RAMP.length];
}

/** Tailored fields for a PROJECT capture: color swatches + optional
 *  purpose. The captured text becomes the project name — no project
 *  selector here (a project cannot belong to a project). */
export function ProjectFields({
  color,
  onColor,
  purpose,
  onPurpose,
}: {
  color: ProjectColor;
  onColor: (c: ProjectColor) => void;
  purpose: string;
  onPurpose: (purpose: string) => void;
}) {
  return (
    <div className="space-y-1.5">
      <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label="Project color">
        <QuietLabel>Color</QuietLabel>
        {COLOR_RAMP.map((c) => {
          const selected = color === c;
          return (
            <button
              key={c}
              type="button"
              aria-label={`Color ${c}`}
              aria-pressed={selected}
              onClick={() => onColor(c)}
              className={cn(
                "h-5 w-5 shrink-0 rounded-[3px] border outline-none transition-colors",
                "focus-visible:shadow-gtc-focus",
                selected
                  ? "border-gtc-accent outline outline-1 outline-offset-1 outline-gtc-accent"
                  : "border-gtc-line hover:border-gtc-text"
              )}
              style={{ background: projectColorVar(c) }}
            />
          );
        })}
      </div>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5">
        <QuietLabel htmlFor="qc-project-purpose">Purpose</QuietLabel>
        <Input
          id="qc-project-purpose"
          value={purpose}
          onChange={(e) => onPurpose(e.target.value)}
          placeholder="Why this project exists (optional)"
          className="min-w-[12rem] flex-1 !py-1.5 !font-sans !text-[0.8rem] normal-case"
        />
      </div>
    </div>
  );
}
