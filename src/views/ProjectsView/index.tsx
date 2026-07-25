import { EmptyState } from "@/components/common/EmptyState";

/** Project list + reusable project-detail screen. */
export default function ProjectsView() {
  return (
    <div className="flex h-full items-center justify-center p-8">
      <EmptyState title="Projects // under construction" className="w-96" />
    </div>
  );
}
