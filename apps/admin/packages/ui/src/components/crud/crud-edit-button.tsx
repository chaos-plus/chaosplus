import { useTranslations } from "use-intl"

import { Button } from "../button"
import type { FormMode } from "../auto-form/types"

export interface CrudEditButtonProps {
  mode: FormMode
  editable?: boolean
  submitting?: boolean
  /** Detail mode: navigate to the edit page. */
  onEdit?: () => void
  submitLabel?: string
  editLabel?: string
}

/**
 * Context-aware action button (= CommonFormEditButton.vue):
 * - add/edit/copy → a submit button that triggers the form's onSubmit
 * - detail        → an "edit" button that navigates to the edit page
 */
export function CrudEditButton({
  mode,
  editable = true,
  submitting,
  onEdit,
  submitLabel,
  editLabel,
}: CrudEditButtonProps) {
  const t = useTranslations("crud")
  if (editable === false) return null

  const submitText = submitLabel ?? t("submit")
  const editText = editLabel ?? t("edit")

  // Sticky bottom toolbar (= CommonFormEditButton.vue q-page-sticky): stays pinned to the
  // bottom of the scroll area so the action is always reachable without scrolling to the end.
  const bar =
    "sticky bottom-0 z-30 -mx-1 mt-6 border-t bg-background/90 px-1 py-3 backdrop-blur supports-[backdrop-filter]:bg-background/75"

  // Universal theme tokens only (present in every theme): submit = solid primary action,
  // detail's edit = outline. (success/warning vars are theme-optional, so avoided here.)
  if (mode === "detail") {
    return (
      <div className={bar}>
        <Button
          type="button"
          variant="outline"
          onClick={onEdit}
          className="w-full"
        >
          {editText}
        </Button>
      </div>
    )
  }

  return (
    <div className={bar}>
      <Button type="submit" disabled={submitting} className="w-full">
        {submitText}
      </Button>
    </div>
  )
}
