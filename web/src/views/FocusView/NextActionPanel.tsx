import { Button } from "@grewelltech/console";

import type { Project } from "@/domain/types";
import { useAppDispatch } from "@/state/AppStore";
import { NextActionFlow } from "@/components/common/NextActionFlow";

/** NEXT ACTION — the single highlighted next step for the current thread.
 *  The lifecycle (done → log → promote, alternates, empty state) is the
 *  shared NextActionFlow; this panel adds the Focus-only navigation. */
export function NextActionPanel({ project }: { project?: Project }) {
  const dispatch = useAppDispatch();
  if (!project) return null;
  return (
    <section>
      <NextActionFlow
        project={project}
        framed
        tourId="next-action"
        extraButtons={
          <>
            <Button
              size="sm"
              onClick={() => dispatch({ type: "OPEN_PROJECT", projectId: project.id })}
            >
              Open project
            </Button>
            <Button
              size="sm"
              onClick={() =>
                dispatch({
                  type: "SET_QUICK_CAPTURE",
                  open: true,
                  preset: { kind: "activity", projectId: project.id },
                })
              }
            >
              Log progress
            </Button>
          </>
        }
      />
    </section>
  );
}
