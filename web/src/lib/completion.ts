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
 *  A patch that sets completedAt is the person correcting it, so its source
 *  becomes manual. The source itself never travels: the server derives it the
 *  same way, and rejects it as an unknown PATCH field. */
export function applyCompletion<T extends Completable>(
  prev: T | undefined,
  patch: Partial<T>,
  finished: (x: T) => boolean
): T {
  const next = { ...prev, ...patch } as T;
  if ("completedAt" in patch) {
    next.completedSource = patch.completedAt ? "manual" : undefined;
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
