import * as React from "react"
import { Checkbox } from "@base-ui/react/checkbox"
import { Check, Minus } from "lucide-react"

import { cn } from "@workspace/ui/lib/utils"

const CheckboxRoot = React.forwardRef<
  React.ElementRef<typeof Checkbox.Root>,
  React.ComponentPropsWithoutRef<typeof Checkbox.Root>
>(({ className, indeterminate, ...props }, ref) => (
  <Checkbox.Root
    ref={ref}
    indeterminate={indeterminate}
    className={cn(
      "peer h-4 w-4 shrink-0 rounded-sm border border-primary ring-offset-background focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50 data-indeterminate:bg-primary data-indeterminate:text-primary-foreground data-checked:bg-primary data-checked:text-primary-foreground",
      className
    )}
    {...props}
  >
    <Checkbox.Indicator className="flex items-center justify-center text-current">
      {indeterminate ? (
        <Minus className="h-3.5 w-3.5" />
      ) : (
        <Check className="h-3.5 w-3.5" />
      )}
    </Checkbox.Indicator>
  </Checkbox.Root>
))
CheckboxRoot.displayName = Checkbox.Root.displayName ?? "Checkbox"

export { CheckboxRoot as Checkbox }
