import type { ViewId } from "@/domain/types";

export const VIEW_IDS: ViewId[] = [
  "focus",
  "timeline",
  "inbox",
  "projects",
  "review",
  "search",
];

export interface ParsedRoute {
  view: ViewId;
  projectId?: string;
}

/** Parse "#/projects/loom"-style hashes. Returns null for unknown routes. */
export function parseHash(hash: string): ParsedRoute | null {
  const parts = hash.replace(/^#\/?/, "").split("/");
  const head = parts[0] as ViewId;
  if (VIEW_IDS.includes(head)) {
    return { view: head, projectId: parts[1] || undefined };
  }
  return null;
}
