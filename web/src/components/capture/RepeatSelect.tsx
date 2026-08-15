import * as React from "react";
import { Select, Input } from "@grewelltech/console";

import type { ReminderRepeat, RepeatUnit } from "@/domain/types";
import {
  MAX_REPEAT_EVERY,
  REPEAT_CUSTOM,
  REPEAT_PRESETS,
  presetIdFor,
} from "@/lib/repeat";

/** Recurrence picker for a reminder: a preset menu (does-not-repeat, hourly,
 *  daily, weekly) plus a Custom… entry that reveals a count and unit.
 *
 *  Emits `undefined` when the reminder does not repeat and a {every, unit}
 *  interval otherwise. It keeps the custom row open once chosen, so a partially
 *  entered interval does not collapse the field back to a preset. */
export function RepeatSelect({
  value,
  onChange,
}: {
  value: ReminderRepeat | undefined;
  onChange: (repeat: ReminderRepeat | undefined) => void;
}) {
  const [custom, setCustom] = React.useState(() => presetIdFor(value) === REPEAT_CUSTOM);
  const [every, setEvery] = React.useState(() => value?.every ?? 2);
  const [unit, setUnit] = React.useState<RepeatUnit>(() => value?.unit ?? "hour");

  const selectValue = custom ? REPEAT_CUSTOM : presetIdFor(value);

  const choosePreset = (id: string) => {
    if (id === REPEAT_CUSTOM) {
      setCustom(true);
      onChange({ every, unit });
      return;
    }
    setCustom(false);
    const preset = REPEAT_PRESETS.find((p) => p.id === id);
    onChange(preset?.repeat ?? undefined);
  };

  // Clamp the count into the range the server accepts, so a stray 0 or blank
  // does not produce an interval the API will reject.
  const clampEvery = (raw: string) => {
    const n = Math.floor(Number(raw));
    if (!Number.isFinite(n) || n < 1) return 1;
    return Math.min(n, MAX_REPEAT_EVERY);
  };

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Select
        value={selectValue}
        onChange={(e) => choosePreset(e.target.value)}
        aria-label="Repeat"
        className="!py-1.5 !text-[0.75rem]"
      >
        {REPEAT_PRESETS.map((p) => (
          <option key={p.id} value={p.id}>
            {p.label}
          </option>
        ))}
        <option value={REPEAT_CUSTOM}>Custom…</option>
      </Select>
      {custom && (
        <div className="flex items-center gap-1.5">
          <span className="font-sans text-[0.75rem] text-gtc-muted">every</span>
          <Input
            type="number"
            min={1}
            max={MAX_REPEAT_EVERY}
            value={String(every)}
            onChange={(e) => {
              const n = clampEvery(e.target.value);
              setEvery(n);
              onChange({ every: n, unit });
            }}
            aria-label="Repeat interval count"
            className="!w-[4.5rem] !py-1.5 !text-[0.75rem]"
          />
          <Select
            value={unit}
            onChange={(e) => {
              const u = e.target.value as RepeatUnit;
              setUnit(u);
              onChange({ every, unit: u });
            }}
            aria-label="Repeat interval unit"
            className="!py-1.5 !text-[0.75rem]"
          >
            <option value="hour">hours</option>
            <option value="day">days</option>
            <option value="week">weeks</option>
          </Select>
        </div>
      )}
    </div>
  );
}
