import * as React from "react";
import { GripVertical } from "lucide-react";
import {
  Button,
  cn,
  SectionLabel,
  Table,
  TableBody,
  TableCell,
  TableHeadCell,
  TableHeader,
  TableRow,
} from "@grewelltech/console";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

import type { Project } from "@/domain/types";
import { useAppDispatch, useAppState, type AppState } from "@/state/AppStore";
import {
  compareProjectOrder,
  isClosedProject,
  latestActivityDate,
  openTaskCount,
} from "@/state/selectors";
import { relativeFromToday } from "@/lib/time";
import { ProjectMark } from "@/components/common/ProjectMark";
import { StatusBadge } from "@/components/common/StatusBadge";
import { UnfiledNotes } from "./UnfiledNotes";
import { UnfiledTasks } from "./UnfiledTasks";

const rowClass = "transition-colors hover:bg-gtc-tint-accent/50 cursor-pointer";

/** The row content after the drag-handle slot — shared by draggable and static
 *  rows so both read identically. `handle` fills the leading grip slot. */
function projectCells(
  p: Project,
  state: AppState,
  open: (id: string) => void,
  handle: React.ReactNode
) {
  const closed = isClosedProject(p);
  const latest = latestActivityDate(state, p.id);
  const openTasks = openTaskCount(state, p.id);
  return (
    <>
      <TableCell>
        <div className="flex items-center gap-1.5">
          {handle}
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              open(p.id);
            }}
            className="flex items-center gap-2.5 rounded-gtc text-left outline-none focus-visible:shadow-gtc-focus"
          >
            <ProjectMark color={p.color} size={8} muted={closed} />
            <span className="font-sans text-[0.9rem] font-medium text-gtc-text">{p.name}</span>
          </button>
        </div>
      </TableCell>
      <TableCell>
        <StatusBadge status={p.status} />
      </TableCell>
      <TableCell>
        <span className="block max-w-[32ch] truncate font-sans text-[0.85rem] text-gtc-text">
          {p.currentFocus}
        </span>
      </TableCell>
      <TableCell mono className="text-[0.75rem] text-gtc-muted">
        {latest ? relativeFromToday(latest) : "—"}
      </TableCell>
      <TableCell mono className="text-right">
        {openTasks > 0 ? openTasks : <span className="text-gtc-muted">—</span>}
      </TableCell>
    </>
  );
}

/** A draggable project row — a raw <tr> so dnd-kit can hold its ref (the
 *  console TableRow forwards attributes but not ref). */
function SortableProjectRow({
  p,
  state,
  open,
}: {
  p: Project;
  state: AppState;
  open: (id: string) => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: p.id,
  });
  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    ...(isDragging ? { opacity: 0.6, position: "relative", zIndex: 1 } : {}),
  };
  const handle = (
    <button
      type="button"
      {...listeners}
      {...attributes}
      onClick={(e) => e.stopPropagation()}
      aria-label={`Reorder ${p.name}`}
      className="flex h-6 w-5 shrink-0 cursor-grab items-center justify-center rounded-gtc text-gtc-muted outline-none hover:text-gtc-text focus-visible:shadow-gtc-focus active:cursor-grabbing"
    >
      <GripVertical className="h-4 w-4" aria-hidden />
    </button>
  );
  return (
    <tr
      ref={setNodeRef}
      style={style}
      onClick={() => open(p.id)}
      className={cn(rowClass, isDragging && "bg-gtc-panel shadow-gtc-glow")}
    >
      {projectCells(p, state, open, handle)}
    </tr>
  );
}

/** A non-draggable row (closed projects and the catch-all) — same layout, an
 *  inert grip slot so the columns still line up. */
function StaticProjectRow({
  p,
  state,
  open,
}: {
  p: Project;
  state: AppState;
  open: (id: string) => void;
}) {
  const handle = <span className="h-6 w-5 shrink-0" aria-hidden />;
  return (
    <TableRow
      onClick={() => open(p.id)}
      className={cn(rowClass, isClosedProject(p) && "opacity-60")}
    >
      {projectCells(p, state, open, handle)}
    </TableRow>
  );
}

