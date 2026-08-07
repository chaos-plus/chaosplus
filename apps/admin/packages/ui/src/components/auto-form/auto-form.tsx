import { useEffect, useId, useMemo } from "react"

import { cn } from "@workspace/ui/lib/utils"

import {
  CheckboxFieldRenderer,
  CustomFieldRenderer,
  DateFieldRenderer,
  DateRangeFieldRenderer,
  DateTimeFieldRenderer,
  DateTimeRangeFieldRenderer,
  MultiSelectFieldRenderer,
  RadioFieldRenderer,
  SelectFieldRenderer,
  SwitchFieldRenderer,
  TabsFieldRenderer,
  TextAreaFieldRenderer,
  TextFieldRenderer,
  TimeFieldRenderer,
  TimeRangeFieldRenderer,
  TitleFieldRenderer,
} from "./field-renderers"
import {
  BoolSelectFieldRenderer,
  ImageFieldRenderer,
  PriceFieldRenderer,
  RelationListFieldRenderer,
  RemoteMultiSelectFieldRenderer,
  RemoteSelectFieldRenderer,
} from "./field-renderers-rich"
import { CodeEditorFieldRenderer } from "./codemirror-field"
import { LocationFieldRenderer } from "./location-field"
import { RichTextFieldRenderer } from "./tiptap-field"
import { useAutoForm } from "./use-auto-form"
import { ReadonlyValue, isReadonlyBlockType } from "./readonly-value"
import {
  getFieldLabel,
  isDisabled,
  isRequired,
  isVisible,
  type AutoFormProps,
  type CustomFieldConfig,
  type FieldConfig,
  type FieldRegistry,
  type FieldRendererProps,
  type FieldType,
} from "./types"

/** Default column span for a 2-column layout. Compact control types sit side-by-side;
 * block-level editors and collections span the full row. */
function defaultColForType(type: FieldType): 1 | 2 {
  switch (type) {
    case "title":
    case "textarea":
    case "rich-text":
    case "code-editor":
    case "image":
    case "media":
    case "relation-list":
    case "custom":
    case "tabs":
    case "multi-select":
    case "checkbox-group":
    case "dateRange":
    case "datetimeRange":
    case "timeRange":
    case "location":
    case "remote-multi-select":
      return 2
    default:
      return 1
  }
}

const defaultRegistry: FieldRegistry = {
  text: TextFieldRenderer,
  textarea: TextAreaFieldRenderer,
  number: TextFieldRenderer,
  password: TextFieldRenderer,
  email: TextFieldRenderer,
  code: TextAreaFieldRenderer,
  select: SelectFieldRenderer,
  "multi-select": MultiSelectFieldRenderer,
  checkbox: CheckboxFieldRenderer,
  "checkbox-group": MultiSelectFieldRenderer,
  switch: SwitchFieldRenderer,
  radio: RadioFieldRenderer,
  date: DateFieldRenderer,
  dateRange: DateRangeFieldRenderer,
  datetime: DateTimeFieldRenderer,
  datetimeRange: DateTimeRangeFieldRenderer,
  time: TimeFieldRenderer,
  timeRange: TimeRangeFieldRenderer,
  tabs: TabsFieldRenderer,
  title: TitleFieldRenderer,
  price: PriceFieldRenderer,
  "rich-text": RichTextFieldRenderer,
  "code-editor": CodeEditorFieldRenderer,
  image: ImageFieldRenderer,
  media: ImageFieldRenderer,
  "remote-select": RemoteSelectFieldRenderer,
  "remote-multi-select": RemoteMultiSelectFieldRenderer,
  "relation-list": RelationListFieldRenderer,
  "bool-select": BoolSelectFieldRenderer,
  location: LocationFieldRenderer,
  custom: CustomFieldRenderer,
}

