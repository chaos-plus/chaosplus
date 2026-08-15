import { useEffect, useMemo, useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import {
  Bold,
  Eye,
  FileText,
  Italic,
  Link,
  List,
  LoaderCircle,
  Paperclip,
  Pencil,
  Trash2,
} from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Textarea } from "@workspace/ui/components/textarea";
import { controlApi, type Attachment } from "../../lib/control-api";

function useAttachmentURL(id: string) {
  const [state, setState] = useState({
    id: "",
    url: "",
    failed: false,
  });
  useEffect(() => {
    if (!id) return;
    let active = true;
    let objectURL = "";
    void controlApi
      .attachmentContent(id)
      .then((blob) => {
        objectURL = URL.createObjectURL(blob);
        if (active) setState({ id, url: objectURL, failed: false });
      })
      .catch(() => {
        if (active) setState({ id, url: "", failed: true });
      });
    return () => {
      active = false;
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
  }, [id]);
  return { ...state, loading: Boolean(id) && state.id !== id };
}

function attachmentID(src?: string) {
  const match = src?.match(/^\/control\/api\/attachments\/([^/]+)\/content$/);
  return match ? decodeURIComponent(match[1]) : "";
}

function MarkdownImage({ src, alt }: { src?: string; alt?: string }) {
  const id = attachmentID(src);
  const state = useAttachmentURL(id);
  if (!id) return <img src={src} alt={alt ?? ""} />;
  if (state.loading)
    return (
      <span className="grid h-32 w-full place-items-center rounded-md border bg-muted/40">
        <LoaderCircle className="size-5 animate-spin text-muted-foreground" />
      </span>
    );
  if (state.failed)
    return (
      <span
        role="img"
        aria-label={alt || "图片加载失败"}
        className="block rounded-md border p-3 text-sm text-destructive"
      >
        图片加载失败
      </span>
    );
  return <img src={state.url} alt={alt ?? ""} />;
}

export function ProtectedAttachmentMedia({
  attachment,
  className,
}: {
  attachment: Attachment;
  className: string;
}) {
  const state = useAttachmentURL(attachment.id);
  if (state.loading)
    return (
      <span className={`${className} grid place-items-center bg-muted/40`}>
        <LoaderCircle className="size-5 animate-spin text-muted-foreground" />
      </span>
    );
  if (state.failed)
    return (
      <span
        className={`${className} grid place-items-center bg-muted text-xs text-destructive`}
      >
        加载失败
      </span>
    );
  if (attachment.mime.startsWith("video/"))
    return (
      <video src={state.url} className={className} muted preload="metadata" />
    );
  return (
    <img
      src={state.url}
      alt={attachment.filename}
      loading="lazy"
      className={className}
    />
  );
}

export function MarkdownView({ value }: { value: string }) {
  if (!value.trim())
    return <p className="text-sm text-muted-foreground">无内容</p>;
  return (
    <div className="min-w-0 space-y-2 break-words text-sm leading-6 [&_a]:text-primary [&_a]:underline [&_blockquote]:border-l-2 [&_blockquote]:pl-3 [&_code]:rounded [&_code]:bg-muted [&_code]:px-1 [&_h1]:text-xl [&_h1]:font-semibold [&_h2]:text-lg [&_h2]:font-semibold [&_img]:max-h-80 [&_img]:max-w-full [&_img]:rounded-md [&_img]:border [&_li]:ml-5 [&_ol]:list-decimal [&_p]:whitespace-pre-wrap [&_ul]:list-disc">
      <ReactMarkdown components={{ img: MarkdownImage }}>{value}</ReactMarkdown>
    </div>
  );
}

export function RichContentEditor({
  id,
  label,
  value,
  onChange,
  rows = 8,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (value: string) => void;
  rows?: number;
}) {
  const [preview, setPreview] = useState(false);
  const ref = useRef<HTMLTextAreaElement>(null);

  const insert = (before: string, after: string, fallback: string) => {
    const element = ref.current;
    const start = element?.selectionStart ?? value.length;
    const end = element?.selectionEnd ?? value.length;
    const selected = value.slice(start, end) || fallback;
    const next = `${value.slice(0, start)}${before}${selected}${after}${value.slice(end)}`;
    onChange(next);
    requestAnimationFrame(() => {
      element?.focus();
      element?.setSelectionRange(
        start + before.length,
        start + before.length + selected.length,
      );
    });
  };

  return (
    <div className="grid gap-1.5">
      <div className="flex min-h-11 flex-wrap items-center gap-1">
        <label htmlFor={id} className="mr-auto text-sm font-medium">
          {label}
        </label>
        {!preview && (
          <>
            <Button
              type="button"
              size="icon"
              variant="ghost"
              className="size-10"
              title="加粗"
              aria-label="加粗"
              onClick={() => insert("**", "**", "文本")}
            >
              <Bold className="size-4" />
            </Button>
            <Button
              type="button"
              size="icon"
              variant="ghost"
              className="size-10"
              title="斜体"
              aria-label="斜体"
              onClick={() => insert("*", "*", "文本")}
            >
              <Italic className="size-4" />
            </Button>
            <Button
              type="button"
              size="icon"
              variant="ghost"
              className="size-10"
              title="列表"
              aria-label="列表"
              onClick={() => insert("- ", "", "列表项")}
            >
              <List className="size-4" />
            </Button>
            <Button
              type="button"
              size="icon"
              variant="ghost"
              className="size-10"
              title="链接"
              aria-label="链接"
              onClick={() => insert("[", "](https://)", "链接文字")}
            >
              <Link className="size-4" />
            </Button>
          </>
        )}
        <Button
          type="button"
          size="icon"
          variant={preview ? "secondary" : "ghost"}
          className="size-10"
          title={preview ? "编辑" : "预览"}
          aria-label={preview ? "编辑 Markdown" : "预览 Markdown"}
          onClick={() => setPreview((value) => !value)}
        >
          {preview ? <Pencil className="size-4" /> : <Eye className="size-4" />}
        </Button>
      </div>
      {preview ? (
        <div className="min-h-40 rounded-md border bg-background p-3">
          <MarkdownView value={value} />
        </div>
      ) : (
        <Textarea
          ref={ref}
          id={id}
          rows={rows}
          value={value}
          onChange={(event) => onChange(event.target.value)}
        />
      )}
    </div>
  );
}

export function AttachmentQueue({
  id,
  files,
  onChange,
  disabled = false,
}: {
  id: string;
  files: File[];
  onChange: (files: File[]) => void;
  disabled?: boolean;
}) {
  const input = useRef<HTMLInputElement>(null);
  const previews = useMemo(
    () =>
      files.map((file) => ({
        file,
        url: file.type.startsWith("image/") ? URL.createObjectURL(file) : "",
      })),
    [files],
  );
  useEffect(
    () => () =>
      previews.forEach((value) => value.url && URL.revokeObjectURL(value.url)),
    [previews],
  );

  return (
    <div className="grid gap-2">
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-medium">图片与附件</span>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="min-h-11 gap-2"
          disabled={disabled}
          onClick={() => input.current?.click()}
        >
          <Paperclip className="size-4" />
          添加文件
        </Button>
        <input
          ref={input}
          id={id}
          type="file"
          multiple
          className="hidden"
          aria-label="添加图片与附件"
          onChange={(event) => {
            onChange([...files, ...Array.from(event.target.files ?? [])]);
            event.target.value = "";
          }}
        />
      </div>
      {previews.length > 0 && (
        <div className="grid gap-2 sm:grid-cols-2">
          {previews.map(({ file, url }, index) => (
            <div
              key={`${file.name}-${file.lastModified}-${index}`}
              className="flex min-w-0 items-center gap-2 rounded-md border p-2"
            >
              {url ? (
                <img
                  src={url}
                  alt={file.name}
                  className="size-12 shrink-0 rounded object-cover"
                />
              ) : (
                <span className="grid size-12 shrink-0 place-items-center rounded bg-muted">
                  <FileText className="size-5 text-muted-foreground" />
                </span>
              )}
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{file.name}</p>
                <p className="text-xs text-muted-foreground">
                  {(file.size / 1024).toFixed(1)} KB
                </p>
              </div>
              <Button
                type="button"
                size="icon"
                variant="ghost"
                className="size-10 shrink-0 text-muted-foreground hover:text-destructive"
                aria-label={`移除 ${file.name}`}
                onClick={() =>
                  onChange(files.filter((_, position) => position !== index))
                }
              >
                <Trash2 className="size-4" />
              </Button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
