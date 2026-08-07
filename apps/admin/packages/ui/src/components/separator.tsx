import * as React from "react"
import { Separator } from "@base-ui/react/separator"

import { cn } from "@workspace/ui/lib/utils"

const SeparatorRoot = React.forwardRef<
  React.ElementRef<typeof Separator>,
  React.ComponentPropsWithoutRef<typeof Separator>
>(({ className, ...props }, ref) => (
  <Separator
    ref={ref}
    className={cn(
      "shrink-0 bg-border",
      props.orientation === "vertical" ? "h-full w-[1px]" : "h-[1px] w-full",
      className
    )}
    {...props}
  />
))
SeparatorRoot.displayName = "Separator"

export { SeparatorRoot as Separator }
