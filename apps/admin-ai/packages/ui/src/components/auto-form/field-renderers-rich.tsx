import { useEffect, useId, useMemo, useRef, useState } from "react"
import { useTranslations } from "use-intl"
import { toast } from "sonner"
import { ImagePlus, PlayCircle, Plus, Trash2, X } from "lucide-react"

import { cn } from "@workspace/ui/lib/utils"

import { useApi } from "../api-provider"
import type { ApiClient } from "@workspace/ui/lib/api-client"
import { Button } from "../button"
import { Checkbox } from "../checkbox"
import { Input } from "../input"
import { SimpleSelect } from "../select"
import { ReadonlyBox } from "./readonly-value"
import {
  evaluate,
  type FieldComponentConfig,
  type FieldRendererProps,
  type SelectOption,
} from "./types"

/** Loads {label,value} options from config.url, reloading when config.dependsOn fields change. */
function useRemoteOptions(
  api: ApiClient,
  config: FieldComponentConfig,
  values?: Record<string, unknown>
): SelectOption[] {
  const url = evaluate(config.url, values)
  const params = useMemo(
    () => (evaluate(config.params, values) ?? {}) as Record<string, unknown>,
    [config.params, values]
  )
  const labelKey = (config.labelKey as string | undefined) ?? "name"
  const valueKey = (config.valueKey as string | undefined) ?? "id"
  const depsKey = useMemo(
    () =>
      (config.dependsOn ?? []).map((d) => String(values?.[d] ?? "")).join("|"),
    [config.dependsOn, values]
  )
  const paramsKey = useMemo(() => JSON.stringify(params), [params])
  const [options, setOptions] = useState<SelectOption[]>([])
  useEffect(() => {
    if (!url) return
    let cancelled = false
    api
      .list<Record<string, unknown>>(url, { limit: 999, filter: params })
      .then((res) => {
        if (cancelled) return
        setOptions(
          res.list.map((it) => ({
            value: String(it[valueKey] ?? ""),
            label: String(it[labelKey] ?? ""),
          }))
        )
      })
      .catch(() => {
        if (!cancelled) setOptions([])
      })
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, depsKey, paramsKey])
  return options
}

const inputErrorClass = "border-destructive focus-visible:ring-destructive"

