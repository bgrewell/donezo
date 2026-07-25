import { EmptyState } from "@/components/common/EmptyState";

/** Daily workflow: what deserves attention right now. */
export default function FocusView() {
  return (
    <div className="flex h-full items-center justify-center p-8">
      <EmptyState title="Focus // under construction" className="w-96" />
    </div>
  );
}
