import * as React from "react"
import { Tooltip } from "@base-ui/react/tooltip"

import { cn } from "@workspace/ui/lib/utils"

const TooltipProvider = Tooltip.Provider
const TooltipRoot = Tooltip.Root
const TooltipTrigger = Tooltip.Trigger

const TooltipContent = React.forwardRef<
  React.ElementRef<typeof Tooltip.Popup>,
  React.ComponentPropsWithoutRef<typeof Tooltip.Popup>
>(({ className, ...props }, ref) => (
  <Tooltip.Portal>
    <Tooltip.Positioner sideOffset={4}>
      <Tooltip.Popup
        ref={ref}
        className={cn(
          "z-50 overflow-hidden rounded-md border bg-popover px-3 py-1.5 text-sm text-popover-foreground shadow-md",
          className
        )}
        {...props}
      />
    </Tooltip.Positioner>
  </Tooltip.Portal>
))
TooltipContent.displayName = Tooltip.Popup.displayName ?? "TooltipContent"

export {
  TooltipRoot as Tooltip,
  TooltipTrigger,
  TooltipContent,
  TooltipProvider,
}
