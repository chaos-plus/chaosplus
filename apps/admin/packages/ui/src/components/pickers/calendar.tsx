import { ChevronDown, ChevronLeft, ChevronRight, ChevronUp } from "lucide-react"
import { useLocale } from "use-intl"
import { DayPicker } from "react-day-picker"
import {
  ar,
  de,
  enGB,
  enUS,
  es,
  faIR,
  fr,
  he,
  hi,
  id,
  it,
  ja,
  ko,
  ms,
  pl,
  pt,
  ru,
  th,
  tr,
  vi,
  zhCN,
  zhTW,
} from "react-day-picker/locale"
import type { Locale } from "react-day-picker/locale"
import type { Matcher } from "react-day-picker"

import { cn } from "@workspace/ui/lib/utils"

// Shared bare calendar (DayPicker + styling + locale). DatePicker and DateTimePicker
// both render this inside their own popover — one calendar, no duplicated config.

const LOCALE_MAP: Record<string, Locale> = {
  ar,
  de,
  fr,
  he,
  hi,
  id,
  it,
  ja,
  ko,
  ms,
  pl,
  ru,
  th,
  tr,
  vi,
  es,
  pt,
  fa: faIR,
  "en-GB": enGB,
  "en-US": enUS,
  "zh-CN": zhCN,
  "zh-TW": zhTW,
}

function resolveDateLocale(localeCode: string): Locale {
  return (
    LOCALE_MAP[localeCode] ?? LOCALE_MAP[localeCode.split("-")[0] ?? ""] ?? enUS
  )
}

const DAY_PICKER_CLASS_NAMES = {
  root: "p-1",
  months: "relative flex flex-col gap-4",
  month: "flex flex-col gap-3",
  month_caption: "relative z-0 flex h-9 items-center justify-center px-9",
  caption_label: "text-sm font-semibold",
  nav: "pointer-events-none absolute inset-x-0 top-0 z-10 flex h-9 items-center justify-between px-0.5",
  button_previous:
    "pointer-events-auto inline-flex size-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-30 aria-disabled:pointer-events-none aria-disabled:opacity-30",
  button_next:
    "pointer-events-auto inline-flex size-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-30 aria-disabled:pointer-events-none aria-disabled:opacity-30",
  chevron: "size-4",
  month_grid: "w-full border-collapse",
  weekdays: "flex",
  weekday:
    "w-9 pb-1 text-[0.7rem] font-medium text-muted-foreground/70 uppercase",
  week: "mt-0.5 flex w-full",
  day: "relative size-9 p-0 text-center text-sm",
  day_button:
    "inline-flex size-9 items-center justify-center rounded-lg font-normal tabular-nums outline-none transition-colors " +
    "hover:bg-accent hover:text-accent-foreground " +
    "focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 " +
    "aria-selected:bg-primary aria-selected:font-medium aria-selected:text-primary-foreground " +
    "aria-selected:hover:bg-primary aria-selected:hover:text-primary-foreground",
  today:
    "[&>button:not([aria-selected])]:border [&>button:not([aria-selected])]:border-primary/40 [&>button:not([aria-selected])]:font-semibold [&>button:not([aria-selected])]:text-primary",
  outside: "[&>button]:text-muted-foreground/40",
  disabled: "[&>button]:pointer-events-none [&>button]:opacity-30",
  hidden: "invisible",
}

function buildDisabledMatcher(
  fromDate?: Date,
  toDate?: Date
): Matcher | Matcher[] | undefined {
  if (fromDate && toDate) return [{ before: fromDate }, { after: toDate }]
  if (fromDate) return { before: fromDate }
  if (toDate) return { after: toDate }
  return undefined
}

export interface CalendarProps {
  selected?: Date
  onSelect?: (date: Date | undefined) => void
  fromDate?: Date
  toDate?: Date
  localeCode?: string
}

export function Calendar({
  selected,
  onSelect,
  fromDate,
  toDate,
  localeCode,
}: CalendarProps) {
  const currentLocale = useLocale()
  const dateLocale = resolveDateLocale(localeCode ?? currentLocale)
  return (
    <DayPicker
      mode="single"
      showOutsideDays
      selected={selected}
      onSelect={onSelect}
      disabled={buildDisabledMatcher(fromDate, toDate)}
      classNames={DAY_PICKER_CLASS_NAMES}
      locale={dateLocale}
      components={{
        Chevron: ({ orientation, className, style, size }) => {
          const Icon =
            orientation === "left"
              ? ChevronLeft
              : orientation === "right"
                ? ChevronRight
                : orientation === "up"
                  ? ChevronUp
                  : ChevronDown
          return (
            <Icon
              className={cn("size-4", className)}
              size={size}
              style={style}
            />
          )
        },
      }}
    />
  )
}
