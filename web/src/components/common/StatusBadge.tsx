import { Badge, type BadgeStatus } from "@grewelltech/console";
import type { ProjectStatus } from "@/domain/types";

const STATUS_MAP: Record<ProjectStatus, { badge: BadgeStatus; label: string }> = {
  active: { badge: "active", label: "Active" },
  waiting: { badge: "warn", label: "Waiting" },
  blocked: { badge: "danger", label: "Blocked" },
  paused: { badge: "neutral", label: "Paused" },
  completed: { badge: "success", label: "Done" },
  // Calm neutral, not danger — cancelling is a decision, not a failure.
  cancelled: { badge: "neutral", label: "Cancelled" },
};

/** Project status rendered as a GTech Console badge with calm wording. */
export function StatusBadge({ status }: { status: ProjectStatus }) {
  const { badge, label } = STATUS_MAP[status];
  return <Badge status={badge}>{label}</Badge>;
}
