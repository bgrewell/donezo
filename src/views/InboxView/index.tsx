import { EmptyState } from "@/components/common/EmptyState";

/** Raw captures awaiting classification. */
export default function InboxView() {
  return (
    <div className="flex h-full items-center justify-center p-8">
      <EmptyState title="Inbox // under construction" className="w-96" />
    </div>
  );
}
