import { useState } from "react"
import type { ChangeEvent, KeyboardEvent, PointerEvent } from "react"
import { Clock } from "lucide-react"

import { cn } from "@workspace/ui/lib/utils"
import { buttonVariants } from "@workspace/ui/components/button"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@workspace/ui/components/popover"

export interface TimePickerProps {
  value?: string
  onChange?: (value: string) => void
  disabled?: boolean
  granularity?: TimeUnit
  showSeconds?: boolean
  compact?: boolean
  formatValue?: (
    value: string,
    parts: { hours: number; minutes: number; seconds: number }
  ) => string
  hourLabel?: string
  minuteLabel?: string
  secondLabel?: string
  incrementLabel?: (label: string) => string
  decrementLabel?: (label: string) => string
  className?: string
}

type TimeUnit = "hour" | "minute" | "second"

const CLOCK_SIZE = 184
const CLOCK_CENTER = CLOCK_SIZE / 2
const CLOCK_RADIUS = 73
const HOUR_VALUES = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12]
const MINUTE_MARKS = Array.from({ length: 60 }, (_, index) => index)
const MINUTE_LABEL_VALUES = Array.from({ length: 12 }, (_, index) => index * 5)

function pad(n: number) {
  return n.toString().padStart(2, "0")
}

function parseTime(raw: string | undefined) {
  const parts = (raw ?? "").split(":").map(Number)
  return {
    h: clamp(Number.isFinite(parts[0]) ? (parts[0] ?? 0) : 0, 0, 23),
    m: clamp(Number.isFinite(parts[1]) ? (parts[1] ?? 0) : 0, 0, 59),
    s: clamp(Number.isFinite(parts[2]) ? (parts[2] ?? 0) : 0, 0, 59),
  }
}

function clamp(val: number, min: number, max: number) {
  return Math.max(min, Math.min(max, val))
}

function formatTime(h: number, m: number, s: number, granularity: TimeUnit) {
  if (granularity === "hour") return pad(h)
  if (granularity === "second") return `${pad(h)}:${pad(m)}:${pad(s)}`
  return `${pad(h)}:${pad(m)}`
}

function nextUnit(unit: TimeUnit, granularity: TimeUnit): TimeUnit {
  if (unit === "hour" && granularity !== "hour") return "minute"
  if (unit === "minute" && granularity === "second") return "second"
  return unit
}

function normalizeActiveUnit(unit: TimeUnit, granularity: TimeUnit): TimeUnit {
  if (unit === "second" && granularity !== "second") return "minute"
  if (unit === "minute" && granularity === "hour") return "hour"
  return unit
}

function stepValue(value: number, delta: number, min: number, max: number) {
  const next = value + delta
  if (next > max) return min
  if (next < min) return max
  return next
}

function handAngle(unit: TimeUnit, value: number) {
  const position = unit === "hour" ? value % 12 : value / 5
  return position * 30 - 90
}

function point(angle: number, radius: number) {
  const radians = (angle * Math.PI) / 180
  return {
    x: CLOCK_CENTER + Math.cos(radians) * radius,
    y: CLOCK_CENTER + Math.sin(radians) * radius,
  }
}

function valueFromPoint(
  clientX: number,
  clientY: number,
  rect: DOMRect,
  unit: TimeUnit
) {
  const x = clientX - rect.left - CLOCK_CENTER
  const y = clientY - rect.top - CLOCK_CENTER
  const degrees = (Math.atan2(y, x) * 180) / Math.PI
  const normalized = (degrees + 90 + 360) % 360

  if (unit === "hour") {
    const hour12 = Math.round(normalized / 30) % 12
    const outerHour = hour12 === 0 ? 12 : hour12
    const distance = Math.hypot(x, y)
    return distance < 58 ? (outerHour === 12 ? 0 : outerHour + 12) : outerHour
  }

  return Math.round(normalized / 6) % 60
}

interface NumberInputProps {
  value: number
  min: number
  max: number
  label: string
  active: boolean
  onFocus: () => void
  onChange: (value: number) => void
}

function NumberInput({
  value,
  min,
  max,
  label,
  active,
  onFocus,
  onChange,
}: NumberInputProps) {
  function handleChange(e: ChangeEvent<HTMLInputElement>) {
    const raw = e.target.value.replace(/\D/g, "").slice(-2)
    if (!raw) return
    onChange(clamp(parseInt(raw, 10), min, max))
  }

  function handleKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === "ArrowUp") {
      e.preventDefault()
      onChange(stepValue(value, 1, min, max))
    }
    if (e.key === "ArrowDown") {
      e.preventDefault()
      onChange(stepValue(value, -1, min, max))
    }
  }

  return (
    <input
      type="text"
      inputMode="numeric"
      value={pad(value)}
      data-active={active ? "true" : undefined}
      aria-label={label}
      onChange={handleChange}
      onKeyDown={handleKeyDown}
      onFocus={(e) => {
        onFocus()
        e.target.select()
      }}
      className={cn(
        "time-picker-input h-9 w-12 rounded-md border border-input bg-background text-center text-sm text-current",
        "font-mono font-semibold tabular-nums transition-colors outline-none",
        "focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40",
        active && "shadow-sm"
      )}
    />
  )
}