/** Master list of all projects: active ones reorder by drag, closed ones dim
 *  at the bottom, and the catch-all is pinned last. */
export function ProjectList() {
  const state = useAppState();
  const dispatch = useAppDispatch();

  // Display order: catch-all last, then closed last, then manual position —
  // the same comparator the timeline rail uses, so a drag reorders both.
  const ordered = React.useMemo(
    () => [...state.projects].sort(compareProjectOrder),
    [state.projects]
  );
  const draggable = ordered.filter((p) => !p.catchall && !isClosedProject(p));
  const pinned = ordered.filter((p) => p.catchall || isClosedProject(p));

  const open = (projectId: string) => dispatch({ type: "OPEN_PROJECT", projectId });

  const sensors = useSensors(
    // A small drag threshold so clicking a row to open it never starts a drag.
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates })
  );

  const onDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const oldIndex = draggable.findIndex((p) => p.id === active.id);
    const newIndex = draggable.findIndex((p) => p.id === over.id);
    if (oldIndex < 0 || newIndex < 0) return;
    // The active set in its new order, then closed (kept after it). One atomic
    // reorder renumbers them server-side to their indices — not N independent
    // PATCHes, so a partial failure can't leave the list half-reordered. The
    // catch-all is untouched and stays pinned last.
    const reordered = arrayMove(draggable, oldIndex, newIndex);
    const closed = ordered.filter((p) => !p.catchall && isClosedProject(p));
    dispatch({
      type: "REORDER_PROJECTS",
      order: [...reordered, ...closed].map((p) => p.id),
    });
  };

  return (
    <div className="mx-auto max-w-[1000px] px-4 py-6 sm:px-6 lg:px-8">
      <div className="mb-3 flex items-center gap-3">
        <SectionLabel
          className="mb-0 mt-0 flex-1"
          trailing={<span className="text-gtc-text">{ordered.length}</span>}
        >
          Projects
        </SectionLabel>
        <Button
          variant="ghost"
          size="sm"
          noGlyph
          onClick={() =>
            dispatch({ type: "SET_QUICK_CAPTURE", open: true, preset: { kind: "project" } })
          }
        >
          + New project
        </Button>
      </div>
      <p className="mb-5 max-w-[70ch] font-sans text-[0.85rem] text-gtc-muted">
        Every stream of work. Open one to resume where you left off — drag the handle to reorder.
      </p>

      {/* min-w keeps columns readable on phones — the Table's own
          overflow-x-auto wrapper scrolls instead of the page. */}
      <Table className="min-w-[640px]">
        <TableHeader>
          <TableRow>
            <TableHeadCell>Project</TableHeadCell>
            <TableHeadCell>Status</TableHeadCell>
            <TableHeadCell>Current focus</TableHeadCell>
            <TableHeadCell>Last activity</TableHeadCell>
            <TableHeadCell className="text-right">Open</TableHeadCell>
          </TableRow>
        </TableHeader>
        <TableBody>
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragEnd={onDragEnd}
            // dnd-kit's screen-reader live region is a <div>; render it on
            // <body> instead of inside <tbody>, which only permits <tr>.
            accessibility={{ container: document.body }}
          >
            <SortableContext
              items={draggable.map((p) => p.id)}
              strategy={verticalListSortingStrategy}
            >
              {draggable.map((p) => (
                <SortableProjectRow key={p.id} p={p} state={state} open={open} />
              ))}
            </SortableContext>
          </DndContext>
          {pinned.map((p) => (
            <StaticProjectRow key={p.id} p={p} state={state} open={open} />
          ))}
        </TableBody>
      </Table>

      {/* Tasks and notes with no project have nowhere else in the app to
          render, so they live alongside the project list rather than inside
          one — the reachability rule from #25, tasks included. */}
      <UnfiledTasks />
      <UnfiledNotes />
    </div>
  );
}
