import type { ComponentType, ReactNode } from "react"
import type { ZodType } from "zod"

export type FieldType =
  | "text"
  | "textarea"
  | "number"
  | "password"
  | "email"
  | "code"
  | "select"
  | "multi-select"
  | "checkbox"
  | "checkbox-group"
  | "switch"
  | "radio"
  | "date"
  | "dateRange"
  | "datetime"
  | "datetimeRange"
  | "time"
  | "timeRange"
  | "tabs"
  | "title"
  // rich admin field types (parity with the legacy xdd CommonForm* components)
  | "price"
  | "rich-text"
  | "code-editor"
  | "image"
  | "media"
  | "remote-select"
  | "remote-multi-select"
  | "relation-list"
  | "bool-select"
  | "location"
  | "custom"

export type Evaluable<T> = T | ((values?: Record<string, unknown>) => T)

export interface SelectOption {
  label: ReactNode
  value: string
}

export interface FieldComponentConfig {
  placeholder?: Evaluable<ReactNode>
  label?: Evaluable<ReactNode>
  prefix?: Evaluable<ReactNode>
  suffix?: Evaluable<ReactNode>
  /** Custom styling for the prefix/suffix (e.g. color text-primary); overrides the default text-muted-foreground */
  prefixClassName?: string
  suffixClassName?: string
  options?: SelectOption[] | Record<string, string>
  tabs?: Record<string, string> | SelectOption[]
  rows?: number
  min?: number
  max?: number
  step?: number
  fromDate?: Date
  toDate?: Date
  showSeconds?: boolean
  granularity?: "hour" | "minute" | "second"
  formatValue?: (
    value: string,
    parts: { hours: number; minutes: number; seconds: number }
  ) => string
  startPlaceholder?: string
  endPlaceholder?: string
  /** price: number of decimal places the cents value represents (default 2). */
  decimals?: number
  /** image/media/checkbox-group: allow multiple values. */
  multiple?: boolean
  /** image/media: max number of files when multiple (default 5). */
  maxFiles?: number
  /** image/media: accept attribute for the file picker. */
  accept?: string
  /** code-editor: language hint (e.g. 'json'). */
  lang?: string
  /** remote-select: endpoint path (e.g. '/goods-categories'). */
  url?: Evaluable<string>
  /** remote-select: extra filter params (field → value | {op,value}); supports Evaluable. */
  params?: Evaluable<Record<string, unknown>>
  /** remote-select: other field names whose change should reload the options. */
  dependsOn?: string[]
  /** remote-select / relation-list: option label / value property names (default 'name' / 'id'). */
  labelKey?: string
  valueKey?: string
  /** relation-list: count property name on each row (default 'count'). */
  countKey?: string
  /** bool-select: labels for the true/false options. */
  trueLabel?: Evaluable<ReactNode>
  falseLabel?: Evaluable<ReactNode>
  requiredMessage?: Evaluable<string>
  /** Validation rules; returning an error string means the value is invalid. Async is supported (e.g. remote uniqueness checks). */
  rules?: Array<
    (
      value: unknown,
      values: Record<string, unknown>
    ) => string | undefined | Promise<string | undefined>
  >
  validate?: (
    value: unknown,
    originalValue: unknown,
    values: Record<string, unknown>
  ) => string | undefined | Promise<string | undefined>
  [key: string]: unknown
}

export interface BaseFieldConfig<TValue = unknown> {
  name: Evaluable<ReactNode>
  type: FieldType
  /** Optionally derive the renderer type at runtime from current values (e.g. the coupon
   * efforts field switches between 'price' and 'number' by coupon type). Overrides `type`
   * for rendering only; the declared `type` still drives the custom discriminator. */
  dynamicType?: (values?: Record<string, unknown>) => FieldType
  value?: TValue
  required?: Evaluable<boolean>
  show?: Evaluable<boolean>
  disabled?: Evaluable<boolean>
  submit?: boolean
  copy?: boolean
  config?: FieldComponentConfig
  /** Grid column span in a 2-column layout. 1 = half width, 2 = full width (default). */
  col?: 1 | 2
}

export interface CustomFieldConfig<
  TValue = unknown,
> extends BaseFieldConfig<TValue> {
  type: "custom"
  component: ComponentType<FieldRendererProps<TValue>>
}

