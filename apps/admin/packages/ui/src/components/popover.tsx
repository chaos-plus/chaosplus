import * as React from "react"
import { Popover } from "@base-ui/react/popover"

import { cn } from "@workspace/ui/lib/utils"

const PopoverRoot = Popover.Root
const PopoverTrigger = Popover.Trigger

const PopoverContent = React.forwardRef<
  React.ElementRef<typeof Popover.Popup>,
  React.ComponentPropsWithoutRef<typeof Popover.Popup>
>(({ className, ...props }, ref) => (
  <Popover.Portal>
    <Popover.Positioner align="center" sideOffset={4} className="z-50">
      <Popover.Popup
        ref={ref}
        className={cn(
          "z-50 w-72 rounded-md border bg-popover p-4 text-popover-foreground shadow-md transition-opacity duration-200 outline-none data-[ending-style]:opacity-0 data-[starting-style]:opacity-0",
          className
        )}
        {...props}
      />
    </Popover.Positioner>
  </Popover.Portal>
))
PopoverContent.displayName = Popover.Popup.displayName ?? "PopoverContent"

export { PopoverRoot as Popover, PopoverTrigger, PopoverContent }
