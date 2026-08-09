import { AppShell } from "@/components/shell/AppShell";
import { useAppState } from "@/state/AppStore";
import type { ViewId } from "@/domain/types";

import TimelineView from "@/views/TimelineView";
import FocusView from "@/views/FocusView";
import InboxView from "@/views/InboxView";
import ProjectsView from "@/views/ProjectsView";
import ReviewView from "@/views/ReviewView";
import SearchView from "@/views/SearchView";
import TrashView from "@/views/TrashView";

const VIEWS: Record<ViewId, () => JSX.Element> = {
  focus: FocusView,
  timeline: TimelineView,
  inbox: InboxView,
  projects: ProjectsView,
  review: ReviewView,
  search: SearchView,
  trash: TrashView,
};

export default function App() {
  const { view } = useAppState();
  const View = VIEWS[view];
  return (
    <AppShell>
      <View />
    </AppShell>
  );
}
