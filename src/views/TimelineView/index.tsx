import { EmptyState } from "@/components/common/EmptyState";

/** Project Pulse — the signature project-by-time visualization. */
export default function TimelineView() {
  return (
    <div className="flex h-full items-center justify-center p-8">
      <EmptyState title="Timeline // under construction" className="w-96" />
    </div>
  );
}