function FieldRow({
  fieldName,
  field,
  values,
  errors,
  readonly,
  hideLabel,
  registry,
  onChange,
  onValidateField,
  className,
}: {
  fieldName: string
  field: FieldConfig
  values: Record<string, unknown>
  errors: Record<string, string | undefined>
  readonly?: boolean
  hideLabel?: boolean
  registry: FieldRegistry
  onChange: (value: unknown) => void
  onValidateField: (name: string) => void
  className?: string
}) {
  const baseId = useId()
  const visible = isVisible(field, values)
  if (!visible) return null

  // dynamicType lets a field pick its renderer from current values at runtime.
  const effectiveType = field.dynamicType
    ? field.dynamicType(values)
    : field.type
  const Renderer = registry[effectiveType]
  if (!Renderer) return null

  const label = getFieldLabel(field, values)
  const required = isRequired(field, values)
  const disabled = isDisabled(field, readonly, values)
  const baseConfig = field.config ?? {}
  // title sections carry their text in `name`; surface it to the title renderer via config.label
  const config =
    field.type === "title"
      ? { ...baseConfig, label: baseConfig.label ?? label }
      : effectiveType === "number"
        ? { type: "number", ...baseConfig }
        : baseConfig
  const labelId = `${baseId}-label`
  const errorId = `${baseId}-error`

  const rendererProps: FieldRendererProps = {
    name: fieldName,
    value: values[fieldName],
    onChange,
    config,
    required,
    disabled,
    readonly,
    values,
    error: errors[fieldName],
    labelId: hideLabel || field.type === "title" ? undefined : labelId,
    errorId,
    describedBy: errors[fieldName] ? errorId : undefined,
  }

  return (
    <div className={cn("flex flex-col gap-1.5", className)}>
      {!hideLabel && field.type !== "title" && (
        <div className="flex items-center gap-1">
          {required && (
            <span aria-hidden className="text-destructive">
              *
            </span>
          )}
          <span id={labelId} className="text-sm font-medium">
            {label}
          </span>
        </div>
      )}
      {/* onBlur bubbles at the container (focusout); any input losing focus triggers single-field real-time validation */}
      <div
        className="min-w-0"
        onBlur={
          field.type === "title" ? undefined : () => onValidateField(fieldName)
        }
      >
        {readonly &&
        field.type !== "title" &&
        isReadonlyBlockType(effectiveType) ? (
          <ReadonlyValue
            type={effectiveType}
            config={config}
            value={values[fieldName]}
          />
        ) : (
          <Renderer {...rendererProps} />
        )}
      </div>
    </div>
  )
}

export function AutoForm({
  fields,
  schema,
  initialData,
  mode = "add",
  readonly,
  hideLabel,
  onSubmit,
  onError,
  onChange,
  className,
  renderers,
  children,
  id,
}: AutoFormProps & { id?: string }) {
  const registry = useMemo(
    () => ({ ...defaultRegistry, ...renderers }),
    [renderers]
  )

  // detail mode forces read-only
  const effectiveReadonly = readonly || mode === "detail"

  const form = useAutoForm({
    fields,
    schema,
    initialData,
    mode,
    onSubmit,
    onError,
  })
  const { values, errors, setValue, validateField } = form

  useEffect(() => {
    onChange?.(values)
  }, [values, onChange])

  return (
    <form
      id={id}
      className={cn(
        "grid grid-cols-1 gap-x-6 gap-y-4 md:grid-cols-2",
        className
      )}
      onSubmit={form.handleSubmit}
      noValidate
    >
      {Object.entries(fields).map(([fieldName, field]) => {
        const col = field.col ?? defaultColForType(field.type)
        const colClass =
          field.type === "title" || col >= 2 ? "md:col-span-2" : undefined

        if (field.type === "custom") {
          const CustomComponent = (field as CustomFieldConfig).component
          const visible = isVisible(field, values)
          if (!visible) return null
          const label = getFieldLabel(field, values)
          const required = isRequired(field, values)
          const disabled = isDisabled(field, effectiveReadonly, values)
          const labelId = `${fieldName}-custom-label`
          const errorId = `${fieldName}-custom-error`
          return (
            <div
              key={fieldName}
              className={cn("flex flex-col gap-1.5", colClass)}
            >
              {!hideLabel && (
                <div className="flex items-center gap-1">
                  {required && (
                    <span aria-hidden className="text-destructive">
                      *
                    </span>
                  )}
                  <span id={labelId} className="text-sm font-medium">
                    {label}
                  </span>
                </div>
              )}
              <div className="min-w-0">
                <CustomComponent
                  name={fieldName}
                  value={values[fieldName]}
                  onChange={(value: unknown) => setValue(fieldName, value)}
                  config={field.config ?? {}}
                  required={required}
                  disabled={disabled}
                  readonly={effectiveReadonly}
                  values={values}
                  error={errors[fieldName]}
                  labelId={hideLabel ? undefined : labelId}
                  errorId={errorId}
                  describedBy={errors[fieldName] ? errorId : undefined}
                />
              </div>
            </div>
          )
        }

        return (
          <FieldRow
            key={fieldName}
            fieldName={fieldName}
            field={field}
            values={values}
            errors={errors}
            readonly={effectiveReadonly}
            hideLabel={hideLabel}
            registry={registry}
            onChange={(value) => setValue(fieldName, value)}
            onValidateField={(name) => void validateField(name)}
            className={colClass}
          />
        )
      })}
      {children}
    </form>
  )
}

export { useAutoForm }
export { defaultRegistry }
