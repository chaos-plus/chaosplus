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

// Match the text Input / Select trigger styling so the picker looks consistent in filter bars and forms.
const TRIGGER_CLASS =
  "flex h-9 w-[240px] items-center justify-start overflow-hidden rounded-md border border-input bg-transparent px-3 py-2 text-left text-sm font-normal shadow-sm ring-offset-background transition-colors hover:bg-accent focus:outline-none focus:ring-2 focus:ring-ring disabled:cursor-not-allowed disabled:opacity-50"

// Shared unified date+time picker: ONE popover, ONE layer — calendar on the left,
// a circular analog clock on the right (no nested time popup). value is a single
// "YYYY-MM-DDTHH:mm[:ss]" string. Use everywhere instead of hand-combining pickers.

const pad = (n: number) => String(n).padStart(2, "0")

const CLOCK_SIZE = 208
const CLOCK_C = CLOCK_SIZE / 2
const CLOCK_R = 78

function polar(deg: number, r: number): { x: number; y: number } {
  const rad = (deg * Math.PI) / 180
  return { x: CLOCK_C + r * Math.cos(rad), y: CLOCK_C + r * Math.sin(rad) }
}

function parseDateValue(value?: string): Date | null {
  if (!value) return null
  const datePart = value.includes("T") ? (value.split("T")[0] ?? "") : value
  const parts = datePart.split("-").map(Number)
  if (parts.length !== 3 || parts.some((p) => Number.isNaN(p))) return null
  return new Date(parts[0]!, parts[1]! - 1, parts[2]!)
}

function formatDateValue(date: Date | null): string {
  if (!date) return ""
  const y = date.getFullYear()
  const m = String(date.getMonth() + 1).padStart(2, "0")
  const d = String(date.getDate()).padStart(2, "0")
  return `${y}-${m}-${d}`
}

function splitDateTime(value?: string): { date: string; time: string } {
  if (!value) return { date: "", time: "" }
  const [date = "", rawTime = ""] = value.split("T")
  return { date, time: rawTime.slice(0, 8) }
}

// Circular analog clock — click an hour (switches to minutes), then a minute.
function Clock({
  value,
  onChange,
}: {
  value: string
  onChange: (time: string) => void
}) {
  const locale = useLocale()
  const [mode, setMode] = useState<"h" | "m">("h")

  // Localized AM/PM via Intl (zh → 上午/下午, en → AM/PM, ms → PG/PTG). No extra i18n keys.
  const periodLabel = (p: "AM" | "PM") => {
    const probe = new Date(2020, 0, 1, p === "AM" ? 6 : 18)
    const parts = new Intl.DateTimeFormat(locale, {
      hour: "numeric",
      hour12: true,
    }).formatToParts(probe)
    return parts.find((x) => x.type === "dayPeriod")?.value ?? p
  }
  const [hh = "0", mm = "0"] = value.split(":")
  const hour24 = Number(hh) || 0
  const minute = Number(mm) || 0
  const period: "AM" | "PM" = hour24 >= 12 ? "PM" : "AM"
  const h12 = hour24 % 12 || 12

  const setHour = (label: number) => {
    const base = label % 12
    const h24 = period === "PM" ? base + 12 : base
    onChange(`${pad(h24)}:${pad(minute)}`)
    setMode("m")
  }
  const setMinute = (m: number) => onChange(`${pad(hour24)}:${pad(m)}`)
  const setPeriod = (p: "AM" | "PM") => {
    const base = hour24 % 12
    const h24 = p === "PM" ? base + 12 : base
    onChange(`${pad(h24)}:${pad(minute)}`)
  }

  const labels =
    mode === "h"
      ? Array.from({ length: 12 }, (_, i) => i + 1) // 1..12
      : Array.from({ length: 12 }, (_, i) => i * 5) // 0,5,...,55

  const handDeg = mode === "h" ? (hour24 % 12) * 30 - 90 : minute * 6 - 90
  const handEnd = polar(handDeg, CLOCK_R)

  return (
    <div className="flex flex-col items-center gap-2">
      <div className="flex items-center gap-1 text-lg font-semibold tabular-nums">
        <button
          type="button"
          onClick={() => setMode("h")}
          className={cn("rounded px-1", mode === "h" && "text-primary")}
        >
          {pad(h12)}
        </button>
        <span>:</span>
        <button
          type="button"
          onClick={() => setMode("m")}
          className={cn("rounded px-1", mode === "m" && "text-primary")}
        >
          {pad(minute)}
        </button>
      </div>

      <div
        className="relative rounded-full bg-muted/40"
        style={{ width: CLOCK_SIZE, height: CLOCK_SIZE }}
      >
        <svg
          className="absolute inset-0 text-primary"
          width={CLOCK_SIZE}
          height={CLOCK_SIZE}
          aria-hidden
        >
          <line
            x1={CLOCK_C}
            y1={CLOCK_C}
            x2={handEnd.x}
            y2={handEnd.y}
            stroke="currentColor"
            strokeWidth={2}
          />
          <circle
            cx={handEnd.x}
            cy={handEnd.y}
            r={18}
            fill="currentColor"
            opacity={0.18}
          />
          <circle cx={CLOCK_C} cy={CLOCK_C} r={3} fill="currentColor" />
        </svg>
        {labels.map((label) => {
          const deg =
            mode === "h" ? (label % 12) * 30 - 90 : (label / 5) * 30 - 90
          const p = polar(deg, CLOCK_R)
          const selected =
            mode === "h" ? label % 12 === hour24 % 12 : label === minute
          return (
            <button
              key={label}
              type="button"
              onClick={() => (mode === "h" ? setHour(label) : setMinute(label))}
              style={{ left: p.x, top: p.y }}
              className={cn(
                "absolute flex size-7 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full text-sm tabular-nums transition-colors hover:bg-accent",
                selected &&
                  "bg-primary text-primary-foreground hover:bg-primary"
              )}
            >
              {pad(label)}
            </button>
          )
        })}
      </div>

      <div className="flex gap-1 text-xs">
        {(["AM", "PM"] as const).map((p) => (
          <button
            key={p}
            type="button"
            onClick={() => setPeriod(p)}
            className={cn(
              "rounded-md border px-3 py-1 transition-colors hover:bg-accent",
              period === p &&
                "bg-primary text-primary-foreground hover:bg-primary"
            )}
          >
            {periodLabel(p)}
          </button>
        ))}
      </div>
    </div>
  )
}

