import * as React from "react"
import { Collapsible } from "@base-ui/react/collapsible"
import { ChevronDown } from "lucide-react"

import { cn } from "@workspace/ui/lib/utils"

const CollapsibleRoot = React.forwardRef<
  React.ElementRef<typeof Collapsible.Root>,
  React.ComponentPropsWithoutRef<typeof Collapsible.Root>
>(({ className, ...props }, ref) => (
  <Collapsible.Root ref={ref} className={cn("w-full", className)} {...props} />
))
CollapsibleRoot.displayName = "Collapsible"

const CollapsibleTrigger = React.forwardRef<
  React.ElementRef<typeof Collapsible.Trigger>,
  React.ComponentPropsWithoutRef<typeof Collapsible.Trigger>
>(({ className, children, ...props }, ref) => (
  <Collapsible.Trigger
    ref={ref}
    className={cn(
      "flex w-full items-center justify-between rounded-md px-3 py-2 text-sm font-medium transition-colors hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none [&[data-panel-open]>svg]:rotate-180",
      className
    )}
    {...props}
  >
    {children}
    <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground transition-transform duration-200" />
  </Collapsible.Trigger>
))
CollapsibleTrigger.displayName = "CollapsibleTrigger"

const CollapsibleContent = React.forwardRef<
  React.ElementRef<typeof Collapsible.Panel>,
  React.ComponentPropsWithoutRef<typeof Collapsible.Panel>
>(({ className, ...props }, ref) => (
  <Collapsible.Panel
    ref={ref}
    className={cn(
      "overflow-hidden text-sm transition-all duration-200 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0",
      className
    )}
    {...props}
  />
))
CollapsibleContent.displayName = "CollapsibleContent"

export {
  CollapsibleRoot as Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
}
