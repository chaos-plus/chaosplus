import { useEffect, useId, useMemo } from "react"

import { Checkbox } from "../checkbox"
import { DatePicker } from "@workspace/ui/components/pickers/date-picker"
import { DateTimePicker } from "@workspace/ui/components/pickers/date-time-picker"
import { Input } from "../input"
import { RadioGroup, RadioGroupOption } from "../radio-group"
import { SimpleSelect } from "../select"
import { Switch } from "../switch"
import { Tabs, TabsList, TabsTrigger } from "../tabs"
import { Textarea } from "../textarea"
import { TimePicker } from "@workspace/ui/components/pickers/time-picker"
import { cn } from "@workspace/ui/lib/utils"
import { evaluate, normalizeOptions, type FieldRendererProps } from "./types"

type RangeValue = {
  start?: string
  end?: string
}

const dateInputClassName =
  "h-8 w-full min-w-[9rem] justify-start rounded-md text-sm"
const dateRangeInputClassName =
  "h-8 min-w-[9rem] flex-1 justify-start rounded-md text-sm"

function OptionalAddon({
  value,
  values,
  className,
}: {
  value?:
    React.ReactNode | ((values?: Record<string, unknown>) => React.ReactNode)
  values?: Record<string, unknown>
  className?: string
}) {
  if (!value) return null
  return (
    <span className={className}>
      {typeof value === "function" ? value(values) : value}
    </span>
  )
}

