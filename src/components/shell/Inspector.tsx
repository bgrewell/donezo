import * as React from "react";
import { ExternalLink, X } from "lucide-react";
import { Button, Field, Input, SectionLabel, Select, cn } from "@grewelltech/console";

import type { ActivityEntry, ActivityType } from "@/domain/types";
import { useAppDispatch, useAppState } from "@/state/AppStore";
import { projectById, selectedActivity } from "@/state/selectors";
import { formatFull } from "@/lib/time";
import { projectColorVar } from "@/lib/projectColors";
import { ProjectMark } from "@/components/common/ProjectMark";
import {
  ACTIVITY_TYPES,
  ACTIVITY_TYPE_IDS,
  ActivityTypeIcon,
} from "@/components/common/activityTypes";

/** Right-side activity inspector. Opens when an activity is selected;
 *  inline at xl+, overlaying the workspace below that. */
export function Inspector() {
  const state = useAppState();
  const activity = selectedActivity(state);
  const open = activity != null;

  // Focus handling lives here, outside the id-keyed panel, so selection
  // changes remount the panel without re-stealing focus: on open from
  // closed, jump to the close button (the panel sits ~150 tab stops after
  // the timeline); on close, return focus to whatever opened it.
  const closeButtonRef = React.useRef<HTMLButtonElement>(null);
  const restoreRef = React.useRef<HTMLElement | null>(null);
  React.useEffect(() => {
    if (open) {
      restoreRef.current =
        document.activeElement instanceof HTMLElement ? document.activeElement : null;
      closeButtonRef.current?.focus();
    } else if (restoreRef.current) {
      if (restoreRef.current.isConnected) restoreRef.current.focus();
      restoreRef.current = null;
    }
  }, [open]);

  if (!activity) return null;
  // Key by id so edit/delete state resets when the selection changes.
  return (
    <InspectorPanel key={activity.id} activity={activity} closeButtonRef={closeButtonRef} />
  );
}

type Mode = "view" | "edit" | "confirm-delete";

interface Draft {
  title: string;
  details: string;
  type: ActivityType;
  effort: string;
  nextAction: string;
}

