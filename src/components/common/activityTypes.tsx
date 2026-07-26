import {
  FlaskConical,
  Flag,
  OctagonAlert,
  Scale,
  Users,
  Wrench,
  type LucideIcon,
} from "lucide-react";
import type { ActivityType } from "@/domain/types";

export interface ActivityTypeMeta {
  label: string;
  Icon: LucideIcon;
  /** Emphasis treatment on the timeline: blockers warn, milestones stand out. */
  emphasis: "normal" | "danger" | "milestone";
}

export const ACTIVITY_TYPES: Record<ActivityType, ActivityTypeMeta> = {
  work: { label: "Work", Icon: Wrench, emphasis: "normal" },
  research: { label: "Research", Icon: FlaskConical, emphasis: "normal" },
  meeting: { label: "Meeting", Icon: Users, emphasis: "normal" },
  decision: { label: "Decision", Icon: Scale, emphasis: "normal" },
  blocker: { label: "Blocker", Icon: OctagonAlert, emphasis: "danger" },
  milestone: { label: "Milestone", Icon: Flag, emphasis: "milestone" },
};

export const ACTIVITY_TYPE_IDS = Object.keys(ACTIVITY_TYPES) as ActivityType[];

export function ActivityTypeIcon({
  type,
  className,
}: {
  type: ActivityType;
  className?: string;
}) {
  const { Icon } = ACTIVITY_TYPES[type];
  return <Icon className={className ?? "h-3.5 w-3.5"} aria-hidden />;
}
