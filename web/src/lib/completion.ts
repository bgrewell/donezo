import type { CompletedSource } from "@/domain/types";
import { nowInstant } from "@/lib/time";

/** Something that records when it was finished: a task or a project. */
export interface Completable {
  completedAt?: string;
  completedSource?: CompletedSource;
}

/** Apply a patch the way the server will, for the optimistic local copy.
 *  Mirrors stampCompletion in internal/store/completion.go:
 *
 *  - becoming finished with no completedAt: stamped now, as recorded;
 *  - not finished: both fields cleared;
 *  - staying finished: as the patch leaves it, including cleared ("unknown").
 *
 *  A patch that sets or clears completedAt is the person correcting it, so
 *  its source becomes manual — kept even with no date. The source itself never travels: the server derives it the
 *  same way, and rejects it as an unknown PATCH field. */
export function applyCompletion<T extends Completable>(
  prev: T | undefined,
  patch: Partial<T>,
  finished: (x: T) => boolean
): T {
  const next = { ...prev, ...patch } as T;
  if ("completedAt" in patch) {
    // Setting or clearing: either way it is the person's call, and a
    // cleared date keeps the mark so a summary can tell it from one that
    // was never recorded.
    next.completedSource = "manual";
  }
  if (!finished(next)) {
    next.completedAt = undefined;
    next.completedSource = undefined;
  } else if (!next.completedAt && !(prev && finished(prev))) {
    next.completedAt = nowInstant();
    next.completedSource = "recorded";
  }
  return next;
}

export const taskFinished = (t: { status: string }) => t.status === "done";
export const projectFinished = (p: { status: string }) => p.status === "completed";
