import {
  cn,
  SectionLabel,
  Table,
  TableBody,
  TableCell,
  TableHeadCell,
  TableHeader,
  TableRow,
} from "@grewelltech/console";

import { useAppDispatch, useAppState } from "@/state/AppStore";
import { latestActivityDate, openTaskCount } from "@/state/selectors";
import { relativeFromToday } from "@/lib/time";
import { ProjectMark } from "@/components/common/ProjectMark";
import { StatusBadge } from "@/components/common/StatusBadge";

/** Master list of all projects, completed ones dimmed at the bottom. */
export function ProjectList() {
  const state = useAppState();
  const dispatch = useAppDispatch();

  const projects = [...state.projects].sort(
    (a, b) => Number(a.status === "completed") - Number(b.status === "completed")
  );

  const open = (projectId: string) => dispatch({ type: "OPEN_PROJECT", projectId });

  return (
    <div className="mx-auto max-w-[1000px] px-8 py-6">
      <SectionLabel
        className="mb-3 mt-0"
        trailing={<span className="text-gtc-text">{projects.length}</span>}
      >
        Projects
      </SectionLabel>
      <p className="mb-5 max-w-[70ch] font-sans text-[0.85rem] text-gtc-muted">
        Every stream of work. Open one to resume where you left off.
      </p>

      <Table>
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
          {projects.map((p) => {
            const completed = p.status === "completed";
            const latest = latestActivityDate(state, p.id);
            const openTasks = openTaskCount(state, p.id);
            return (
              <TableRow
                key={p.id}
                onClick={() => open(p.id)}
                className={cn("cursor-pointer", completed && "opacity-60")}
              >
                <TableCell>
                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      open(p.id);
                    }}
                    className="flex items-center gap-2.5 rounded-gtc text-left outline-none focus-visible:shadow-gtc-focus"
                  >
                    <ProjectMark color={p.color} size={8} muted={completed} />
                    <span className="font-sans text-[0.9rem] font-medium text-gtc-text">
                      {p.name}
                    </span>
                  </button>
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
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}
