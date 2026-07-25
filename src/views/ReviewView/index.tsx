import { EmptyState } from "@/components/common/EmptyState";

/** Gentle resurfacing: stale items, still-relevant checks, reschedules. */
export default function ReviewView() {
  return (
    <div className="flex h-full items-center justify-center p-8">
      <EmptyState title="Review // under construction" className="w-96" />
    </div>
  );
}
