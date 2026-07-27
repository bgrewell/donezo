import * as React from "react";
import { Archive, ArchiveRestore, Check, ChevronDown, Pencil, Plus } from "lucide-react";
import { Button, Input, cn } from "@grewelltech/console";

import type { ProjectColor, Space } from "@/domain/types";
import { useSession } from "@/components/auth/session";
import { useSyncErrors } from "@/state/AppStore";
import { projectColorVar } from "@/lib/projectColors";
import { ProjectMark } from "@/components/common/ProjectMark";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/Popover";
import { Tip } from "@/components/ui/Tooltip";

/** How long the "space created" confirmation stays up. */
const CREATED_NOTICE_MS = 4000;

// Set just before a successful create switches spaces: the whole shell
// is keyed on the space id, so the switch remounts this component and
// React state can't carry the confirmation across. Consumed on mount by
// the freshly mounted switcher in the new space.
let announceCreatedSpace = false;

const COLOR_RAMP: ProjectColor[] = ["blue", "green", "tan", "violet", "rose", "orange", "steel"];

/** Square color picker row shared by the new-space and rename forms. */
function ColorRow({
  value,
  onChange,
}: {
  value: ProjectColor;
  onChange: (c: ProjectColor) => void;
}) {
  return (
    <div className="flex items-center gap-1.5" role="group" aria-label="Space color">
      {COLOR_RAMP.map((c) => (
        <button
          key={c}
          type="button"
          aria-label={c}
          aria-pressed={value === c}
          onClick={() => onChange(c)}
          className={cn(
            "flex h-5 w-5 items-center justify-center rounded-gtc border outline-none transition-colors",
            "focus-visible:shadow-gtc-focus",
            value === c ? "border-gtc-accent" : "border-transparent hover:border-gtc-line"
          )}
        >
          <ProjectMark color={c} size={10} />
        </button>
      ))}
    </div>
  );
}

/** Inline name(+color) mini-form (new space / rename). */
function SpaceForm({
  initialName,
  initialColor,
  submitLabel,
  withColor = true,
  onSubmit,
  onCancel,
}: {
  initialName: string;
  initialColor: ProjectColor;
  submitLabel: string;
  withColor?: boolean;
  onSubmit: (name: string, color: ProjectColor) => Promise<void>;
  onCancel: () => void;
}) {
  const [name, setName] = React.useState(initialName);
  const [color, setColor] = React.useState<ProjectColor>(initialColor);
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const ready = name.trim() !== "" && !busy;

  const submit = async () => {
    if (!ready) return;
    setBusy(true);
    setError(null);
    try {
      await onSubmit(name.trim(), color);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setBusy(false);
    }
  };

  return (
    <div className="space-y-2.5 px-1 py-1">
      <Input
        autoFocus
        value={name}
        onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            void submit();
          } else if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            onCancel();
          }
        }}
        placeholder="Space name"
        aria-label="Space name"
        className="!py-1.5 !text-[0.8rem]"
      />
      {withColor && <ColorRow value={color} onChange={setColor} />}
      {error && (
        <p className="m-0 font-mono text-[0.66rem] text-gtc-danger" role="alert">
          ▸ {error}
        </p>
      )}
      <div className="flex items-center gap-1.5">
        <Button size="sm" variant="primary" noGlyph disabled={!ready} onClick={() => void submit()}>
          {submitLabel}
        </Button>
        <Button size="sm" variant="ghost" noGlyph onClick={onCancel}>
          Cancel
        </Button>
      </div>
    </div>
  );
}

/** Small square icon button used on space rows. */
function RowIconButton({
  label,
  onClick,
  children,
}: {
  label: string;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      className={cn(
        "flex h-6 w-6 shrink-0 items-center justify-center rounded-gtc text-gtc-muted outline-none",
        "transition-colors hover:bg-gtc-tint-accent hover:text-gtc-text focus-visible:shadow-gtc-focus"
      )}
    >
      {children}
    </button>
  );
}

type Mode = { kind: "list" } | { kind: "new" } | { kind: "rename"; id: string };

/**
 * The color-tick space row below the NavRail brand: the active space
 * (tick + wash in its color, square + name) as a trigger for the space
 * menu — switch, create, rename, archive.
 */
