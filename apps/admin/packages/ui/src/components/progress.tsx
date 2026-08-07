import * as React from "react"
import { Progress } from "@base-ui/react/progress"

import { cn } from "@workspace/ui/lib/utils"

interface ProgressProps extends React.ComponentPropsWithoutRef<
  typeof Progress.Root
> {
  className?: string
}

const ProgressRoot = React.forwardRef<
  React.ElementRef<typeof Progress.Root>,
  ProgressProps
>(({ className, value, ...props }, ref) => (
  <Progress.Root
    ref={ref}
    value={value}
    data-slot="progress"
    className={cn("relative w-full", className)}
    {...props}
  >
    <Progress.Track className="relative h-2 w-full overflow-hidden rounded-full bg-secondary">
      <Progress.Indicator className="h-full bg-primary transition-all duration-300 ease-in-out" />
    </Progress.Track>
  </Progress.Root>
))
ProgressRoot.displayName = "Progress"

export { ProgressRoot as Progress }