/** PriceField stores the value in integer cents but displays/edits yuan. */
export function PriceFieldRenderer(props: FieldRendererProps) {
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
  const decimals = (config.decimals as number | undefined) ?? 2
  const factor = 10 ** decimals
  const display =
    value === "" || value == null ? "" : String(Number(value) / factor)

  const handle = (raw: string) => {
    if (raw === "") {
      onChange("")
      return
    }
    const n = Number(raw)
    if (Number.isNaN(n)) {
      onChange(raw) // keep raw so validation can flag it
      return
    }
    onChange(Math.round(n * factor))
  }

  return (
    <div className="w-full">
      <div className="relative flex items-stretch">
        {config.prefix ? (
          <span className="absolute top-1/2 left-3 -translate-y-1/2 text-xs text-muted-foreground">
            {evaluate(config.prefix, values)}
          </span>
        ) : null}
        <Input
          id={id}
          name={name}
          inputMode="decimal"
          value={display}
          onChange={(e) => handle(e.target.value)}
          disabled={disabled}
          aria-labelledby={labelId}
          aria-describedby={describedBy}
          placeholder={config.placeholder as string | undefined}
          className={cn(
            config.prefix ? "pl-16" : "",
            config.suffix ? "pr-10" : "",
            error && inputErrorClass
          )}
        />
        {config.suffix ? (
          <span className="absolute top-1/2 right-3 -translate-y-1/2 text-xs text-muted-foreground">
            {evaluate(config.suffix, values)}
          </span>
        ) : null}
      </div>
      {error ? (
        <p id={errorId} className="mt-1 text-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}

/** BoolSelect maps a boolean value to a two-option select. */
export function BoolSelectFieldRenderer(props: FieldRendererProps) {
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
  const t = useTranslations("form")
  const current = value === true || value === "true" ? "true" : "false"
  const trueLabel = evaluate(config.trueLabel) ?? t("yes")
  const falseLabel = evaluate(config.falseLabel) ?? t("no")
  return (
    <div className="w-full">
      <SimpleSelect
        options={[
          { value: "true", label: trueLabel },
          { value: "false", label: falseLabel },
        ]}
        value={current}
        onValueChange={(v) => onChange(v === "true")}
        disabled={disabled}
        className={cn(
          "w-full",
          error && "border-destructive focus:ring-destructive"
        )}
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

/**
 * RemoteSelect loads its options from a backend endpoint (config.url), optionally
 * filtered by config.params and reloaded when any config.dependsOn field changes.
 * This single field type replaces every business SELECTION_* component from the legacy admin.
 */
export function RemoteSelectFieldRenderer(props: FieldRendererProps) {
  const {
    value,
    onChange,
    config,
    disabled,
    readonly,
    error,
    labelId,
    errorId,
    describedBy,
    values,
  } = props
  const options = useRemoteOptions(useApi(), config, values)
  const current = value == null || value === "" ? "" : String(value)

  // Detail mode: show the resolved label as a read-only box, not a (disabled) dropdown.
  if (readonly) {
    const opt = options.find((o) => o.value === current)
    return <ReadonlyBox text={opt ? String(opt.label) : current} chevron />
  }

  return (
    <div className="w-full">
      <SimpleSelect
        options={options}
        value={current}
        onValueChange={(v) => onChange(v)}
        disabled={disabled}
        placeholder={config.placeholder as string | undefined}
        className={cn(
          "w-full",
          error && "border-destructive focus:ring-destructive"
        )}
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

/** Remote multi-select: loads options from config.url, value = array of id strings. */
export function RemoteMultiSelectFieldRenderer(props: FieldRendererProps) {
  const {
    value,
    onChange,
    config,
    disabled,
    readonly,
    error,
    errorId,
    labelId,
    values,
  } = props
  const t = useTranslations("form")
  const options = useRemoteOptions(useApi(), config, values)
  const selected = useMemo(
    () => (Array.isArray(value) ? (value as unknown[]).map(String) : []),
    [value]
  )
  const toggle = (v: string) => {
    onChange(
      selected.includes(v) ? selected.filter((s) => s !== v) : [...selected, v]
    )
  }
  // Detail mode: show the resolved labels as a read-only box.
  if (readonly) {
    const labels = selected.map((s) => {
      const opt = options.find((o) => o.value === s)
      return opt ? String(opt.label) : s
    })
    return <ReadonlyBox text={labels.join("、")} chevron />
  }
  return (
    <div className="w-full">
      <div
        className="flex flex-wrap gap-3 rounded-md border p-2"
        aria-labelledby={labelId}
      >
        {options.length === 0 ? (
          <span className="text-xs text-muted-foreground">
            {t("noOptions")}
          </span>
        ) : (
          options.map((opt) => (
            <label key={opt.value} className="flex items-center gap-2 text-sm">
              <Checkbox
                checked={selected.includes(opt.value)}
                onCheckedChange={() => toggle(opt.value)}
                disabled={disabled}
              />
              {opt.label}
            </label>
          ))
        )}
      </div>
      {error ? (
        <p id={errorId} className="mt-1 text-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}

interface RelationItem {
  id: string
  count: number
}

function toRelationItems(
  value: unknown,
  valueKey: string,
  countKey: string
): RelationItem[] {
  if (!Array.isArray(value)) return []
  return (value as Record<string, unknown>[]).map((it) => ({
    id: String(it[valueKey] ?? it.id ?? ""),
    count: Number(it[countKey] ?? it.count ?? 1),
  }))
}

/**
 * Relation-list: rows of (remote-select + count), with add/remove — the React equivalent of
 * the combination-range (GoodsCombinationSelector) / gift-pack (FormGiftPacksSelector) widgets. Value is an array of
 * { [valueKey]: id, [countKey]: count } so the factory can JSON-encode it directly.
 */
export function RelationListFieldRenderer(props: FieldRendererProps) {
  const { value, onChange, config, disabled, error, errorId, labelId, values } =
    props
  const t = useTranslations("form")
  const options = useRemoteOptions(useApi(), config, values)
  const valueKey = (config.valueKey as string | undefined) ?? "id"
  const countKey = (config.countKey as string | undefined) ?? "count"
  const items = toRelationItems(value, valueKey, countKey)

  const emit = (next: RelationItem[]) => {
    onChange(next.map((it) => ({ [valueKey]: it.id, [countKey]: it.count })))
  }
  const setRow = (i: number, patch: Partial<RelationItem>) => {
    emit(items.map((it, idx) => (idx === i ? { ...it, ...patch } : it)))
  }

  return (
    <div className="w-full" aria-labelledby={labelId}>
      <div className="flex flex-col gap-2">
        {items.map((it, i) => (
          <div key={i} className="flex items-center gap-2">
            <SimpleSelect
              options={options}
              value={it.id}
              onValueChange={(v) => setRow(i, { id: v })}
              disabled={disabled}
              placeholder={t("select")}
              className="h-8 flex-1"
            />
            <Input
              type="number"
              className="h-8 w-24"
              value={String(it.count)}
              onChange={(e) =>
                setRow(i, { count: Number(e.target.value) || 0 })
              }
              disabled={disabled}
            />
            {!disabled ? (
              <Button
                type="button"
                variant="ghost"
                size="icon"
                onClick={() => emit(items.filter((_, idx) => idx !== i))}
                aria-label="remove"
              >
                <X className="size-4" />
              </Button>
            ) : null}
          </div>
        ))}
        {!disabled ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="self-start"
            onClick={() => emit([...items, { id: "", count: 1 }])}
          >
            <Plus className="size-4" />
            {t("add")}
          </Button>
        ) : null}
      </div>
      {error ? (
        <p id={errorId} className="mt-1 text-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}

interface UploadedFile {
  imageFileId: string
  imageFileUrl: string
  name?: string
  args?: { type?: string; duration?: number }
}

function toFileArray(value: unknown): UploadedFile[] {
  if (Array.isArray(value)) return value as UploadedFile[]
  if (value && typeof value === "object") return [value as UploadedFile]
  return []
}

/** Returns whether the uploaded object looks like a video. */
function isVideoFile(f: UploadedFile): boolean {
  if (f.args?.type?.startsWith("video/")) return true
  return /\.(mp4|mov|webm|avi|mkv)$/i.test(f.imageFileUrl)
}

interface UploadLimits {
  max_image_bytes: number
  max_video_bytes: number
  max_other_bytes: number
  allowed_exts?: string[]
  allowed_mimes?: string[]
}

function formatBytes(bytes: number): string {
  if (bytes >= 1024 * 1024) return `${Math.round(bytes / (1024 * 1024))}M`
  if (bytes >= 1024) return `${Math.round(bytes / 1024)}K`
  return `${bytes}B`
}

function acceptCategories(accept: string): ("image" | "video" | "other")[] {
  const cats: ("image" | "video" | "other")[] = []
  const a = accept.toLowerCase()
  const hasImage = a.includes("image/*") || /\bimage\//.test(a)
  const hasVideo = a.includes("video/*") || /\bvideo\//.test(a)
  if (hasImage) cats.push("image")
  if (hasVideo) cats.push("video")
  if (!hasImage && !hasVideo) cats.push("other")
  return cats
}

function maxBytesFor(
  cats: ("image" | "video" | "other")[],
  limits: UploadLimits
): number {
  let max = 0
  for (const cat of cats) {
    const v =
      cat === "image"
        ? limits.max_image_bytes
        : cat === "video"
          ? limits.max_video_bytes
          : limits.max_other_bytes
    if (v > max) max = v
  }
  return max || limits.max_other_bytes || limits.max_image_bytes
}

function fileExt(name: string): string {
  const i = name.lastIndexOf(".")
  return i >= 0 ? name.slice(i).toLowerCase() : ""
}

/**
 * ImageField — bordered uploader matching the legacy FileUploader/CommonFormImage: a
 * header (file picker + size hint + "N added, M max" counter), 110×110 thumbnails
 * (click to preview, red round delete), video play-icons, and drag-and-drop. Value is an
 * array of { imageFileId, imageFileUrl, args } (or a single object when multiple=false).
 */
export function ImageFieldRenderer(props: FieldRendererProps) {
  const { value, onChange, config, disabled, error, errorId, labelId } = props
  const t = useTranslations("form")
  const api = useApi()
  const inputRef = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)
  const [dragOver, setDragOver] = useState(false)
  const [limits, setLimits] = useState<UploadLimits | null>(null)

  useEffect(() => {
    let cancelled = false
    api
      .get<UploadLimits>("/sys/configs/file-storage/limits")
      .then((res) => {
        if (!cancelled && res.data) setLimits(res.data)
      })
      .catch(() => {
        // Limits are optional hints; failures fall back to the default labels.
      })
    return () => {
      cancelled = true
    }
  }, [api])

  const multiple = config.multiple === true
  const maxCount = multiple
    ? typeof config.maxFiles === "number"
      ? config.maxFiles
      : 5
    : 1
  const accept = (config.accept as string | undefined) ?? "image/*"
  const files = toFileArray(value)
  const canAdd = !disabled && files.length < maxCount

  const categories = useMemo(() => acceptCategories(accept), [accept])
  const maxBytes = useMemo(() => {
    if (limits) return maxBytesFor(categories, limits)
    return categories.includes("video") ? 1024 * 1024 * 1024 : 10 * 1024 * 1024
  }, [categories, limits])

  const sizeHintKey =
    categories.length === 1 && categories[0] === "video"
      ? "maxSizeVideo"
      : categories.length === 1 && categories[0] === "image"
        ? "maxSizeImage"
        : "maxSizeOther"

  const sizeHint = t(sizeHintKey, { size: formatBytes(maxBytes) })

  const emit = (next: UploadedFile[]) =>
    onChange(multiple ? next : (next[0] ?? null))

  const uploadFiles = async (list: File[]) => {
    if (list.length === 0) return
    const room = maxCount - files.length
    const toUpload = list.slice(0, Math.max(0, room))
    if (toUpload.length === 0) return

    for (const file of toUpload) {
      if (file.size > maxBytes) {
        toast.error(t("fileTooLarge", { size: formatBytes(maxBytes) }))
        return
      }
    }

    if (
      limits &&
      (limits.allowed_mimes?.length || limits.allowed_exts?.length)
    ) {
      for (const file of toUpload) {
        const mimeOk =
          !limits.allowed_mimes?.length ||
          limits.allowed_mimes.includes(file.type)
        const extOk =
          !limits.allowed_exts?.length ||
          limits.allowed_exts.includes(fileExt(file.name))
        if (!mimeOk && !extOk) {
          toast.error(t("fileTypeNotAllowed"))
          return
        }
      }
    }

    setBusy(true)
    try {
      const uploaded: UploadedFile[] = []
      for (const file of toUpload) {
        const res = await api.upload<UploadedFile>("/sys/files", file, {
          visibility: "public",
        })
        if (res.data) uploaded.push(res.data)
      }
      emit(multiple ? [...files, ...uploaded] : uploaded.slice(0, 1))
    } finally {
      setBusy(false)
      if (inputRef.current) inputRef.current.value = ""
    }
  }

  const remove = (id: string) => emit(files.filter((f) => f.imageFileId !== id))

  return (
    <div className="w-full" aria-labelledby={labelId}>
      <div
        className={cn(
          "min-h-[180px] rounded-md border transition-colors",
          dragOver && "border-primary bg-primary/5",
          error && "border-destructive"
        )}
        onDragOver={(e) => {
          if (!canAdd) return
          e.preventDefault()
          setDragOver(true)
        }}
        onDragLeave={() => setDragOver(false)}
        onDrop={(e) => {
          e.preventDefault()
          setDragOver(false)
          if (canAdd) void uploadFiles(Array.from(e.dataTransfer.files))
        }}
      >
        <div className="flex min-h-12 items-center gap-2 border-b px-3 py-2">
          {canAdd ? (
            <button
              type="button"
              disabled={busy}
              onClick={() => inputRef.current?.click()}
              className="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-sm hover:bg-muted disabled:opacity-50"
            >
              <ImagePlus className="size-4" />
              {busy ? t("uploading") : t("selectFile")}
              <span className="text-xs text-muted-foreground">{sizeHint}</span>
            </button>
          ) : (
            <span className="text-sm text-muted-foreground">
              {disabled ? t("readonly") : t("maxReached")}
            </span>
          )}
          <span className="ms-auto text-xs text-muted-foreground">
            {t("addedCount", { count: files.length, max: maxCount })}
          </span>
        </div>

        <div className="flex flex-wrap gap-2 p-2">
          {files.map((f) => (
            <div key={f.imageFileId} className="relative">
              {isVideoFile(f) ? (
                <div className="flex size-[110px] items-center justify-center rounded bg-muted">
                  <PlayCircle className="size-10 text-muted-foreground" />
                </div>
              ) : (
                <img
                  src={api.resolveAsset(f.imageFileUrl)}
                  alt={f.name ?? ""}
                  className="size-[110px] cursor-pointer rounded bg-muted object-contain"
                  onClick={() =>
                    window.open(api.resolveAsset(f.imageFileUrl), "_blank")
                  }
                />
              )}
              {!disabled ? (
                <button
                  type="button"
                  onClick={() => remove(f.imageFileId)}
                  className="absolute top-1 left-1 rounded-full bg-destructive p-1 text-white shadow hover:bg-destructive/85"
                  aria-label="remove"
                >
                  <Trash2 className="size-3" />
                </button>
              ) : null}
            </div>
          ))}
          {files.length === 0 ? (
            <div className="flex h-[110px] w-full items-center justify-center text-xs text-muted-foreground">
              {canAdd ? t("dropHint") : t("none")}
            </div>
          ) : null}
        </div>
      </div>
      <input
        ref={inputRef}
        type="file"
        accept={accept}
        multiple={multiple}
        hidden
        onChange={(e) => void uploadFiles(Array.from(e.target.files ?? []))}
      />
      {error ? (
        <p id={errorId} className="mt-1 text-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}

// RichText (Tiptap) and CodeEditor (CodeMirror) live in their own files so their heavy
// imports stay isolated; they are registered in auto-form.tsx alongside these renderers.
