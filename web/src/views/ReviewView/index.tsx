import type { ReactNode } from "react";
import { Button, SectionLabel, cn, type ButtonProps } from "@grewelltech/console";

import type { Project } from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { latestActivityDate } from "@/state/selectors";
import { addDaysISO, diffDays, formatDay, relativeFromToday, todayISO } from "@/lib/time";
import { EmptyState } from "@/components/common/EmptyState";
import { DetailsDisclosure } from "@/components/common/DetailsDisclosure";
import { ProjectMark } from "@/components/common/ProjectMark";

const META = "font-mono text-[0.64rem] uppercase tracking-label text-gtc-muted";
const TITLE = "truncate text-[0.85rem] text-gtc-text";

/** Compact ghost action for review rows. */
function RowAction({ muted, className, ...props }: ButtonProps & { muted?: boolean }) {
  return (
    <Button
      size="sm"
      noGlyph
      className={cn(
        "px-2 py-1 text-[0.64rem]",
        muted &&
          "border-gtc-line text-gtc-muted hover:border-gtc-line hover:bg-gtc-inset hover:text-gtc-text",
        className
      )}
      {...props}
    />
  );
}

/** One resurfaced item: content left, actions right, hairline below.
 *  On narrow screens the action row drops under the content, right-aligned. */
function ReviewRow({ children, actions }: { children: ReactNode; actions?: ReactNode }) {
  return (
    <div className="-mx-2 flex flex-wrap items-center gap-x-3 gap-y-1.5 border-b border-gtc-line/60 px-2 py-2 transition-colors hover:bg-gtc-tint-accent">
      <div className="min-w-0 grow basis-full sm:basis-0">{children}</div>
      {actions && (
        <div className="flex shrink-0 grow items-center justify-end gap-1.5 sm:grow-0">
          {actions}
        </div>
      )}
    </div>
  );
}

/** Section header + rows; collapses to a single muted line when empty. */
function ReviewSection({
  label,
  count,
  emptyLine,
  children,
}: {
  label: string;
  count: number;
  emptyLine: string;
  children?: ReactNode;
}) {
  return (
    <section>
      <SectionLabel className="my-0 mb-1" trailing={<span className="text-gtc-text">{count}</span>}>
        {label}
      </SectionLabel>
      {count === 0 ? (
        <p className="py-1.5 text-[0.8rem] text-gtc-muted">{emptyLine}</p>
      ) : (
        <div>{children}</div>
      )}
    </section>
  );
}

