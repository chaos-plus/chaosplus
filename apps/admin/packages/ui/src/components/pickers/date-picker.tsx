import { CalendarIcon } from "lucide-react"
import { useLocale } from "use-intl"
import { useState } from "react"

import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@workspace/ui/components/popover"
import { cn } from "@workspace/ui/lib/utils"

import { Calendar } from "./calendar"

// Match the text Input / Select trigger styling so pickers look consistent in filter bars and forms.
const TRIGGER_CLASS =
  "flex h-9 w-[240px] items-center justify-start rounded-md border border-input bg-transparent px-3 py-2 text-left text-sm font-normal shadow-sm ring-offset-background transition-colors hover:bg-accent focus:outline-none focus:ring-2 focus:ring-ring disabled:cursor-not-allowed disabled:opacity-50"

export interface DatePickerProps {
  value?: Date | null
  onChange?: (date: Date | null) => void
  /** Shown when no date is selected — pass an i18n string. */
  placeholder?: string
  disabled?: boolean
  fromDate?: Date
  toDate?: Date
  /** Override date formatting. Defaults to `toLocaleDateString()`. */
  formatValue?: (date: Date) => string
  className?: string
  /** Override the locale code (defaults to next-intl current locale). */
  localeCode?: string
}

export function DatePicker({
  value,
  onChange,
  placeholder = "Select date",
  disabled = false,
  fromDate,
  toDate,
  formatValue,
  className,
  localeCode,
}: DatePickerProps) {
  const currentLocale = useLocale()
  const [open, setOpen] = useState(false)

  const displayValue = value
    ? formatValue
      ? formatValue(value)
      : value.toLocaleDateString(currentLocale)
    : null

  function handleSelect(date: Date | undefined) {
    onChange?.(date ?? null)
    setOpen(false)
  }

  return (
    <Popover open={open} onOpenChange={(next) => !disabled && setOpen(next)}>
      <PopoverTrigger
        disabled={disabled}
        className={cn(
          TRIGGER_CLASS,
          !displayValue && "text-muted-foreground",
          className
        )}
      >
        <CalendarIcon className="mr-2 size-4 shrink-0 opacity-50" />
        {displayValue ?? placeholder}
      </PopoverTrigger>

      <PopoverContent className="z-[100] w-auto rounded-xl p-3 shadow-lg">
        <Calendar
          selected={value ?? undefined}
          onSelect={handleSelect}
          fromDate={fromDate}
          toDate={toDate}
          localeCode={localeCode}
        />
      </PopoverContent>
    </Popover>
  )
}
