import * as React from "react";
import * as MenuPrimitive from "@radix-ui/react-dropdown-menu";
import { Check } from "lucide-react";
import { cn } from "@grewelltech/console";

export const DropdownMenu = MenuPrimitive.Root;
export const DropdownMenuTrigger = MenuPrimitive.Trigger;

export const DropdownMenuContent = React.forwardRef<
  React.ElementRef<typeof MenuPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof MenuPrimitive.Content>
>(function DropdownMenuContent({ className, sideOffset = 6, ...props }, ref) {
  return (
    <MenuPrimitive.Portal>
      <MenuPrimitive.Content
        ref={ref}
        sideOffset={sideOffset}
        className={cn(
          "z-50 min-w-[190px] rounded-gtc border border-gtc-line bg-gtc-panel bg-gtc-sheen py-1",
          className
        )}
        {...props}
      />
    </MenuPrimitive.Portal>
  );
});

export const DropdownMenuItem = React.forwardRef<
  React.ElementRef<typeof MenuPrimitive.Item>,
  React.ComponentPropsWithoutRef<typeof MenuPrimitive.Item>
>(function DropdownMenuItem({ className, ...props }, ref) {
  return (
    <MenuPrimitive.Item
      ref={ref}
      className={cn(
        "flex cursor-pointer select-none items-center gap-2 px-3 py-1.5 outline-none",
        "font-mono text-[0.72rem] uppercase tracking-chrome text-gtc-text",
        "data-[highlighted]:bg-gtc-tint-accent data-[highlighted]:text-gtc-accent-bright",
        "data-[disabled]:cursor-not-allowed data-[disabled]:opacity-50",
        className
      )}
      {...props}
    />
  );
});

export const DropdownMenuCheckboxItem = React.forwardRef<
  React.ElementRef<typeof MenuPrimitive.CheckboxItem>,
  React.ComponentPropsWithoutRef<typeof MenuPrimitive.CheckboxItem>
>(function DropdownMenuCheckboxItem({ className, children, ...props }, ref) {
  return (
    <MenuPrimitive.CheckboxItem
      ref={ref}
      className={cn(
        "flex cursor-pointer select-none items-center gap-2 px-3 py-1.5 outline-none",
        "font-mono text-[0.72rem] uppercase tracking-chrome text-gtc-text",
        "data-[highlighted]:bg-gtc-tint-accent data-[highlighted]:text-gtc-accent-bright",
        className
      )}
      {...props}
    >
      <span className="flex h-3 w-3 items-center justify-center border border-gtc-line">
        <MenuPrimitive.ItemIndicator>
          <Check className="h-2.5 w-2.5 text-gtc-accent" strokeWidth={3} />
        </MenuPrimitive.ItemIndicator>
      </span>
      {children}
    </MenuPrimitive.CheckboxItem>
  );
});

export function DropdownMenuLabel({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "px-3 pb-1 pt-2 font-mono text-[0.66rem] uppercase tracking-label text-gtc-muted",
        className
      )}
      {...props}
    />
  );
}

export function DropdownMenuSeparator({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("my-1 h-px bg-gtc-line", className)} {...props} />;
}
