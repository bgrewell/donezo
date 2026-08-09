import * as React from "react";
import { Button, SectionLabel, cn } from "@grewelltech/console";

import {
  emptyTrash,
  fetchSpaceState,
  fetchTrash,
  purgeTrashItem,
  restoreTrashItem,
  type TrashItem,
} from "@/api/client";
import { useSpaceId } from "@/state/AppStore";
import { useAppDispatch } from "@/state/AppStore";
import { relativeFromInstant } from "@/lib/time";
import { EmptyState } from "@/components/common/EmptyState";

/** How a trashed entity is named to a person. */
const ENTITY_LABEL: Record<string, string> = {
  project: "project",
  activity: "activity",
  task: "task",
  note: "note",
  reminder: "reminder",
  inbox_item: "capture",
};

const META = "font-mono text-[0.66rem] uppercase tracking-label text-gtc-muted";

/** Everything deleted recently, with a way to undo it.
 *
 *  Deliberately plain. The trash is somewhere you arrive after a mistake,
 *  wanting one specific thing back and then to leave — so it is a flat list in
 *  the order things were deleted, not another surface to organise.
 *
 *  The store is not told about restores. Rather than teach the reducer to
 *  reconstruct six entity types from a trash row, a restore refetches the
 *  space — which is what the freshness poll would do a moment later anyway,
 *  and it cannot drift from what the server actually did. */
export default function TrashView() {
  const spaceId = useSpaceId();
  const dispatch = useAppDispatch();
  const [items, setItems] = React.useState<TrashItem[] | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  const [busy, setBusy] = React.useState<string | null>(null);
  const [confirmEmpty, setConfirmEmpty] = React.useState(false);

  const load = React.useCallback(() => {
    if (!spaceId) return;
    fetchTrash(spaceId)
      .then((t) => {
        setItems(t);
        setError(null);
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)));
  }, [spaceId]);

  React.useEffect(load, [load]);

  const act = async (key: string, fn: () => Promise<unknown>) => {
    setBusy(key);
    try {
      await fn();
      load();
      // A restore puts rows back across as many as six tables at once, so the
      // space is re-read rather than patched. This is the same REPLACE_STATE
      // the freshness poll would apply a few seconds later; doing it now just
      // means the rest of the app is not briefly wrong.
      dispatch({ type: "REPLACE_STATE", data: await fetchSpaceState(spaceId) });
      setError(null);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  };


  return (
    <div className="h-full overflow-y-auto px-6 py-5">
      <div className="mx-auto max-w-3xl">
        <div className="mb-3 flex items-center justify-between gap-3">
          <SectionLabel className="mb-0">Trash</SectionLabel>
          {items && items.length > 0 && (
            <span className="flex items-center gap-2">
              {confirmEmpty ? (
                <>
                  <span className={META}>delete {items.length} item(s) for good?</span>
                  <Button
                    size="sm"
                    variant="danger"
                    noGlyph
                    disabled={busy !== null}
                    onClick={() =>
                      void act("empty", () => emptyTrash(spaceId)).then(() => setConfirmEmpty(false))
                    }
                  >
                    Empty trash
                  </Button>
                  <Button size="sm" variant="ghost" noGlyph onClick={() => setConfirmEmpty(false)}>
                    Keep
                  </Button>
                </>
              ) : (
                <Button size="sm" variant="ghost" noGlyph onClick={() => setConfirmEmpty(true)}>
                  Empty trash
                </Button>
              )}
            </span>
          )}
        </div>

        <p className="mb-4 font-sans text-[0.8rem] text-gtc-muted">
          Deleted items stay here until they are restored, removed for good, or age out of the
          instance&rsquo;s retention window. Restoring brings back everything deleted at the same
          time.
        </p>

        {error && (
          <p className="mb-3 font-mono text-[0.7rem] text-gtc-danger" role="alert">
            {error}
          </p>
        )}

        {items === null && <p className={META}>loading…</p>}

        {items !== null && items.length === 0 && (
          <EmptyState
            title="Nothing deleted"
            hint="Anything you delete lands here first, so a mistake is recoverable."
          />
        )}

        {items !== null && items.length > 0 && (
          <ul>
            {items.map((it) => {
              const key = `${it.entity}:${it.id}`;
              const working = busy === key;
              return (
                <li
                  key={key}
                  className="group flex items-start gap-3 border-b border-gtc-line/60 py-2.5"
                >
                  <span className={cn(META, "w-20 shrink-0 pt-0.5")}>
                    {ENTITY_LABEL[it.entity] ?? it.entity}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-sans text-[0.85rem] text-gtc-text">
                      {it.label || <span className="text-gtc-muted">(untitled)</span>}
                    </span>
                    <span className={cn(META, "mt-0.5 block")}>
                      deleted {relativeFromInstant(it.deletedAt)}
                      {it.batchSize > 1 && ` · with ${it.batchSize - 1} other item(s)`}
                    </span>
                  </span>
                  <span className="flex shrink-0 items-center gap-1 opacity-0 transition-opacity focus-within:opacity-100 group-hover:opacity-100">
                    <Button
                      size="sm"
                      variant="ghost"
                      noGlyph
                      disabled={busy !== null}
                      onClick={() => void act(key, () => restoreTrashItem(spaceId, it.entity, it.id))}
                    >
                      {working ? "…" : "Restore"}
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      noGlyph
                      disabled={busy !== null}
                      onClick={() => void act(key, () => purgeTrashItem(spaceId, it.entity, it.id))}
                    >
                      Delete forever
                    </Button>
                  </span>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}
