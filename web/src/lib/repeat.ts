/**
 * Reminder recurrence: the presets the picker offers and the words the app
 * uses to describe an interval.
 *
 * A recurring reminder re-reminds on its interval and stops only when it is
 * marked done — so the vocabulary here is deliberately about repetition, not a
 * calendar event.
 */
import type { ReminderRepeat, RepeatUnit } from "@/domain/types";

/** A quick-pick recurrence. `id` is the option value; `repeat` is what it
 *  means (null is "does not repeat"). */
export interface RepeatPreset {
  id: string;
  label: string;
  repeat: ReminderRepeat | null;
}

/** The preset menu, in order. "custom" is handled separately by the picker,
 *  which reveals a count + unit when it is chosen. */
export const REPEAT_PRESETS: RepeatPreset[] = [
  { id: "none", label: "Does not repeat", repeat: null },
  { id: "hourly", label: "Hourly", repeat: { every: 1, unit: "hour" } },
  { id: "daily", label: "Daily", repeat: { every: 1, unit: "day" } },
  { id: "weekly", label: "Weekly", repeat: { every: 1, unit: "week" } },
];

/** Sentinel option value for the custom (count + unit) branch. */
export const REPEAT_CUSTOM = "custom";

/** The bound the server enforces on the interval count (store.MaxRepeatEvery),
 *  mirrored so the field can reject nonsense before a round-trip. */
export const MAX_REPEAT_EVERY = 1000;

/** Singular names for each unit, for building phrases. */
const UNIT_SINGULAR: Record<RepeatUnit, string> = {
  hour: "hour",
  day: "day",
  week: "week",
};

/** Which preset (if any) a repeat value equals, so an editor can select the
 *  matching menu entry. Returns REPEAT_CUSTOM for an interval no preset covers,
 *  and "none" for undefined. */
export function presetIdFor(repeat: ReminderRepeat | undefined): string {
  if (!repeat) return "none";
  const match = REPEAT_PRESETS.find(
    (p) => p.repeat && p.repeat.every === repeat.every && p.repeat.unit === repeat.unit
  );
  return match ? match.id : REPEAT_CUSTOM;
}

/** A short phrase for an interval: "day" → "every day", "3 hours" → "every 3
 *  hours". Used both in the picker's custom row and wherever a reminder shows
 *  that it recurs. */
export function repeatPhrase(repeat: ReminderRepeat): string {
  const unit = UNIT_SINGULAR[repeat.unit];
  return repeat.every === 1 ? `every ${unit}` : `every ${repeat.every} ${unit}s`;
}

/** A capitalised label for a recurring reminder, e.g. "Repeats every 3 days".
 *  Returns "" for a one-shot reminder so callers can render nothing. */
export function describeRepeat(repeat: ReminderRepeat | undefined): string {
  if (!repeat) return "";
  return `Repeats ${repeatPhrase(repeat)}`;
}