export function TextFieldRenderer(props: FieldRendererProps) {
  const {
    name,
    value,
    onChange,
    config,
    disabled,
    error,
    labelId,
    errorId,
    describedBy,
    values,
  } = props
  const id = useId()
  const inputType = (config.type as string) || "text"
  return (
    <div className="w-full">
      <div className="relative flex items-stretch">
        <OptionalAddon
          value={config.prefix}
          values={values}
          className={cn(
            "absolute top-1/2 left-3 -translate-y-1/2 text-xs text-muted-foreground",
            config.prefixClassName as string | undefined
          )}
        />
        <Input
          id={id}
          name={name}
          type={inputType}
          value={(value as string | number | undefined) ?? ""}
          onChange={(e) => onChange(e.target.value)}
          disabled={disabled}
          aria-labelledby={labelId}
          aria-describedby={describedBy}
          placeholder={config.placeholder as string | undefined}
          min={config.min as number | undefined}
          max={config.max as number | undefined}
          step={config.step as number | undefined}
          className={[
            config.prefix ? "pl-10" : "",
            config.suffix ? "pr-10" : "",
            error ? "border-destructive focus-visible:ring-destructive" : "",
          ].join(" ")}
        />
        <OptionalAddon
          value={config.suffix}
          values={values}
          className={cn(
            "absolute top-1/2 right-3 -translate-y-1/2 text-xs text-muted-foreground",
            config.suffixClassName as string | undefined
          )}
        />
      </div>
      {error ? (
        <p id={errorId} className="mt-1 text-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}

export function TextAreaFieldRenderer(props: FieldRendererProps) {
  const {
    name,
    value,
    onChange,
    config,
    disabled,
    error,
    labelId,
    errorId,
    describedBy,
    values,
  } = props
  const id = useId()
  const maxLenField = config.maxLenField as string | undefined
  const maxLen = maxLenField ? Number(values?.[maxLenField] ?? 0) : 0
  const currentLen = String(value ?? "").length
  return (
    <div className="w-full">
      <Textarea
        id={id}
        name={name}
        value={(value as string | undefined) ?? ""}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
        aria-labelledby={labelId}
        aria-describedby={describedBy}
        placeholder={config.placeholder as string | undefined}
        rows={config.rows ?? 3}
        className={
          error ? "border-destructive focus-visible:ring-destructive" : ""
        }
      />
      <div className="mt-1 flex items-center justify-between gap-2">
        {error ? (
          <p id={errorId} className="text-xs text-destructive">
            {error}
          </p>
        ) : (
          <span />
        )}
        {maxLen > 0 && (
          <span
            className={cn(
              "text-xs text-muted-foreground tabular-nums",
              currentLen > maxLen && "text-destructive"
            )}
          >
            {currentLen}/{maxLen}
          </span>
        )}
      </div>
    </div>
  )
}

export function SelectFieldRenderer(props: FieldRendererProps) {
  const {
    value,
    onChange,
    config,
    disabled,
    error,
    labelId,
    errorId,
    describedBy,
  } = props
  const options = useMemo(
    () => normalizeOptions(config.options),
    [config.options]
  )
  const current = value as string | undefined

  return (
    <div className="w-full">
      <SimpleSelect
        options={options}
        value={current ?? ""}
        onValueChange={(v) => onChange(v)}
        disabled={disabled}
        placeholder={config.placeholder as string | undefined}
        className={[
          "w-full",
          error ? "border-destructive focus:ring-destructive" : "",
        ].join(" ")}
        aria-labelledby={labelId}
        aria-describedby={describedBy}
      />
      {error ? (
        <p id={errorId} className="mt-1 text-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}

export function MultiSelectFieldRenderer(props: FieldRendererProps) {
  const { value, onChange, config, disabled, labelId } = props
  const options = useMemo(
    () => normalizeOptions(config.options),
    [config.options]
  )
  const selected = useMemo(
    () =>
      Array.isArray(value) ? (value as string[]) : value ? [String(value)] : [],
    [value]
  )

  const toggle = (optValue: string) => {
    const next = selected.includes(optValue)
      ? selected.filter((v) => v !== optValue)
      : [...selected, optValue]
    onChange(next)
  }

  return (
    <div className="flex flex-wrap gap-4" aria-labelledby={labelId}>
      {options.map((opt) => (
        <label
          key={opt.value}
          className="flex items-center gap-2 text-sm disabled:opacity-50"
        >
          <Checkbox
            checked={selected.includes(opt.value)}
            onCheckedChange={() => toggle(opt.value)}
            disabled={disabled}
          />
          {opt.label}
        </label>
      ))}
    </div>
  )
}

export function CheckboxFieldRenderer(props: FieldRendererProps) {
  const { value, onChange, config, disabled, labelId } = props
  const label = evaluate(config.label)
  return (
    <label className="flex items-center gap-2 text-sm">
      <Checkbox
        checked={Boolean(value)}
        onCheckedChange={(checked) => onChange(checked === true)}
        disabled={disabled}
        aria-labelledby={labelId}
      />
      {label ?? evaluate(config.placeholder)}
    </label>
  )
}

export function SwitchFieldRenderer(props: FieldRendererProps) {
  const { value, onChange, config, disabled, labelId } = props
  return (
    <label className="flex items-center gap-3 text-sm">
      <Switch
        checked={Boolean(value)}
        onCheckedChange={(checked) => onChange(checked === true)}
        disabled={disabled}
        aria-labelledby={labelId}
      />
      <span className="text-muted-foreground">{evaluate(config.label)}</span>
    </label>
  )
}

export function RadioFieldRenderer(props: FieldRendererProps) {
  const { value, onChange, config, disabled, labelId } = props
  const options = useMemo(
    () => normalizeOptions(config.options),
    [config.options]
  )
  return (
    <RadioGroup
      value={(value as string | undefined) ?? ""}
      onValueChange={(v) => onChange(v)}
      className="flex flex-wrap gap-4"
      aria-labelledby={labelId}
    >
      {options.map((opt) => (
        <RadioGroupOption
          key={opt.value}
          value={opt.value}
          label={opt.label}
          disabled={disabled}
        />
      ))}
    </RadioGroup>
  )
}

export function DateFieldRenderer(props: FieldRendererProps) {
  const {
    value,
    onChange,
    config,
    disabled,
    error,
    labelId,
    errorId,
    describedBy,
  } = props
  const current = parseDateValue(value)
  return (
    <div className="w-full">
      <DatePicker
        value={current}
        onChange={(date) => onChange(formatDateValue(date))}
        disabled={disabled}
        placeholder={config.placeholder as string | undefined}
        fromDate={config.fromDate}
        toDate={config.toDate}
        className={cn(
          dateInputClassName,
          error && "border-destructive focus-visible:ring-destructive"
        )}
      />
      <span
        className="sr-only"
        aria-labelledby={labelId}
        aria-describedby={describedBy}
      />
      {error ? (
        <p id={errorId} className="mt-1 text-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}

export function DateTimeFieldRenderer(props: FieldRendererProps) {
  const {
    value,
    onChange,
    config,
    disabled,
    error,
    labelId,
    errorId,
    describedBy,
  } = props
  return (
    <div className="w-full">
      <div aria-labelledby={labelId} aria-describedby={describedBy}>
        <DateTimePicker
          value={(value as string | undefined) ?? ""}
          onChange={(v) => onChange(v)}
          disabled={disabled}
          placeholder={config.placeholder as string | undefined}
          showSeconds={config.showSeconds === true}
          fromDate={config.fromDate}
          toDate={config.toDate}
          className={cn(
            dateRangeInputClassName,
            error && "border-destructive focus-visible:ring-destructive"
          )}
        />
      </div>
      {error ? (
        <p id={errorId} className="mt-1 text-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}

export function TimeFieldRenderer(props: FieldRendererProps) {
  const {
    value,
    onChange,
    config,
    disabled,
    error,
    labelId,
    errorId,
    describedBy,
  } = props
  return (
    <div className="w-full">
      <TimePicker
        value={(value as string | undefined) ?? ""}
        onChange={(next) => onChange(next)}
        disabled={disabled}
        granularity={config.granularity}
        showSeconds={config.showSeconds === true}
        formatValue={config.formatValue}
        compact
        className={cn(
          "h-8",
          error && "border-destructive focus-visible:ring-destructive"
        )}
      />
      <span
        className="sr-only"
        aria-labelledby={labelId}
        aria-describedby={describedBy}
      />
      {error ? (
        <p id={errorId} className="mt-1 text-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}

export function DateRangeFieldRenderer(props: FieldRendererProps) {
  const {
    value,
    onChange,
    config,
    disabled,
    error,
    labelId,
    errorId,
    describedBy,
  } = props
  const range = normalizeRangeValue(value)

  const update = (patch: Partial<RangeValue>) => {
    onChange({ ...range, ...patch })
  }

  return (
    <div className="w-full">
      <div
        className="flex max-w-full flex-wrap items-center gap-2"
        aria-labelledby={labelId}
        aria-describedby={describedBy}
      >
        <DatePicker
          value={parseDateValue(range.start)}
          onChange={(date) => update({ start: formatDateValue(date) })}
          disabled={disabled}
          placeholder={
            config.startPlaceholder ??
            (config.placeholder as string | undefined)
          }
          fromDate={config.fromDate}
          toDate={config.toDate}
          className={cn(
            dateRangeInputClassName,
            error && "border-destructive focus-visible:ring-destructive"
          )}
        />
        <span className="shrink-0 text-sm text-muted-foreground">-</span>
        <DatePicker
          value={parseDateValue(range.end)}
          onChange={(date) => update({ end: formatDateValue(date) })}
          disabled={disabled}
          placeholder={
            config.endPlaceholder ?? (config.placeholder as string | undefined)
          }
          fromDate={
            range.start
              ? (parseDateValue(range.start) ?? config.fromDate)
              : config.fromDate
          }
          toDate={config.toDate}
          className={cn(
            dateRangeInputClassName,
            error && "border-destructive focus-visible:ring-destructive"
          )}
        />
      </div>
      {error ? (
        <p id={errorId} className="mt-1 text-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}

export function TimeRangeFieldRenderer(props: FieldRendererProps) {
  const {
    value,
    onChange,
    config,
    disabled,
    error,
    labelId,
    errorId,
    describedBy,
  } = props
  const range = normalizeRangeValue(value)
  const showSeconds = config.showSeconds === true

  const update = (patch: Partial<RangeValue>) => {
    onChange({ ...range, ...patch })
  }

  return (
    <div className="w-full">
      <div
        className="flex max-w-full flex-wrap items-center gap-2"
        aria-labelledby={labelId}
        aria-describedby={describedBy}
      >
        <TimePicker
          value={range.start ?? ""}
          onChange={(next) => update({ start: next })}
          disabled={disabled}
          granularity={config.granularity}
          showSeconds={showSeconds}
          formatValue={config.formatValue}
          compact
          className={cn(
            "h-8",
            error && "border-destructive focus-visible:ring-destructive"
          )}
        />
        <span className="shrink-0 text-sm text-muted-foreground">-</span>
        <TimePicker
          value={range.end ?? ""}
          onChange={(next) => update({ end: next })}
          disabled={disabled}
          granularity={config.granularity}
          showSeconds={showSeconds}
          formatValue={config.formatValue}
          compact
          className={cn(
            "h-8",
            error && "border-destructive focus-visible:ring-destructive"
          )}
        />
      </div>
      {error ? (
        <p id={errorId} className="mt-1 text-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}

export function DateTimeRangeFieldRenderer(props: FieldRendererProps) {
  const {
    value,
    onChange,
    config,
    disabled,
    error,
    labelId,
    errorId,
    describedBy,
  } = props
  const range = normalizeRangeValue(value)
  const start = splitDateTimeValue(range.start)
  const end = splitDateTimeValue(range.end)
  const showSeconds = config.showSeconds === true

  const join = (date: string, time: string) =>
    date && time ? `${date}T${time}` : ""
  const update = (patch: Partial<RangeValue>) => {
    onChange({ ...range, ...patch })
  }

  return (
    <div className="w-full">
      <div
        className="grid max-w-full gap-2"
        aria-labelledby={labelId}
        aria-describedby={describedBy}
      >
        <div className="flex max-w-full flex-wrap items-center gap-2">
          <DatePicker
            value={parseDateValue(start.date)}
            onChange={(date) =>
              update({ start: join(formatDateValue(date), start.time) })
            }
            disabled={disabled}
            placeholder={
              config.startPlaceholder ??
              (config.placeholder as string | undefined)
            }
            fromDate={config.fromDate}
            toDate={config.toDate}
            className={cn(
              dateRangeInputClassName,
              error && "border-destructive focus-visible:ring-destructive"
            )}
          />
          <TimePicker
            value={start.time}
            onChange={(time) => update({ start: join(start.date, time) })}
            disabled={disabled}
            granularity={config.granularity}
            showSeconds={showSeconds}
            formatValue={config.formatValue}
            compact
            className={cn(
              "h-8 shrink-0",
              error && "border-destructive focus-visible:ring-destructive"
            )}
          />
        </div>
        <div className="flex max-w-full flex-wrap items-center gap-2">
          <DatePicker
            value={parseDateValue(end.date)}
            onChange={(date) =>
              update({ end: join(formatDateValue(date), end.time) })
            }
            disabled={disabled}
            placeholder={
              config.endPlaceholder ??
              (config.placeholder as string | undefined)
            }
            fromDate={
              start.date
                ? (parseDateValue(start.date) ?? config.fromDate)
                : config.fromDate
            }
            toDate={config.toDate}
            className={cn(
              dateRangeInputClassName,
              error && "border-destructive focus-visible:ring-destructive"
            )}
          />
          <TimePicker
            value={end.time}
            onChange={(time) => update({ end: join(end.date, time) })}
            disabled={disabled}
            granularity={config.granularity}
            showSeconds={showSeconds}
            formatValue={config.formatValue}
            compact
            className={cn(
              "h-8 shrink-0",
              error && "border-destructive focus-visible:ring-destructive"
            )}
          />
        </div>
      </div>
      {error ? (
        <p id={errorId} className="mt-1 text-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}

export function TabsFieldRenderer(props: FieldRendererProps) {
  const { value, onChange, config, labelId } = props
  const options = useMemo(() => {
    const raw = config.tabs
    if (!raw) return []
    if (Array.isArray(raw)) return raw
    return Object.entries(raw).map(([value, label]) => ({ value, label }))
  }, [config.tabs])

  const current = (value as string | undefined) ?? (options[0]?.value || "")

  useEffect(() => {
    if (value === undefined && options[0]?.value) {
      onChange(options[0].value)
    }
  }, [value, options, onChange])

  return (
    <Tabs value={current} onValueChange={(v) => onChange(v)}>
      <TabsList className="w-full justify-start" aria-labelledby={labelId}>
        {options.map((opt) => (
          <TabsTrigger key={opt.value} value={opt.value}>
            {opt.label}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  )
}

export function TitleFieldRenderer(props: FieldRendererProps) {
  const { config } = props
  const label =
    (config.label as React.ReactNode) ?? evaluate(config.placeholder)
  return (
    <div className="col-span-full my-2 flex items-center gap-2 rounded-md bg-primary px-3 py-2 text-sm font-bold text-primary-foreground">
      {label}
    </div>
  )
}

export function CustomFieldRenderer(
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  _props: FieldRendererProps
) {
  // This renderer is never reached because AutoForm directly renders the custom component.
  return null
}

function parseDateValue(value: unknown): Date | null {
  if (value instanceof Date && !Number.isNaN(value.getTime())) return value
  if (typeof value !== "string" || !value) return null
  const datePart = value.includes("T") ? (value.split("T")[0] ?? "") : value
  const parts = datePart.split("-").map(Number)
  if (parts.length !== 3 || parts.some((part) => Number.isNaN(part)))
    return null
  return new Date(parts[0]!, parts[1]! - 1, parts[2]!)
}

function formatDateValue(date: Date | null): string {
  if (!date) return ""
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, "0")
  const day = String(date.getDate()).padStart(2, "0")
  return `${year}-${month}-${day}`
}

function splitDateTimeValue(value: unknown): { date: string; time: string } {
  if (typeof value !== "string" || !value) return { date: "", time: "" }
  const [date = "", rawTime = ""] = value.split("T")
  return { date, time: rawTime.slice(0, 8) }
}

function normalizeRangeValue(value: unknown): RangeValue {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {}
  const range = value as RangeValue
  return {
    start: typeof range.start === "string" ? range.start : "",
    end: typeof range.end === "string" ? range.end : "",
  }
}
