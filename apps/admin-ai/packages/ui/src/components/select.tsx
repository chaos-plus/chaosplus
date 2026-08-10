import * as React from "react"
import { Select } from "@base-ui/react/select"
import { Check, ChevronDown } from "lucide-react"

import { Separator } from "@base-ui/react/separator"

import { cn } from "@workspace/ui/lib/utils"

const SelectRoot = Select.Root
const SelectGroup = Select.Group
const SelectValue = Select.Value

const SelectTrigger = React.forwardRef<
  React.ElementRef<typeof Select.Trigger>,
  React.ComponentPropsWithoutRef<typeof Select.Trigger>
>(({ className, children, ...props }, ref) => (
  <Select.Trigger
    ref={ref}
    className={cn(
      "flex h-9 w-full items-center justify-between rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-sm ring-offset-background focus:ring-2 focus:ring-ring focus:outline-none disabled:cursor-not-allowed disabled:opacity-50 data-[placeholder]:text-muted-foreground",
      className
    )}
    {...props}
  >
    {children}
    <Select.Icon className="text-muted-foreground">
      <ChevronDown className="h-4 w-4 opacity-50" />
    </Select.Icon>
  </Select.Trigger>
))
SelectTrigger.displayName = Select.Trigger.displayName ?? "SelectTrigger"

const SelectContent = React.forwardRef<
  React.ElementRef<typeof Select.Popup>,
  React.ComponentPropsWithoutRef<typeof Select.Popup>
>(({ className, children, ...props }, ref) => (
  <Select.Portal>
    <Select.Positioner
      className="z-50"
      side="bottom"
      align="start"
      sideOffset={4}
      collisionPadding={8}
      alignItemWithTrigger={false}
    >
      <Select.Popup
        ref={ref}
        className={cn(
          "relative z-50 max-h-[min(24rem,var(--available-height))] min-w-[var(--anchor-width)] overflow-y-auto rounded-md border bg-popover text-popover-foreground shadow-md transition-opacity duration-200 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0",
          className
        )}
        {...props}
      >
        <Select.ScrollUpArrow className="flex justify-center py-1 text-muted-foreground">
          <ChevronDown className="h-4 w-4 rotate-180" />
        </Select.ScrollUpArrow>
        <div className="p-1">{children}</div>
        <Select.ScrollDownArrow className="flex justify-center py-1 text-muted-foreground">
          <ChevronDown className="h-4 w-4" />
        </Select.ScrollDownArrow>
      </Select.Popup>
    </Select.Positioner>
  </Select.Portal>
))
SelectContent.displayName = Select.Popup.displayName ?? "SelectContent"

const SelectLabel = React.forwardRef<
  React.ElementRef<typeof Select.Label>,
  React.ComponentPropsWithoutRef<typeof Select.Label>
>(({ className, ...props }, ref) => (
  <Select.Label
    ref={ref}
    className={cn("py-1.5 pr-2 pl-8 text-sm font-semibold", className)}
    {...props}
  />
))
SelectLabel.displayName = Select.Label.displayName ?? "SelectLabel"

const SelectItem = React.forwardRef<
  React.ElementRef<typeof Select.Item>,
  React.ComponentPropsWithoutRef<typeof Select.Item>
>(({ className, children, ...props }, ref) => (
  <Select.Item
    ref={ref}
    className={cn(
      "relative flex w-full cursor-pointer items-center rounded-sm py-1.5 pr-2 pl-8 text-sm outline-none select-none focus:bg-accent focus:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
      className
    )}
    {...props}
  >
    <span className="absolute left-2 flex h-3.5 w-3.5 items-center justify-center">
      <Select.ItemIndicator>
        <Check className="h-4 w-4" />
      </Select.ItemIndicator>
    </span>
    <Select.ItemText>{children}</Select.ItemText>
  </Select.Item>
))
SelectItem.displayName = Select.Item.displayName ?? "SelectItem"

const SelectSeparator = React.forwardRef<
  React.ElementRef<typeof Separator>,
  React.ComponentPropsWithoutRef<typeof Separator>
>(({ className, ...props }, ref) => (
  <Select.Separator
    ref={ref}
    className={cn("-mx-1 my-1 h-px bg-muted", className)}
    {...props}
  />
))
SelectSeparator.displayName = Select.Separator.displayName ?? "SelectSeparator"

export interface SimpleSelectOption {
  label: React.ReactNode
  value: string
}

function normalizeSelectOptions(
  options: SimpleSelectOption[] | Record<string, React.ReactNode>
): SimpleSelectOption[] {
  if (Array.isArray(options)) return options
  return Object.entries(options).map(([value, label]) => ({ value, label }))
}

export interface SimpleSelectProps {
  options: SimpleSelectOption[] | Record<string, React.ReactNode>
  value?: string
  onValueChange: (value: string) => void
  placeholder?: React.ReactNode
  content?: React.ReactNode
  disabled?: boolean
  className?: string
  triggerClassName?: string
  id?: string
  "aria-labelledby"?: string
  "aria-describedby"?: string
}

/**
 * SimpleSelect is the single source of truth for option dropdowns. It always feeds base-ui
 * the `items` map, so the selected value resolves to its LABEL in the trigger (not the raw
 * value/key). Use this everywhere instead of composing Select/SelectTrigger/SelectItem by
 * hand — that avoids the "trigger shows the raw value" translation bug per dropdown.
 */
export function SimpleSelect({
  options,
  value,
  onValueChange,
  placeholder,
  content,
  disabled,
  className,
  triggerClassName,
  id,
  "aria-labelledby": ariaLabelledby,
  "aria-describedby": ariaDescribedby,
}: SimpleSelectProps) {
  const items = normalizeSelectOptions(options)
  return (
    <SelectRoot
      items={items as never}
      value={value ?? ""}
      onValueChange={(v: string | null) => onValueChange(v ?? "")}
      disabled={disabled}
    >
      <SelectTrigger
        id={id}
        className={cn(className, triggerClassName)}
        aria-labelledby={ariaLabelledby}
        aria-describedby={ariaDescribedby}
      >
        <SelectValue placeholder={placeholder as React.ReactNode} />
      </SelectTrigger>
      <SelectContent>
        {content ??
          items.map((o) => (
            <SelectItem key={o.value} value={o.value}>
              {o.label}
            </SelectItem>
          ))}
      </SelectContent>
    </SelectRoot>
  )
}

export {
  SelectRoot as Select,
  SelectGroup,
  SelectValue,
  SelectTrigger,
  SelectContent,
  SelectLabel,
  SelectItem,
  SelectSeparator,
}
