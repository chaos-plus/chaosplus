import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useTranslations } from "use-intl"

import {
  evaluate,
  isRequired,
  isVisible,
  type FormConfig,
  type FormMode,
  type UseAutoFormOptions,
  type UseAutoFormReturn,
} from "./types"

function isEmpty(value: unknown): boolean {
  if (value === undefined || value === null) return true
  if (typeof value === "string") return value.trim() === ""
  if (Array.isArray(value)) return value.length === 0
  return false
}

function buildInitialValues(
  fields: FormConfig,
  initialData?: Record<string, unknown>,
  mode: FormMode = "add"
): Record<string, unknown> {
  // add mode: ignore initialData, use each field's default value (blank new record)
  const usePrefill = mode !== "add" && initialData != null
  const values: Record<string, unknown> = {}
  for (const [key, field] of Object.entries(fields)) {
    if (field.type === "title") continue
    // copy mode: fields marked copy:false (e.g. a unique id) reset to their default value
    if (mode === "copy" && field.copy === false) {
      values[key] = field.value
      continue
    }
    values[key] = usePrefill ? (initialData[key] ?? field.value) : field.value
  }
  return values
}

export function useAutoForm(options: UseAutoFormOptions): UseAutoFormReturn {
  const {
    fields,
    schema,
    initialData,
    mode = "add",
    onSubmit,
    onError,
  } = options
  const t = useTranslations("form")

  const initialValues = useMemo(
    () => buildInitialValues(fields, initialData, mode),
    [fields, initialData, mode]
  )

  const [values, setValuesState] =
    useState<Record<string, unknown>>(initialValues)
  const [errors, setErrors] = useState<Record<string, string | undefined>>({})
  const [touched, setTouched] = useState<Record<string, boolean>>({})
  const [isSubmitting, setIsSubmitting] = useState(false)

  const originalDataRef = useRef<Record<string, unknown> | null>(null)

  useEffect(() => {
    originalDataRef.current = initialData ? { ...initialData } : null
  }, [initialData])

  const setValue = useCallback((name: string, value: unknown) => {
    setValuesState((prev) => ({ ...prev, [name]: value }))
    setErrors((prev) => ({ ...prev, [name]: undefined }))
    setTouched((prev) => ({ ...prev, [name]: true }))
  }, [])

  const setValues = useCallback(
    (data: Record<string, unknown>, options?: { isCopy?: boolean }) => {
      const next: Record<string, unknown> = {}
      for (const [key, field] of Object.entries(fields)) {
        if (field.type === "title") continue
        if (options?.isCopy && field.copy === false) {
          next[key] = field.value
          continue
        }
        next[key] = data[key] ?? field.value
      }
      setValuesState(next)
      setErrors({})
      setTouched({})
      originalDataRef.current = data ? { ...data } : null
    },
    [fields]
  )

  const reset = useCallback(() => {
    setValuesState(initialValues)
    setErrors({})
    setTouched({})
    originalDataRef.current = initialData ? { ...initialData } : null
  }, [initialValues, initialData])

  // Validate a single visible field, returning an error string (supports async rules/validate)
  const validateOne = useCallback(
    async (
      key: string,
      vals: Record<string, unknown>
    ): Promise<string | undefined> => {
      const field = fields[key]
      if (!field || field.type === "title") return undefined
      if (!isVisible(field, vals)) return undefined

      const value = vals[key]
      if (isRequired(field, vals) && isEmpty(value)) {
        const configuredMessage = field.config?.requiredMessage
        return configuredMessage
          ? evaluate(configuredMessage, vals)
          : t("required")
      }

      const config = field.config ?? {}
      if (config.rules) {
        for (const rule of config.rules) {
          const message = await rule(value, vals)
          if (message) return message
        }
      }
      if (config.validate) {
        const message = await config.validate(
          value,
          originalDataRef.current?.[key],
          vals
        )
        if (message) return message
      }
      return undefined
    },
    [fields, t]
  )

  const validate = useCallback(async (): Promise<boolean> => {
    const nextErrors: Record<string, string | undefined> = {}

    const entries = Object.keys(fields)
    const messages = await Promise.all(
      entries.map((key) => validateOne(key, values))
    )
    entries.forEach((key, i) => {
      if (messages[i]) nextErrors[key] = messages[i]
    })

    if (schema) {
      const result = schema.safeParse(values)
      if (!result.success) {
        const flattened = (
          result.error as {
            flatten?: () => {
              fieldErrors: Record<string, string[] | undefined>
            }
          }
        ).flatten?.()
        if (flattened) {
          for (const [key, messages] of Object.entries(flattened.fieldErrors)) {
            if (messages?.length && !nextErrors[key]) {
              nextErrors[key] = messages[0]
            }
          }
        }
      }
    }

    setErrors(nextErrors)
    return Object.values(nextErrors).every((e) => !e)
  }, [fields, schema, values, validateOne])

  // Real-time single-field validation on blur
  const validateField = useCallback(
    async (name: string): Promise<void> => {
      const message = await validateOne(name, values)
      setErrors((prev) => ({ ...prev, [name]: message }))
    },
    [validateOne, values]
  )

  const getValues = useCallback((): Record<string, unknown> => {
    const data: Record<string, unknown> = {}
    for (const [key, field] of Object.entries(fields)) {
      if (field.type === "title") continue
      if (!isVisible(field, values)) continue
      if (field.submit === false) continue
      data[key] = values[key]
    }
    return data
  }, [fields, values])

  const handleSubmit = useCallback(
    async (e?: React.FormEvent) => {
      e?.preventDefault()
      const ok = await validate()
      if (!ok) return
      if (!onSubmit) return
      setIsSubmitting(true)
      try {
        await onSubmit(getValues())
      } catch (err) {
        // Don't swallow submit errors: hand them to the caller (toast/rollback)
        onError?.(err)
      } finally {
        setIsSubmitting(false)
      }
    },
    [getValues, onSubmit, onError, validate]
  )

  return {
    values,
    errors,
    touched,
    setValue,
    setValues,
    reset,
    validate,
    validateField,
    getValues,
    handleSubmit,
    isSubmitting,
  }
}