export interface DateTimePickerProps {
  value?: string
  onChange?: (value: string) => void
  disabled?: boolean
  placeholder?: string
  showSeconds?: boolean
  fromDate?: Date
  toDate?: Date
  className?: string
}

export function DateTimePicker({
  value,
  onChange,
  disabled = false,
  placeholder = "Select date & time",
  showSeconds = false,
  fromDate,
  toDate,
  className,
}: DateTimePickerProps) {
  const currentLocale = useLocale()
  const [open, setOpen] = useState(false)
  const { date, time } = splitDateTime(value)
  const dateObj = parseDateValue(date)

  const defaultTime = showSeconds ? "00:00:00" : "00:00"
  const display = dateObj ? `${date} ${time || defaultTime}` : null

  // A value exists only when both date and time are set (matches form semantics).
  const emit = (nextDate: string, nextTime: string) =>
    onChange?.(nextDate && nextTime ? `${nextDate}T${nextTime}` : "")

  return (
    <Popover open={open} onOpenChange={(next) => !disabled && setOpen(next)}>
      <PopoverTrigger
        disabled={disabled}
        className={cn(
          TRIGGER_CLASS,
          !display && "text-muted-foreground",
          className
        )}
      >
        <CalendarIcon className="mr-2 size-4 shrink-0 opacity-50" />
        <span className="truncate">{display ?? placeholder}</span>
      </PopoverTrigger>

      <PopoverContent className="w-auto rounded-xl p-3 shadow-lg">
        <div className="flex items-center gap-3">
          {/* left = date */}
          <Calendar
            selected={dateObj ?? undefined}
            onSelect={(d) =>
              emit(formatDateValue(d ?? null), time || defaultTime)
            }
            fromDate={fromDate}
            toDate={toDate}
            localeCode={currentLocale}
          />
          {/* right = circular clock (one layer, no nested popup) */}
          <div className="self-stretch border-l pl-3">
            <Clock
              value={(time || defaultTime).slice(0, 5)}
              onChange={(t) =>
                emit(
                  date || formatDateValue(new Date()),
                  showSeconds ? `${t}:00` : t
                )
              }
            />
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}