interface ClockFaceProps {
  unit: TimeUnit
  value: number
  label: string
  onSelect: (value: number) => void
  onCommit: () => void
}

function ClockFace({ unit, value, label, onSelect, onCommit }: ClockFaceProps) {
  const angle = handAngle(unit, value)
  const handLength =
    unit === "hour" && value > 12 ? 46 : unit === "hour" ? 62 : 70

  function selectFromPointer(e: PointerEvent<HTMLDivElement>) {
    const rect = e.currentTarget.getBoundingClientRect()
    onSelect(valueFromPoint(e.clientX, e.clientY, rect, unit))
  }

  function handlePointerDown(e: PointerEvent<HTMLDivElement>) {
    e.currentTarget.setPointerCapture(e.pointerId)
    selectFromPointer(e)
  }

  function handlePointerMove(e: PointerEvent<HTMLDivElement>) {
    if (e.currentTarget.hasPointerCapture(e.pointerId)) {
      selectFromPointer(e)
    }
  }

  function handlePointerUp(e: PointerEvent<HTMLDivElement>) {
    if (e.currentTarget.hasPointerCapture(e.pointerId)) {
      e.currentTarget.releasePointerCapture(e.pointerId)
    }
    onCommit()
  }

  return (
    <div
      role="group"
      aria-label={label}
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={handlePointerUp}
      className="relative mx-auto touch-none rounded-full bg-muted/60"
      style={{ width: CLOCK_SIZE, height: CLOCK_SIZE }}
    >
      <div
        className="absolute top-1/2 left-1/2 h-0.5 origin-left rounded-full bg-primary"
        style={{
          width: handLength,
          transform: `translateY(-50%) rotate(${angle}deg)`,
        }}
      />
      <div className="absolute top-1/2 left-1/2 size-2 -translate-x-1/2 -translate-y-1/2 rounded-full bg-primary" />

      {MINUTE_MARKS.map((mark) => {
        const markAngle = mark * 6 - 90
        const position = point(markAngle, mark % 5 === 0 ? 80 : 82)

        return (
          <span
            key={`mark-${mark}`}
            className={cn(
              "pointer-events-none absolute -translate-x-1/2 -translate-y-1/2 rounded-full bg-muted-foreground/40",
              mark % 5 === 0 ? "size-1" : "size-0.5"
            )}
            style={{ left: position.x, top: position.y }}
          />
        )
      })}

      {unit === "hour" &&
        HOUR_VALUES.map((option) => {
          const optionAngle = handAngle("hour", option)
          const position = point(optionAngle, CLOCK_RADIUS)
          const selectedHour =
            value === option || (value === 0 && option === 12)

          return (
            <button
              key={`hour-${option}`}
              type="button"
              aria-pressed={selectedHour}
              onPointerDown={(e) => {
                e.stopPropagation()
                onSelect(option === 12 ? (value >= 12 ? 12 : 0) : option)
              }}
              onPointerUp={(e) => {
                e.stopPropagation()
                onCommit()
              }}
              className={cn(
                "pointer-events-auto",
                "absolute flex size-8 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full",
                "text-xs font-medium tabular-nums transition-colors",
                selectedHour
                  ? "bg-primary text-primary-foreground shadow-sm"
                  : "text-foreground hover:bg-accent hover:text-accent-foreground"
              )}
              style={{ left: position.x, top: position.y }}
            >
              {option}
            </button>
          )
        })}

      {unit !== "hour" &&
        MINUTE_LABEL_VALUES.map((option) => {
          const optionAngle = handAngle(unit, option)
          const position = point(optionAngle, CLOCK_RADIUS)
          const selectedMinute = value === option

          return (
            <button
              key={`${unit}-${option}`}
              type="button"
              aria-pressed={selectedMinute}
              onPointerDown={(e) => {
                e.stopPropagation()
                onSelect(option)
              }}
              onPointerUp={(e) => {
                e.stopPropagation()
                onCommit()
              }}
              className={cn(
                "pointer-events-auto",
                "absolute flex size-8 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full",
                "text-xs font-medium tabular-nums transition-colors",
                selectedMinute
                  ? "bg-primary text-primary-foreground shadow-sm"
                  : "text-foreground hover:bg-accent hover:text-accent-foreground"
              )}
              style={{ left: position.x, top: position.y }}
            >
              {pad(option)}
            </button>
          )
        })}

      {unit === "hour" && (
        <div className="pointer-events-none absolute inset-[54px] rounded-full border border-border/60" />
      )}

      {unit === "hour" &&
        [13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 0].map((option) => {
          const optionAngle = handAngle("hour", option)
          const position = point(optionAngle, 47)
          const selectedHour = value === option

          return (
            <button
              key={`hour-inner-${option}`}
              type="button"
              aria-pressed={selectedHour}
              onPointerDown={(e) => {
                e.stopPropagation()
                onSelect(option)
              }}
              onPointerUp={(e) => {
                e.stopPropagation()
                onCommit()
              }}
              className={cn(
                "pointer-events-auto",
                "absolute flex size-7 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full",
                "text-[0.68rem] font-medium tabular-nums transition-colors",
                selectedHour
                  ? "bg-primary text-primary-foreground shadow-sm"
                  : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              )}
              style={{ left: position.x, top: position.y }}
            >
              {pad(option)}
            </button>
          )
        })}
    </div>
  )
}

