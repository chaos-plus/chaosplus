import { useRef, useState } from "react"
import { useTranslations } from "use-intl"
import { Plus } from "lucide-react"

import { Label } from "../label"
import { FileInput } from "@workspace/ui/components/pickers/file-input"
import { cn } from "@workspace/ui/lib/utils"
import { DemoSection } from "./_section"

function LocalSection({
  title,
  children,
}: {
  title: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-3">
      <h4 className="text-sm font-semibold text-muted-foreground">{title}</h4>
      {children}
    </div>
  )
}

export function FileUploadDemo() {
  const t = useTranslations("showcase.demos.fileUpload")

  return (
    <div className="space-y-8">
      <DemoSection titleKey="single">
        <div className="grid w-full max-w-sm gap-1.5">
          <Label>{t("upload")}</Label>
          <FileInput buttonLabel={t("browse")} placeholder={t("noFile")} />
        </div>
      </DemoSection>

      <DemoSection titleKey="dropZone">
        <DropZoneDemo />
      </DemoSection>

      <LocalSection title={t("multiple")}>
        <div className="grid w-full max-w-sm gap-1.5">
          <Label>{t("upload")}</Label>
          <FileInput
            multiple
            buttonLabel={t("browse")}
            placeholder={t("noFile")}
            showFileList
          />
        </div>
      </LocalSection>

      <LocalSection title={t("fileTypes")}>
        <div className="grid w-full max-w-sm gap-4">
          <div className="grid gap-1.5">
            <Label>{t("imageOnly")}</Label>
            <FileInput
              accept="image/*"
              buttonLabel={t("browse")}
              placeholder={t("noFile")}
            />
          </div>
          <div className="grid gap-1.5">
            <Label>{t("pdfOnly")}</Label>
            <FileInput
              accept=".pdf"
              buttonLabel={t("browse")}
              placeholder={t("noFile")}
            />
          </div>
        </div>
      </LocalSection>
    </div>
  )
}

function DropZoneDemo() {
  const t = useTranslations("showcase.demos.fileUpload")
  const [dragActive, setDragActive] = useState(false)
  const [droppedFiles, setDroppedFiles] = useState<FileList | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    setDragActive(true)
  }

  const handleDragLeave = (e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    setDragActive(false)
  }

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    setDragActive(false)
    if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
      setDroppedFiles(e.dataTransfer.files)
    }
  }

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files && e.target.files.length > 0) {
      setDroppedFiles(e.target.files)
    }
  }

  return (
    <div className="grid w-full max-w-sm gap-1.5">
      <label
        onDragOver={handleDragOver}
        onDragEnter={handleDragOver}
        onDragLeave={handleDragLeave}
        onDrop={handleDrop}
        className={cn(
          "flex cursor-pointer flex-col items-center justify-center rounded-lg border border-dashed px-6 py-10 transition-colors",
          dragActive
            ? "border-primary bg-primary/10"
            : "border-border bg-muted/50 hover:bg-muted"
        )}
      >
        <Plus
          className={cn(
            "mb-2 size-8",
            dragActive ? "text-primary" : "text-muted-foreground"
          )}
        />
        <span className="text-sm font-medium">{t("dropHere")}</span>
        <span className="text-xs text-muted-foreground">{t("supports")}</span>
        <input
          ref={inputRef}
          type="file"
          className="hidden"
          onChange={handleChange}
        />
      </label>
      {droppedFiles && droppedFiles[0] && (
        <p className="text-sm text-muted-foreground">
          {t("selected")}: {droppedFiles[0].name}
        </p>
      )}
    </div>
  )
}