function InspectorPanel({
  activity,
  closeButtonRef,
}: {
  activity: ActivityEntry;
  closeButtonRef: React.Ref<HTMLButtonElement>;
}) {
  const state = useAppState();
  const dispatch = useAppDispatch();
  const project = projectById(state, activity.projectId);

  const [mode, setMode] = React.useState<Mode>("view");
  const [draft, setDraft] = React.useState<Draft>(() => ({
    title: activity.title,
    details: activity.details,
    type: activity.type,
    effort: activity.effortHours != null ? String(activity.effortHours) : "",
    nextAction: activity.nextAction ?? "",
  }));

  const startEdit = () => {
    setDraft({
      title: activity.title,
      details: activity.details,
      type: activity.type,
      effort: activity.effortHours != null ? String(activity.effortHours) : "",
      nextAction: activity.nextAction ?? "",
    });
    setMode("edit");
  };

  const save = () => {
    const effort = Number(draft.effort);
    const patch: Partial<ActivityEntry> = {
      title: draft.title.trim() || activity.title,
      details: draft.details,
      type: draft.type,
      effortHours: draft.effort.trim() === "" || Number.isNaN(effort) ? undefined : effort,
      nextAction: draft.nextAction.trim() || undefined,
    };
    dispatch({ type: "UPDATE_ACTIVITY", id: activity.id, patch });
    setMode("view");
  };

  const typeMeta = ACTIVITY_TYPES[activity.type];

  return (
    <aside
      aria-label="Activity inspector"
      className={cn(
        "fixed inset-y-0 right-0 z-40 flex w-[var(--dz-inspector-w)] flex-col",
        "border-l border-gtc-line bg-gtc-panel",
        "xl:static xl:inset-auto xl:z-auto"
      )}
    >
      {/* Header */}
      <div className="flex h-[var(--dz-topbar-h)] shrink-0 items-center border-b border-gtc-line px-3">
        <span className="font-mono text-[0.68rem] uppercase tracking-label text-gtc-muted">
          Activity
        </span>
        <div className="flex-1" />
        <button
          ref={closeButtonRef}
          type="button"
          aria-label="Close inspector"
          onClick={() => dispatch({ type: "SELECT_ACTIVITY", id: null })}
          className={cn(
            "flex h-7 w-7 items-center justify-center rounded-gtc text-gtc-muted",
            "outline-none transition-colors hover:text-gtc-text focus-visible:shadow-gtc-focus"
          )}
        >
          <X className="h-4 w-4" aria-hidden />
        </button>
      </div>

      {/* Body */}
      <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-4 py-3">
        {mode === "edit" ? (
          <>
            <Field label="Title" htmlFor="insp-title">
              <Input
                id="insp-title"
                value={draft.title}
                onChange={(e) => setDraft({ ...draft, title: e.target.value })}
                className="!font-sans normal-case"
              />
            </Field>
            <Field label="Details" htmlFor="insp-details">
              <textarea
                id="insp-details"
                rows={6}
                value={draft.details}
                onChange={(e) => setDraft({ ...draft, details: e.target.value })}
                className={cn(
                  "w-full resize-y rounded-gtc border border-gtc-line bg-gtc-inset px-3 py-[9px]",
                  "font-sans text-[0.85rem] leading-relaxed text-gtc-text placeholder:text-gtc-muted/70",
                  "transition-shadow focus:border-gtc-accent focus:outline-none focus:shadow-gtc-focus"
                )}
              />
            </Field>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Type" htmlFor="insp-type">
                <Select
                  id="insp-type"
                  value={draft.type}
                  onChange={(e) => setDraft({ ...draft, type: e.target.value as ActivityType })}
                >
                  {ACTIVITY_TYPE_IDS.map((t) => (
                    <option key={t} value={t}>
                      {ACTIVITY_TYPES[t].label}
                    </option>
                  ))}
                </Select>
              </Field>
              <Field label="Effort (h)" htmlFor="insp-effort">
                <Input
                  id="insp-effort"
                  type="number"
                  step={0.5}
                  min={0}
                  value={draft.effort}
                  onChange={(e) => setDraft({ ...draft, effort: e.target.value })}
                />
              </Field>
            </div>
            <Field label="Next action" htmlFor="insp-next">
              <Input
                id="insp-next"
                value={draft.nextAction}
                onChange={(e) => setDraft({ ...draft, nextAction: e.target.value })}
                placeholder="Single next concrete step"
                className="!font-sans normal-case"
              />
            </Field>
          </>
        ) : (
          <>
            {/* Project */}
            <div className="flex items-center gap-2">
              {project && (
                <>
                  <ProjectMark color={project.color} />
                  <button
                    type="button"
                    onClick={() => dispatch({ type: "OPEN_PROJECT", projectId: project.id })}
                    className={cn(
                      "rounded-gtc font-mono text-[0.72rem] uppercase tracking-chrome text-gtc-text",
                      "outline-none transition-colors hover:text-gtc-accent focus-visible:shadow-gtc-focus"
                    )}
                  >
                    {project.name}
                  </button>
                </>
              )}
              {activity.planned && (
                <span className="rounded-gtc border border-dashed border-gtc-line px-1.5 py-0.5 font-mono text-[0.6rem] uppercase tracking-label text-gtc-muted">
                  Planned
                </span>
              )}
            </div>

            {/* Date */}
            <div className="font-mono text-[0.68rem] uppercase tracking-label text-gtc-muted">
              {formatFull(activity.date)}
            </div>

            {/* Type chip */}
            <div>
              <span
                className={cn(
                  "inline-flex items-center gap-1.5 rounded-gtc border px-2 py-1",
                  "font-mono text-[0.64rem] uppercase tracking-chrome",
                  typeMeta.emphasis === "danger"
                    ? "border-gtc-danger-dim text-gtc-danger"
                    : "border-gtc-line text-gtc-text"
                )}
                style={
                  typeMeta.emphasis === "milestone" && project
                    ? {
                        borderColor: `color-mix(in srgb, ${projectColorVar(project.color)} 60%, transparent)`,
                      }
                    : undefined
                }
              >
                <ActivityTypeIcon type={activity.type} className="h-3.5 w-3.5" />
                {typeMeta.label}
              </span>
            </div>

            {/* Title */}
            <h2 className="font-sans text-[1rem] font-medium normal-case leading-snug text-gtc-text">
              {activity.title}
            </h2>

            {/* Details */}
            {activity.details && (
              <p className="whitespace-pre-wrap font-sans text-[0.85rem] leading-relaxed text-gtc-text">
                {activity.details}
              </p>
            )}

            {/* Meta */}
            <dl className="grid grid-cols-2 gap-x-4 border-t border-gtc-line pt-3 text-[0.72rem]">
              <div>
                <dt className="font-mono uppercase tracking-label text-gtc-muted">Effort</dt>
                <dd className="mt-0.5 font-mono text-gtc-text">
                  {activity.effortHours != null ? `${activity.effortHours}h` : "—"}
                </dd>
              </div>
              <div>
                <dt className="font-mono uppercase tracking-label text-gtc-muted">Source</dt>
                <dd className="mt-0.5 font-mono text-gtc-text">{activity.source}</dd>
              </div>
            </dl>

            {/* Tags */}
            {activity.tags.length > 0 && (
              <div>
                <div className="mb-1.5 font-mono text-[0.66rem] uppercase tracking-label text-gtc-muted">
                  Tags
                </div>
                <div className="flex flex-wrap gap-1.5">
                  {activity.tags.map((tag) => (
                    <span
                      key={tag}
                      className="rounded-gtc border border-gtc-line px-1.5 py-0.5 font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted"
                    >
                      {tag}
                    </span>
                  ))}
                </div>
              </div>
            )}

            {/* Links */}
            {activity.links.length > 0 && (
              <div>
                <div className="mb-1.5 font-mono text-[0.66rem] uppercase tracking-label text-gtc-muted">
                  Links
                </div>
                <div className="space-y-1">
                  {activity.links.map((link) => (
                    <a
                      key={link.url}
                      href={link.url}
                      target="_blank"
                      rel="noreferrer"
                      className={cn(
                        "flex items-center gap-1.5 rounded-gtc text-[0.78rem] text-gtc-accent",
                        "outline-none transition-colors hover:text-gtc-accent-bright focus-visible:shadow-gtc-focus"
                      )}
                    >
                      <ExternalLink className="h-3.5 w-3.5 shrink-0" aria-hidden />
                      <span className="truncate">{link.label}</span>
                    </a>
                  ))}
                </div>
              </div>
            )}

            {/* Next action */}
            {activity.nextAction && (
              <div>
                <SectionLabel className="my-0 mb-2">Next action</SectionLabel>
                <div className="border-l-2 border-gtc-accent bg-gtc-tint-accent px-3 py-2 font-sans text-[0.85rem] leading-relaxed text-gtc-text">
                  {activity.nextAction}
                </div>
              </div>
            )}
          </>
        )}
      </div>

      {/* Footer */}
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-t border-gtc-line px-3 py-2">
        {mode === "edit" ? (
          <>
            <div className="flex-1" />
            <Button size="sm" variant="ghost" noGlyph onClick={() => setMode("view")}>
              Cancel
            </Button>
            <Button size="sm" variant="primary" onClick={save}>
              Save
            </Button>
          </>
        ) : mode === "confirm-delete" ? (
          <>
            <span className="basis-full font-mono text-[0.64rem] uppercase tracking-label text-gtc-danger">
              Delete this entry?
            </span>
            <div className="flex-1" />
            <Button
              size="sm"
              variant="danger"
              noGlyph
              onClick={() => dispatch({ type: "DELETE_ACTIVITY", id: activity.id })}
            >
              Confirm delete
            </Button>
            <Button size="sm" variant="ghost" noGlyph onClick={() => setMode("view")}>
              Keep
            </Button>
          </>
        ) : (
          <>
            <Button size="sm" variant="ghost" noGlyph onClick={startEdit}>
              Edit
            </Button>
            <div className="flex-1" />
            <Button size="sm" variant="danger" noGlyph onClick={() => setMode("confirm-delete")}>
              Delete
            </Button>
          </>
        )}
      </div>
    </aside>
  );
}