export function SpaceSwitcher({ collapsed }: { collapsed: boolean }) {
  const session = useSession();
  const { failures } = useSyncErrors();
  const [open, setOpen] = React.useState(false);
  const [mode, setMode] = React.useState<Mode>({ kind: "list" });
  const [showArchived, setShowArchived] = React.useState(false);
  const [rowError, setRowError] = React.useState<string | null>(null);
  // Switch target awaiting the unsaved-changes confirmation: a queued
  // sync failure dies with the per-space store, so leaving must be the
  // user's explicit choice, never a side effect of navigation.
  const [confirmSwitch, setConfirmSwitch] = React.useState<Space | null>(null);
  // Transient confirmation after arriving in a just-created space.
  const [createdNotice, setCreatedNotice] = React.useState(false);

  React.useEffect(() => {
    if (!announceCreatedSpace) return;
    announceCreatedSpace = false;
    setCreatedNotice(true);
  }, []);
  React.useEffect(() => {
    if (!createdNotice) return;
    const t = window.setTimeout(() => setCreatedNotice(false), CREATED_NOTICE_MS);
    return () => window.clearTimeout(t);
  }, [createdNotice]);

  const active =
    session.spaces.find((s) => s.id === session.activeSpaceId) ?? session.spaces[0];
  const live = session.spaces.filter((s) => !s.archivedAt);
  const archived = session.spaces.filter((s) => s.archivedAt);

  const openChange = (next: boolean) => {
    setOpen(next);
    setMode({ kind: "list" });
    setRowError(null);
    setConfirmSwitch(null);
  };

  const guard = (op: Promise<void>) => {
    op.catch((err: unknown) => {
      console.error("donezo: space operation failed", err);
      setRowError(err instanceof Error ? err.message : String(err));
    });
  };

  // On success the store remounts and this popover unmounts with it; on
  // failure it stays open and guard surfaces the error inline.
  const startSwitch = (space: Space) => {
    if (space.id === session.activeSpaceId) {
      setOpen(false);
      return;
    }
    if (failures.length > 0) {
      setConfirmSwitch(space);
      return;
    }
    guard(session.switchSpace(space.id));
  };

  const spaceRow = (space: Space) => {
    if (mode.kind === "rename" && mode.id === space.id) {
      return (
        <li key={space.id}>
          <SpaceForm
            initialName={space.name}
            initialColor={space.color}
            submitLabel="Rename"
            withColor={false}
            onSubmit={async (name) => {
              await session.renameSpace(space.id, name);
              setMode({ kind: "list" });
            }}
            onCancel={() => setMode({ kind: "list" })}
          />
        </li>
      );
    }
    const isActive = space.id === session.activeSpaceId;
    const isArchived = Boolean(space.archivedAt);
    return (
      <li key={space.id} className="flex items-center gap-0.5">
        <button
          type="button"
          onClick={() => startSwitch(space)}
          aria-current={isActive ? "true" : undefined}
          className={cn(
            "flex h-8 min-w-0 flex-1 items-center gap-2 rounded-gtc px-2 text-left outline-none",
            "font-mono text-[0.72rem] uppercase tracking-chrome transition-colors",
            "focus-visible:shadow-gtc-focus",
            isActive
              ? "bg-gtc-tint-accent text-gtc-accent"
              : "text-gtc-text hover:bg-gtc-tint-accent",
            isArchived && "text-gtc-muted"
          )}
        >
          <ProjectMark color={space.color} size={8} muted={isArchived} />
          <span className="min-w-0 flex-1 truncate">{space.name}</span>
          {isActive && <Check className="h-3.5 w-3.5 shrink-0" aria-hidden />}
        </button>
        <RowIconButton label={`Rename ${space.name}`} onClick={() => setMode({ kind: "rename", id: space.id })}>
          <Pencil className="h-3 w-3" aria-hidden />
        </RowIconButton>
        {isArchived ? (
          <RowIconButton
            label={`Unarchive ${space.name}`}
            onClick={() => guard(session.setArchived(space.id, false))}
          >
            <ArchiveRestore className="h-3 w-3" aria-hidden />
          </RowIconButton>
        ) : (
          <RowIconButton
            label={`Archive ${space.name}`}
            onClick={() => guard(session.setArchived(space.id, true))}
          >
            <Archive className="h-3 w-3" aria-hidden />
          </RowIconButton>
        )}
      </li>
    );
  };

  return (
    <Popover open={open} onOpenChange={openChange}>
      <Tip content="Switch or create spaces" side="bottom">
        <PopoverTrigger asChild>
          <button
            type="button"
            aria-label={`Space: ${active?.name ?? "none"}. Switch or create spaces`}
            style={
              active
                ? {
                    background: `color-mix(in srgb, ${projectColorVar(active.color)} 8%, transparent)`,
                  }
                : undefined
            }
            className={cn(
              "relative flex h-[38px] w-full min-w-0 items-center border-b border-gtc-line outline-none transition-colors",
              "font-mono text-[0.72rem] font-medium uppercase tracking-chrome text-gtc-text",
              "hover:text-gtc-accent-bright focus-visible:shadow-gtc-focus",
              collapsed ? "justify-center gap-1 px-0" : "gap-2 px-3.5"
            )}
          >
            {/* Space-color tick — the nav's active-item language, but in
                the space's own color: this row is "where you are". */}
            {active && (
              <span
                aria-hidden
                className="absolute inset-y-[7px] left-0 w-0.5"
                style={{ background: projectColorVar(active.color) }}
              />
            )}
            {active && <ProjectMark color={active.color} size={collapsed ? 10 : 8} />}
            {!collapsed && (
              <span className="min-w-0 flex-1 truncate text-left">{active?.name ?? "donezo"}</span>
            )}
            {/* Persistent chevron — collapsed included — so the row always
                reads as a menu, never static furniture. */}
            <ChevronDown
              className={cn("shrink-0 text-gtc-muted", collapsed ? "h-3 w-3" : "h-3.5 w-3.5")}
              aria-hidden
            />
          </button>
        </PopoverTrigger>
      </Tip>
      {createdNotice && (
        <div
          role="status"
          className={cn(
            "pointer-events-none absolute left-1.5 top-full z-40 mt-1 whitespace-nowrap",
            "rounded-gtc border border-gtc-line bg-gtc-panel px-2 py-1",
            "font-mono text-[0.62rem] uppercase tracking-label text-gtc-accent"
          )}
        >
          space created — you&rsquo;re in it
        </div>
      )}
      <PopoverContent align="start" sideOffset={4} className="w-64 p-2">
        {confirmSwitch ? (
          <div className="space-y-2.5 px-1 py-1">
            <div className="font-mono text-[0.62rem] uppercase tracking-label text-gtc-warn">
              Unsaved change
            </div>
            <p className="m-0 text-[0.8rem] text-gtc-text">
              {failures.length === 1
                ? "A change hasn’t reached the server yet."
                : `${failures.length} changes haven’t reached the server yet.`}{" "}
              Switching to {confirmSwitch.name} discards {failures.length === 1 ? "it" : "them"}.
            </p>
            <div className="flex items-center gap-1.5">
              <Button
                size="sm"
                variant="primary"
                noGlyph
                onClick={() => {
                  const target = confirmSwitch;
                  setConfirmSwitch(null);
                  guard(session.switchSpace(target.id));
                }}
              >
                Switch anyway
              </Button>
              <Button size="sm" variant="ghost" noGlyph onClick={() => setConfirmSwitch(null)}>
                Stay
              </Button>
            </div>
            {rowError && (
              <p className="m-0 font-mono text-[0.66rem] text-gtc-danger" role="alert">
                ▸ {rowError}
              </p>
            )}
          </div>
        ) : (
          <>
            {/* Creation is pinned to the top — the single discoverable
                path to a new space, always in the same place. */}
            {mode.kind === "new" ? (
              <SpaceForm
                initialName=""
                initialColor={COLOR_RAMP[session.spaces.length % COLOR_RAMP.length]}
                submitLabel="Create"
                onSubmit={async (name, color) => {
                  // Flagged before the switch: success remounts the whole
                  // keyed shell, and the fresh switcher shows the notice.
                  announceCreatedSpace = true;
                  try {
                    await session.createSpace(name, color);
                  } catch (err) {
                    announceCreatedSpace = false;
                    throw err;
                  }
                  setOpen(false);
                  setMode({ kind: "list" });
                }}
                onCancel={() => setMode({ kind: "list" })}
              />
            ) : (
              <button
                type="button"
                onClick={() => setMode({ kind: "new" })}
                className={cn(
                  "flex h-8 w-full items-center gap-2 rounded-gtc px-2 outline-none",
                  "font-mono text-[0.72rem] uppercase tracking-chrome text-gtc-text",
                  "transition-colors hover:bg-gtc-tint-accent hover:text-gtc-accent-bright focus-visible:shadow-gtc-focus"
                )}
              >
                <Plus className="h-3.5 w-3.5 text-gtc-accent" aria-hidden />
                New space
              </button>
            )}

            <div className="my-1.5 h-px bg-gtc-line" />

            <div className="px-1 pb-1.5 pt-0.5 font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
              Spaces
            </div>
            <ul className="space-y-0.5">{live.map(spaceRow)}</ul>

            {showArchived && archived.length > 0 && (
              <>
                <div className="px-1 pb-1 pt-2 font-mono text-[0.62rem] uppercase tracking-label text-gtc-muted">
                  Archived
                </div>
                <ul className="space-y-0.5">{archived.map(spaceRow)}</ul>
              </>
            )}

            {rowError && (
              <p className="m-0 px-1 pt-1.5 font-mono text-[0.66rem] text-gtc-danger" role="alert">
                ▸ {rowError}
              </p>
            )}

            {archived.length > 0 && (
              <>
                <div className="my-1.5 h-px bg-gtc-line" />
                <button
                  type="button"
                  onClick={() => setShowArchived((v) => !v)}
                  aria-pressed={showArchived}
                  className={cn(
                    "h-7 rounded-gtc px-2 font-mono text-[0.62rem] uppercase tracking-label outline-none",
                    "transition-colors focus-visible:shadow-gtc-focus",
                    showArchived ? "text-gtc-accent" : "text-gtc-muted hover:text-gtc-text"
                  )}
                >
                  {showArchived ? "Hide archived" : `Show archived (${archived.length})`}
                </button>
              </>
            )}
          </>
        )}
      </PopoverContent>
    </Popover>
  );
}
