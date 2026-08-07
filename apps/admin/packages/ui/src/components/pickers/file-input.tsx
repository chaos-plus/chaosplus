import * as React from "react"
import { File, Upload, X } from "lucide-react"

import { cn } from "@workspace/ui/lib/utils"
import { Button } from "@workspace/ui/components/button"

export interface FileInputProps {
  /** Selected files (controlled) */
  value?: FileList | File[] | null
  onChange?: (files: FileList | null) => void
  accept?: string
  multiple?: boolean
  disabled?: boolean
  /** Button text; the caller passes in an i18n-translated string */
  buttonLabel?: string
  /** Placeholder hint shown when no file is selected */
  placeholder?: string
  /** Show the list of selected files */
  showFileList?: boolean
  className?: string
}

function FileInput({
  value,
  onChange,
  accept,
  multiple = false,
  disabled = false,
  buttonLabel = "Browse files",
  placeholder = "No file selected",
  showFileList = false,
  className,
}: FileInputProps) {
  const inputRef = React.useRef<HTMLInputElement>(null)
  const isControlled = value !== undefined
  const [internalFiles, setInternalFiles] = React.useState<File[]>([])

  const files: File[] = isControlled
    ? value
      ? Array.from(value)
      : []
    : internalFiles

  const handleButtonClick = () => {
    if (!disabled) {
      inputRef.current?.click()
    }
  }

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const incoming = e.target.files

    if (!isControlled) {
      const next = incoming ? Array.from(incoming) : []
      if (multiple) {
        setInternalFiles((prev) => {
          const existingNames = new Set(prev.map((f) => f.name))
          const added = next.filter((f) => !existingNames.has(f.name))
          return [...prev, ...added]
        })
      } else {
        setInternalFiles(next)
      }
    }

    onChange?.(incoming)
    // Reset so the same file can be re-selected
    e.target.value = ""
  }

  const removeFile = (fileName: string) => {
    if (disabled) return

    const next = files.filter((f) => f.name !== fileName)

    if (!isControlled) {
      setInternalFiles(next)
    }

    if (onChange) {
      if (next.length === 0) {
        onChange(null)
      } else {
        // Build a synthetic DataTransfer to hand back a FileList
        const dt = new DataTransfer()
        next.forEach((f) => dt.items.add(f))
        onChange(dt.files)
      }
    }
  }

  const displayName =
    !showFileList && files.length === 1
      ? files[0]!.name
      : !showFileList && files.length > 1
        ? `${files.length} files`
        : null

  return (
    <div className={cn("flex flex-col gap-2", className)}>
      {/* Trigger row */}
      <div className="flex items-center gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled}
          onClick={handleButtonClick}
          className="shrink-0"
        >
          <Upload />
          {buttonLabel}
        </Button>

        {!showFileList && (
          <span
            className={cn(
              "truncate text-sm",
              displayName ? "text-foreground" : "text-muted-foreground"
            )}
          >
            {displayName ?? placeholder}
          </span>
        )}
      </div>

      {/* File list (showFileList mode) */}
      {showFileList && files.length > 0 && (
        <ul className="flex flex-col gap-1">
          {files.map((file) => (
            <li
              key={file.name}
              className="flex items-center gap-2 rounded-md border px-2 py-1.5 text-sm"
            >
              <File className="size-4 shrink-0 text-muted-foreground" />
              <span className="min-w-0 flex-1 truncate">{file.name}</span>
              <button
                type="button"
                disabled={disabled}
                onClick={() => removeFile(file.name)}
                aria-label={`Remove ${file.name}`}
                className="shrink-0 rounded-sm text-muted-foreground transition-colors hover:text-foreground disabled:pointer-events-none disabled:opacity-50"
              >
                <X className="size-4" />
              </button>
            </li>
          ))}
        </ul>
      )}

      {/* Hidden native input */}
      <input
        ref={inputRef}
        type="file"
        className="sr-only"
        accept={accept}
        multiple={multiple}
        disabled={disabled}
        onChange={handleInputChange}
        tabIndex={-1}
        aria-hidden
      />
    </div>
  )
}

export { FileInput }
