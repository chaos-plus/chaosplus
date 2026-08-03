import { useMemo } from "react"
import CodeMirror from "@uiw/react-codemirror"
import { json } from "@codemirror/lang-json"

import { cn } from "@workspace/ui/lib/utils"

import type { FieldRendererProps } from "./types"

/** Code editor backed by CodeMirror 6. config.lang selects the language (default json). */
export function CodeEditorFieldRenderer(props: FieldRendererProps) {
  const { value, onChange, config, disabled, error, errorId } = props
  const lang = (config.lang as string | undefined) ?? "json"

  const extensions = useMemo(() => (lang === "json" ? [json()] : []), [lang])

  return (
    <div className="w-full">
      <div
        className={cn(
          "overflow-hidden rounded-md border",
          error && "border-destructive"
        )}
      >
        <CodeMirror
          value={(value as string | undefined) ?? ""}
          height="240px"
          editable={!disabled}
          readOnly={disabled}
          extensions={extensions}
          onChange={(val) => onChange(val)}
          basicSetup={{
            lineNumbers: true,
            foldGutter: true,
            highlightActiveLine: !disabled,
          }}
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
