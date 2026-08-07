import * as React from "react"
import { RadioGroup as RadioGroupPrimitive } from "@base-ui/react/radio-group"
import { Radio } from "@base-ui/react/radio"

import { cn } from "@workspace/ui/lib/utils"

const RadioGroup = React.forwardRef<
  React.ElementRef<typeof RadioGroupPrimitive>,
  React.ComponentPropsWithoutRef<typeof RadioGroupPrimitive>
>(({ className, ...props }, ref) => (
  <RadioGroupPrimitive
    ref={ref}
    data-slot="radio-group"
    className={cn("grid gap-2", className)}
    {...props}
  />
))
RadioGroup.displayName = "RadioGroup"

const RadioGroupItem = React.forwardRef<
  React.ElementRef<typeof Radio.Root>,
  React.ComponentPropsWithoutRef<typeof Radio.Root>
>(({ className, ...props }, ref) => (
  <Radio.Root
    ref={ref}
    data-slot="radio-group-item"
    className={cn(
      "flex aspect-square h-4 w-4 items-center justify-center rounded-full border border-primary text-primary ring-offset-background focus:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50",
      className
    )}
    {...props}
  >
    <Radio.Indicator className="h-2.5 w-2.5 rounded-full bg-primary data-[unchecked]:hidden" />
  </Radio.Root>
))
RadioGroupItem.displayName = "RadioGroupItem"

const RadioGroupOption = React.forwardRef<
  React.ElementRef<typeof Radio.Root>,
  React.ComponentPropsWithoutRef<typeof Radio.Root> & {
    label: React.ReactNode
    description?: React.ReactNode
    wrapperClassName?: string
  }
>(
  (
    { className, wrapperClassName, label, description, disabled, ...props },
    ref
  ) => (
    <label
      className={cn(
        "flex cursor-pointer items-center gap-2 text-sm",
        disabled && "cursor-not-allowed opacity-50",
        wrapperClassName
      )}
    >
      <RadioGroupItem
        ref={ref}
        className={className}
        disabled={disabled}
        {...props}
      />
      <span className="grid gap-0.5">
        <span>{label}</span>
        {description ? (
          <span className="text-xs text-muted-foreground">{description}</span>
        ) : null}
      </span>
    </label>
  )
)
RadioGroupOption.displayName = "RadioGroupOption"

export { RadioGroup, RadioGroupItem, RadioGroupOption }