/** Gentle resurfacing: stale items, still-relevant checks, reschedules. */
export default function ReviewView() {
  const state = useAppState();
  const dispatch = useAppDispatch();
  const today = todayISO();

  // NEEDS REVIEW — captures that sat, tasks whose due date slipped past.
  const staleInbox = state.inbox
    .filter((i) => i.status === "pending" && diffDays(today, i.capturedAt.slice(0, 10)) > 3)
    .sort((a, b) => a.capturedAt.localeCompare(b.capturedAt));
  const overdueTasks = state.tasks
    .filter((t) => t.status === "open" && !!t.due && t.due < today)
    .sort((a, b) => (a.due ?? "").localeCompare(b.due ?? ""));

  // STILL RELEVANT? — old someday tasks, paused projects gone quiet.
  const oldSomeday = state.tasks
    .filter((t) => t.status === "someday" && diffDays(today, t.createdAt) > 30)
    .sort((a, b) => a.createdAt.localeCompare(b.createdAt));
  const quietPaused = state.projects.filter((p) => {
    if (p.status !== "paused") return false;
    const latest = latestActivityDate(state, p.id);
    return !latest || diffDays(today, latest) > 30;
  });

  // WAITING TOO LONG — waiting tasks and waiting/blocked projects gone stale.
  const longWaitingTasks = state.tasks
    .filter((t) => t.status === "waiting" && diffDays(today, t.createdAt) > 14)
    .sort((a, b) => a.createdAt.localeCompare(b.createdAt));
  const longWaitingProjects = state.projects.filter((p) => {
    if (p.status !== "waiting" && p.status !== "blocked") return false;
    const latest = latestActivityDate(state, p.id);
    return !latest || diffDays(today, latest) > 14;
  });

  const needsReviewCount = staleInbox.length + overdueTasks.length;
  const stillRelevantCount = oldSomeday.length + quietPaused.length;
  const waitingCount = longWaitingTasks.length + longWaitingProjects.length;
  const total = needsReviewCount + stillRelevantCount + waitingCount;

  const projectRowMeta = (p: Project, prefix: string) => {
    const latest = latestActivityDate(state, p.id);
    return latest ? `${prefix} ${relativeFromToday(latest)}` : `${prefix} —`;
  };

  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto max-w-[880px] space-y-7 px-4 py-6 sm:px-6 lg:px-8">
        <p className="max-w-[62ch] text-[0.85rem] text-gtc-muted">
          A gentle sweep of things that may have drifted. Nothing here is urgent.
        </p>

        {total === 0 ? (
          <EmptyState
            title="All clear"
            hint="Nothing has drifted out of view. Come back after a few days away."
          />
        ) : (
          <>
            <ReviewSection
              label="Needs review"
              count={needsReviewCount}
              emptyLine="Nothing needs review."
            >
              {staleInbox.map((i) => (
                <ReviewRow
                  key={i.id}
                  actions={
                    <RowAction onClick={() => dispatch({ type: "SET_VIEW", view: "inbox" })}>
                      Open inbox
                    </RowAction>
                  }
                >
                  <div className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5">
                    <span className={cn(TITLE, "min-w-0 grow basis-full sm:basis-0")}>
                      {i.raw}
                    </span>
                    <span className={cn(META, "shrink-0")}>
                      captured {relativeFromToday(i.capturedAt.slice(0, 10))}
                    </span>
                  </div>
                </ReviewRow>
              ))}
              {overdueTasks.map((t) => (
                <ReviewRow
                  key={t.id}
                  actions={
                    <>
                      <RowAction
                        onClick={() =>
                          dispatch({
                            type: "UPDATE_TASK",
                            id: t.id,
                            patch: { due: addDaysISO(today, 7) },
                          })
                        }
                      >
                        Reschedule +1w
                      </RowAction>
                      <RowAction
                        onClick={() =>
                          dispatch({ type: "UPDATE_TASK", id: t.id, patch: { status: "someday" } })
                        }
                      >
                        Move to someday
                      </RowAction>
                      <RowAction
                        onClick={() =>
                          dispatch({ type: "UPDATE_TASK", id: t.id, patch: { status: "done" } })
                        }
                      >
                        Done
                      </RowAction>
                    </>
                  }
                >
                  <div className={TITLE}>{t.title}</div>
                  <DetailsDisclosure details={t.details} />
                  <div className={cn(META, "mt-0.5")}>
                    was due {formatDay(t.due ?? today)} — still relevant?
                  </div>
                </ReviewRow>
              ))}
            </ReviewSection>

            <ReviewSection
              label="Still relevant?"
              count={stillRelevantCount}
              emptyLine="Nothing to reconsider."
            >
              {oldSomeday.map((t) => (
                <ReviewRow
                  key={t.id}
                  actions={
                    <>
                      <RowAction
                        onClick={() =>
                          dispatch({ type: "UPDATE_TASK", id: t.id, patch: { status: "open" } })
                        }
                      >
                        Reactivate
                      </RowAction>
                      <RowAction
                        muted
                        onClick={() =>
                          dispatch({ type: "UPDATE_TASK", id: t.id, patch: { status: "done" } })
                        }
                      >
                        Let it go
                      </RowAction>
                    </>
                  }
                >
                  <div className={TITLE}>{t.title}</div>
                  <DetailsDisclosure details={t.details} />
                  <div className={cn(META, "mt-0.5")}>
                    someday · added {relativeFromToday(t.createdAt)}
                  </div>
                </ReviewRow>
              ))}
              {quietPaused.map((p) => (
                <ReviewRow
                  key={p.id}
                  actions={
                    <RowAction
                      onClick={() => dispatch({ type: "OPEN_PROJECT", projectId: p.id })}
                    >
                      Open
                    </RowAction>
                  }
                >
                  <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5">
                    <ProjectMark color={p.color} muted />
                    <span className={TITLE}>{p.name}</span>
                    <span className={cn(META, "shrink-0 basis-full sm:basis-auto")}>
                      {projectRowMeta(p, "paused · last touched")}
                    </span>
                  </div>
                </ReviewRow>
              ))}
            </ReviewSection>

            <ReviewSection
              label="Waiting too long"
              count={waitingCount}
              emptyLine="Nothing waiting too long."
            >
              {longWaitingTasks.map((t) => (
                <ReviewRow
                  key={t.id}
                  actions={
                    <>
                      <RowAction
                        onClick={() =>
                          dispatch({ type: "UPDATE_TASK", id: t.id, patch: { status: "open" } })
                        }
                      >
                        Reactivate
                      </RowAction>
                      <RowAction
                        onClick={() =>
                          dispatch({ type: "UPDATE_TASK", id: t.id, patch: { status: "someday" } })
                        }
                      >
                        Move to someday
                      </RowAction>
                    </>
                  }
                >
                  <div className={TITLE}>{t.title}</div>
                  <DetailsDisclosure details={t.details} />
                  <div className={cn(META, "mt-0.5")}>
                    waiting on {t.waitingOn ?? "—"} · since {formatDay(t.createdAt)}
                  </div>
                </ReviewRow>
              ))}
              {longWaitingProjects.map((p) => {
                const latest = latestActivityDate(state, p.id);
                return (
                  <ReviewRow
                    key={p.id}
                    actions={
                      <RowAction
                        onClick={() => dispatch({ type: "OPEN_PROJECT", projectId: p.id })}
                      >
                        Open
                      </RowAction>
                    }
                  >
                    <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5">
                      <ProjectMark color={p.color} />
                      <span className={TITLE}>{p.name}</span>
                      <span className={cn(META, "shrink-0 basis-full sm:basis-auto")}>
                        waiting on {p.waitingOn ?? "—"} · since {latest ? formatDay(latest) : "—"}
                      </span>
                    </div>
                  </ReviewRow>
                );
              })}
            </ReviewSection>
          </>
        )}
      </div>
    </div>
  );
}
