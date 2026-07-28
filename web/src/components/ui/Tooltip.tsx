import * as React from "react";
import * as TooltipPrimitive from "@radix-ui/react-tooltip";
import { cn } from "@grewelltech/console";

export function TooltipProvider({ children }: { children: React.ReactNode }) {
  return (
    <TooltipPrimitive.Provider delayDuration={300} skipDelayDuration={200}>
      {children}
    </TooltipPrimitive.Provider>
  );
}

export interface TipProps {
  content: React.ReactNode;
  side?: "top" | "bottom" | "left" | "right";
  children: React.ReactNode;
  /** Extra classes for the content bubble. */
  className?: string;
}

/** GTech Console-styled tooltip. Wrap any trigger element. */
export function Tip({ content, side = "top", children, className }: TipProps) {
  return (
    <TooltipPrimitive.Root>
      <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
      <TooltipPrimitive.Portal>
        <TooltipPrimitive.Content
          side={side}
          sideOffset={6}
          className={cn(
            "z-50 max-w-xs rounded-gtc border border-gtc-line bg-gtc-inset px-2.5 py-1.5",
            "text-[0.78rem] text-gtc-text",
            className
          )}
        >
          {content}
        </TooltipPrimitive.Content>
      </TooltipPrimitive.Portal>
    </TooltipPrimitive.Root>
  );
}
