import type { ViewId } from "@/domain/types";
import { CAPTURE_KEY_LABEL } from "@/lib/platform";

/** Preferred coachmark position relative to the target (flips to fit). */
export type TourPlacement = "below" | "above" | "right" | "left";

export interface TourStep {
  id: string;
  /** View the target lives in; "any" keeps whatever view is active. */
  view: ViewId | "any";
  /** data-tour attribute value of the highlighted element. */
  target: string;
  title: string;
  body: string;
  placement: TourPlacement;
}

/** The seven-stop product tour, in order. */
export const TOUR_STEPS: TourStep[] = [
  {
    id: "pulse",
    view: "timeline",
    target: "pulse",
    title: "This is Project Pulse",
    body: "Projects run down, time runs across. Each capsule is something that actually happened — gaps are just gaps, not guilt.",
    placement: "below",
  },
  {
    id: "zoom",
    view: "timeline",
    target: "zoom",
    title: "Zoom changes meaning",
    body: "Day shows individual entries, Week rolls them up, Month shows density. Same history, different altitude.",
    placement: "below",
  },
  {
    id: "rail",
    view: "timeline",
    target: "rail",
    title: "Projects at a glance",
    body: "Status, current focus, and how long since you touched it. Click through for the full picture and a resume-here note.",
    placement: "right",
  },
  {
    id: "log",
    view: "timeline",
    target: "pulse",
    title: "Log as it happens",
    body: "Click any empty cell to record what you did — the date and project are already filled in.",
    placement: "below",
  },
  {
    id: "capture",
    view: "any",
    target: "capture",
    title: "Capture from anywhere",
    body: `${CAPTURE_KEY_LABEL} opens capture over any screen. Type the thought; file it now or let it land in the Inbox.`,
    placement: "below",
  },
  {
    id: "inbox",
    view: "inbox",
    target: "inbox",
    title: "Nothing gets lost",
    body: "Raw captures wait here. Classify them when it's cheap — or don't. The inbox is a buffer, not a backlog.",
    placement: "right",
  },
  {
    id: "next-action",
    view: "focus",
    target: "next-action",
    title: "Start your day here",
    body: "One highlighted next action, what's time-sensitive, and what you were doing before the interruption.",
    placement: "below",
  },
];
