import * as React from "react";

/** Re-asserts focus into the GTC Dialog panel that contains the returned
 *  ref's node shortly after it opens. The Dialog focuses its panel once in
 *  its open effect, but a Radix menu that opened it refocuses its trigger
 *  asynchronously just after closing — leaving focus outside the modal so
 *  the next Enter reopens the menu on top of it. Mirrors the tour card's
 *  120ms re-assert. Skips whenever focus already sits inside any open
 *  dialog, so a layer stacked above is never robbed. */
export function useDialogFocusReassert(open: boolean): React.RefObject<HTMLDivElement> {
  const ref = React.useRef<HTMLDivElement>(null);
  React.useEffect(() => {
    if (!open) return;
    const reassert = () => {
      const panel = ref.current?.closest<HTMLElement>('[role="dialog"]');
      if (!panel) return;
      const active = document.activeElement;
      if (active instanceof HTMLElement && active.closest('[role="dialog"]')) return;
      panel.focus();
    };
    const t = window.setTimeout(reassert, 120);
    return () => window.clearTimeout(t);
  }, [open]);
  return ref;
}