export function TimePicker({
  value,
  onChange,
  disabled = false,
  granularity,
  showSeconds = false,
  compact = true,
  formatValue,
  hourLabel = "Hours",
  minuteLabel = "Minutes",
  secondLabel = "Seconds",
  className,
}: TimePickerProps) {
  const [open, setOpen] = useState(false)
  const [activeUnit, setActiveUnit] = useState<TimeUnit>("hour")
  const { h, m, s } = parseTime(value)
  const effectiveGranularity =
    granularity ?? (showSeconds ? "second" : "minute")
  const normalizedActiveUnit = normalizeActiveUnit(
    activeUnit,
    effectiveGranularity
  )

  function emit(nextHour: number, nextMinute: number, nextSecond: number) {
    onChange?.(
      formatTime(nextHour, nextMinute, nextSecond, effectiveGranularity)
    )
  }

  function update(unit: TimeUnit, next: number) {
    if (unit === "hour") emit(next, m, s)
    if (unit === "minute") emit(h, next, s)
    if (unit === "second") emit(h, m, next)
  }

  function handleClockSelect(next: number) {
    update(normalizedActiveUnit, next)
  }

  function handleClockCommit() {
    const next = nextUnit(normalizedActiveUnit, effectiveGranularity)
    if (next === normalizedActiveUnit) {
      setOpen(false)
      return
    }
    setActiveUnit(next)
  }

  function handleOpenChange(next: boolean) {
    if (!disabled) {
      if (next) setActiveUnit("hour")
      setOpen(next)
    }
  }

  const activeValue =
    normalizedActiveUnit === "hour"
      ? h
      : normalizedActiveUnit === "minute"
        ? m
        : s
  const activeLabel =
    normalizedActiveUnit === "hour"
      ? hourLabel
      : normalizedActiveUnit === "minute"
        ? minuteLabel
        : secondLabel
  const rawValue = formatTime(h, m, s, effectiveGranularity)
  const displayValue =
    formatValue?.(rawValue, { hours: h, minutes: m, seconds: s }) ?? rawValue

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger
        disabled={disabled}
        className={cn(
          buttonVariants({ variant: "outline" }),
          compact
            ? "time-picker h-8 w-[8.5rem] justify-start text-left font-normal"
            : "time-picker h-9 w-[9rem] justify-start text-left font-normal",
          disabled && "pointer-events-none opacity-50",
          className
        )}
      >
        <Clock className="mr-2 size-4 shrink-0 opacity-50" />
        <span className="font-mono tabular-nums">{displayValue}</span>
      </PopoverTrigger>

      <PopoverContent className="time-picker-panel w-auto rounded-xl p-3 shadow-lg">
        <div className="space-y-3">
          <div className="text-center text-xs font-medium text-muted-foreground">
            {activeLabel}
          </div>
          <ClockFace
            unit={normalizedActiveUnit}
            value={activeValue}
            label={activeLabel}
            onSelect={handleClockSelect}
            onCommit={handleClockCommit}
          />

          <div className="flex items-center justify-center gap-1 text-foreground">
            <NumberInput
              value={h}
              min={0}
              max={23}
              label={hourLabel}
              active={normalizedActiveUnit === "hour"}
              onFocus={() => setActiveUnit("hour")}
              onChange={(next) => update("hour", next)}
            />
            {effectiveGranularity !== "hour" && (
              <>
                <span className="pb-px text-lg font-light text-current select-none">
                  :
                </span>
                <NumberInput
                  value={m}
                  min={0}
                  max={59}
                  label={minuteLabel}
                  active={normalizedActiveUnit === "minute"}
                  onFocus={() => setActiveUnit("minute")}
                  onChange={(next) => update("minute", next)}
                />
              </>
            )}
            {effectiveGranularity === "second" && (
              <>
                <span className="pb-px text-lg font-light text-current select-none">
                  :
                </span>
                <NumberInput
                  value={s}
                  min={0}
                  max={59}
                  label={secondLabel}
                  active={normalizedActiveUnit === "second"}
                  onFocus={() => setActiveUnit("second")}
                  onChange={(next) => update("second", next)}
                />
              </>
            )}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}