export type FieldConfig<TValue = unknown> =
  BaseFieldConfig<TValue> | CustomFieldConfig<TValue>

export type FormConfig = Record<string, FieldConfig>

export interface FieldRendererProps<T = unknown> {
  name: string
  value: T
  onChange: (value: T) => void
  onBlur?: () => void
  config: FieldComponentConfig
  required: boolean
  disabled: boolean
  /** Detail/read-only mode: renderers that can't be value-blocked (e.g. remote-select)
   * should show a read-only representation instead of an interactive control. */
  readonly?: boolean
  error?: string
  labelId?: string
  errorId?: string
  describedBy?: string
  /** All current form values — used by dependency-aware fields (e.g. remote-select). */
  values?: Record<string, unknown>
}

export type FieldRegistry = Record<FieldType, ComponentType<FieldRendererProps>>

/**
 * The form's four business modes:
 * - add    create: blank form (ignores initialData), everything editable
 * - edit   edit: prefilled from initialData, editable
 * - detail view: prefilled from initialData, forced read-only
 * - copy   copy-create: prefilled from initialData, but `copy:false` fields reset to their default, editable
 */
export type FormMode = "add" | "edit" | "detail" | "copy"

export interface UseAutoFormOptions {
  fields: FormConfig
  schema?: ZodType<Record<string, unknown>>
  initialData?: Record<string, unknown>
  mode?: FormMode
  onSubmit?: (data: Record<string, unknown>) => void | Promise<void>
  /** Callback when onSubmit throws (for toast/rollback); the error is no longer swallowed */
  onError?: (error: unknown) => void
}

export interface UseAutoFormReturn {
  values: Record<string, unknown>
  errors: Record<string, string | undefined>
  touched: Record<string, boolean>
  setValue: (name: string, value: unknown) => void
  setValues: (
    data: Record<string, unknown>,
    options?: { isCopy?: boolean }
  ) => void
  reset: () => void
  /** Validate all visible fields (async, supports remote rules). */
  validate: () => Promise<boolean>
  /** Validate a single field (used for real-time validation on blur). */
  validateField: (name: string) => Promise<void>
  getValues: () => Record<string, unknown>
  handleSubmit: (e?: React.FormEvent) => Promise<void>
  isSubmitting: boolean
}

export interface AutoFormProps {
  fields: FormConfig
  schema?: ZodType<Record<string, unknown>>
  initialData?: Record<string, unknown>
  /** Business mode; determines prefill and read-only behavior (detail forces read-only). Defaults to 'add'. */
  mode?: FormMode
  readonly?: boolean
  hideLabel?: boolean
  onSubmit?: (data: Record<string, unknown>) => void | Promise<void>
  /** Callback when onSubmit throws */
  onError?: (error: unknown) => void
  onChange?: (values: Record<string, unknown>) => void
  className?: string
  renderers?: Partial<FieldRegistry>
  children?: ReactNode
}

export function evaluate<T>(
  prop: Evaluable<T>,
  values?: Record<string, unknown>
): T {
  if (typeof prop === "function") {
    const fn = prop as (values?: Record<string, unknown>) => T
    return fn(values)
  }
  return prop
}

export function isVisible(
  field: FieldConfig,
  values: Record<string, unknown>
): boolean {
  // Titles honor `show` too (so a section header hides with its section); with no `show`
  // set, evaluate() yields undefined → treated as visible, keeping existing titles shown.
  const show = evaluate(field.show, values)
  return show !== false
}

export function isRequired(
  field: FieldConfig,
  values: Record<string, unknown>
): boolean {
  return evaluate(field.required, values) === true
}

export function isDisabled(
  field: FieldConfig,
  readonly?: boolean,
  values?: Record<string, unknown>
): boolean {
  if (readonly) return true
  return evaluate(field.disabled, values) === true
}

export function getFieldLabel(
  field: FieldConfig,
  values?: Record<string, unknown>
): ReactNode {
  return evaluate(field.name, values)
}

export function normalizeOptions(
  options?: SelectOption[] | Record<string, string>
): SelectOption[] {
  if (!options) return []
  if (Array.isArray(options)) return options
  return Object.entries(options).map(([value, label]) => ({ value, label }))
}
