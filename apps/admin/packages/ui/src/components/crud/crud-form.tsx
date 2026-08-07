import { useEffect, useMemo, useState } from "react"
import { useTranslations } from "use-intl"
import { toast } from "sonner"

import { AutoForm } from "../auto-form/auto-form"
import type { FormMode } from "../auto-form/types"
import { CrudEditButton } from "./crud-edit-button"
import type { FormFactory, Row } from "./types"

export interface CrudFormProps {
  factory: FormFactory
  mode: FormMode
  /** Entity id (edit/detail). */
  id?: string
  /** Source id to clone from (add?copyId=). */
  copyId?: string
  /** Called after a successful commit (page navigates back). */
  onSubmitted?: () => void
  /** Detail mode: navigate to the edit page. */
  onEdit?: () => void
  /** Optional success message override. */
  successMessage?: string
  /** When true, don't render the action button (page manages it externally). */
  hideActions?: boolean
  /** HTML id for the <form> element, for external submit buttons via form= attribute. */
  formId?: string
  /** Reports submitting state changes to parent. */
  onSubmittingChange?: (submitting: boolean) => void
}

/**
 * CrudForm is the factory-driven create/edit/detail/copy form — the React equivalent of
 * `CommonForm.vue`. It loads the entity (edit/detail/copy) via the factory, maps it to the
 * form shape, renders AutoForm in the right mode, and commits via the factory on submit.
 */
export function CrudForm({
  factory,
  mode,
  id,
  copyId,
  onSubmitted,
  onEdit,
  successMessage,
  hideActions,
  formId,
  onSubmittingChange,
}: CrudFormProps) {
  const t = useTranslations("crud")
  const fields = useMemo(() => factory.getFormDef(), [factory])

  const isCopy = mode === "copy" || (mode === "add" && !!copyId)
  const loadId = mode === "add" ? copyId : id

  // undefined = still loading; null = blank (add); object = prefilled.
  // Seed synchronously from loadId so we never reset state inside the effect: a missing loadId
  // is immediately "blank", a present one is "loading" until the async fetch resolves below.
  const [initialData, setInitialData] = useState<Row | null | undefined>(() =>
    loadId ? undefined : null
  )
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    onSubmittingChange?.(submitting)
  }, [submitting, onSubmittingChange])

  // Realign to the loading/blank state during render when loadId changes (avoids a setState
  // in the effect body), then let the effect own only the async fetch.
  const [lastLoadId, setLastLoadId] = useState(loadId)
  if (loadId !== lastLoadId) {
    setLastLoadId(loadId)
    setInitialData(loadId ? undefined : null)
  }

  useEffect(() => {
    if (!loadId) return
    let cancelled = false
    factory
      .getInfo(loadId)
      .then(async (raw) => {
        const data = factory.toLocalData ? await factory.toLocalData(raw) : raw
        if (!cancelled) setInitialData(data)
      })
      .catch(() => {
        if (!cancelled) setInitialData(null)
      })
    return () => {
      cancelled = true
    }
  }, [factory, loadId])

  const effectiveMode: FormMode = isCopy ? "copy" : mode

  const handleCommit = async (values: Row) => {
    const body = factory.toServerData ? factory.toServerData(values) : values
    setSubmitting(true)
    try {
      if (mode === "add") {
        await factory.commitAdd(body)
      } else {
        await factory.commitEdit(id ?? "", body)
      }
      toast.success(successMessage ?? t("success"))
      factory.commitCallback?.onSuccess?.()
      onSubmitted?.()
    } catch (error) {
      // the api client already surfaces the error message; just run the callback
      factory.commitCallback?.onError?.(error)
    } finally {
      setSubmitting(false)
    }
  }

  if (initialData === undefined) {
    return (
      <div className="p-6 text-sm text-muted-foreground">{t("loading")}</div>
    )
  }

  return (
    <AutoForm
      id={formId}
      fields={fields}
      mode={effectiveMode}
      initialData={initialData ?? undefined}
      onSubmit={handleCommit}
    >
      {!hideActions && (
        <CrudEditButton
          mode={mode}
          editable={factory.editable}
          submitting={submitting}
          onEdit={onEdit}
        />
      )}
    </AutoForm>
  )
}
