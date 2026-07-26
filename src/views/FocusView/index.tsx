import { format } from "date-fns";

import { formatDay, parseDate } from "@/lib/time";
import { useFocusData } from "./useFocusData";
import { NowSection } from "./NowSection";
import { NextActionPanel } from "./NextActionPanel";
import { TimeSensitiveSection } from "./TimeSensitiveSection";
import { WaitingSection } from "./WaitingSection";
import { InterruptedSection } from "./InterruptedSection";
import { TodaySection } from "./TodaySection";

/** Quiet one-line pulse for the date header, from real counts. */
function summarize(timeSensitive: number, waiting: number): string {
  const ts =
    timeSensitive === 0
      ? "Nothing time-sensitive this week"
      : timeSensitive === 1
        ? "1 item is time-sensitive"
        : `${timeSensitive} items are time-sensitive`;
  const w =
    waiting === 0
      ? "no threads waiting on someone else"
      : waiting === 1
        ? "1 thread waiting on someone else"
        : `${waiting} threads waiting on someone else`;
  return `${ts} · ${w}.`;
}

/** Focus — answers "what should I do right now?" in one vertical read. */
export default function FocusView() {
  const data = useFocusData();
  const waitingCount = data.waitingTasks.length + data.waitingProjects.length;

  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto max-w-[880px] space-y-7 px-4 py-6 sm:px-6 lg:px-8">
        <header>
          <div className="font-mono text-[0.68rem] uppercase tracking-label text-gtc-muted">
            {format(parseDate(data.today), "EEEE")} · {formatDay(data.today)}
          </div>
          <p className="mt-1.5 font-sans text-[0.85rem] text-gtc-muted">
            {summarize(data.timeSensitive.length, waitingCount)}
          </p>
        </header>

        <NowSection project={data.nowProject} lastTouched={data.nowLastTouched} />
        <NextActionPanel project={data.nowProject} />
        <TimeSensitiveSection rows={data.timeSensitive} />
        {waitingCount > 0 && (
          <WaitingSection tasks={data.waitingTasks} projects={data.waitingProjects} />
        )}
        {data.interrupted.length > 0 && <InterruptedSection rows={data.interrupted} />}
        <TodaySection rows={data.todayRows} totalHours={data.todayEffort} />
      </div>
    </div>
  );
}
