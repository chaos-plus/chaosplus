import { useEffect } from "react"
import { EditorContent, useEditor } from "@tiptap/react"
import StarterKit from "@tiptap/starter-kit"
import Underline from "@tiptap/extension-underline"
import Link from "@tiptap/extension-link"
import Image from "@tiptap/extension-image"
import TextAlign from "@tiptap/extension-text-align"
import Youtube from "@tiptap/extension-youtube"
import {
  AlignCenter,
  AlignLeft,
  AlignRight,
  Bold,
  Code,
  Heading1,
  Heading2,
  Heading3,
  Image as ImageIcon,
  Italic,
  Link2,
  List,
  ListOrdered,
  Minus,
  Quote,
  Redo,
  Strikethrough,
  Underline as UnderlineIcon,
  Undo,
  Video,
} from "lucide-react"

import { cn } from "@workspace/ui/lib/utils"

import type { FieldRendererProps } from "./types"

const btn =
  "inline-flex size-7 items-center justify-center rounded text-muted-foreground hover:bg-muted disabled:opacity-40 data-[active=true]:bg-muted data-[active=true]:text-foreground"
const Sep = () => <span className="mx-0.5 h-6 w-px self-center bg-border" />

/** Rich text editor backed by Tiptap. Stores HTML. Full toolbar incl. headings, alignment,
 *  links, images (by URL) and video (YouTube embed). */
export function RichTextFieldRenderer(props: FieldRendererProps) {
  const { value, onChange, disabled, error, errorId, labelId } = props
  const editor = useEditor({
    immediatelyRender: false,
    editable: !disabled,
    extensions: [
      StarterKit,
      Underline,
      Link.configure({ openOnClick: false, autolink: true }),
      Image,
      TextAlign.configure({ types: ["heading", "paragraph"] }),
      Youtube.configure({ width: 480, height: 270, nocookie: true }),
    ],
    content: (value as string) || "",
    onUpdate: ({ editor }) => onChange(editor.getHTML()),
    editorProps: {
      attributes: {
        class:
          "prose prose-sm dark:prose-invert max-w-none min-h-[200px] px-3 py-2 focus:outline-none [&_img]:rounded-md",
        "aria-labelledby": labelId ?? "",
      },
    },
  })

  useEffect(() => {
    if (!editor) return
    const html = (value as string) || ""
    if (!editor.isFocused && html !== editor.getHTML())
      editor.commands.setContent(html)
  }, [value, editor])

  useEffect(() => {
    editor?.setEditable(!disabled)
  }, [disabled, editor])

  if (!editor) return <div className="min-h-[200px] rounded-md border" />

  const setLink = () => {
    const prev = editor.getAttributes("link").href as string | undefined
    const url = window.prompt("链接地址 URL", prev ?? "https://")
    if (url === null) return
    if (url === "") {
      editor.chain().focus().unsetLink().run()
      return
    }
    editor.chain().focus().extendMarkRange("link").setLink({ href: url }).run()
  }
  const addImage = () => {
    const url = window.prompt("图片地址 URL", "https://")
    if (url) editor.chain().focus().setImage({ src: url }).run()
  }
  const addVideo = () => {
    const url = window.prompt("视频地址（YouTube URL）", "https://")
    if (url) editor.commands.setYoutubeVideo({ src: url })
  }

  return (
    <div className="w-full">
      <div
        className={cn(
          "overflow-hidden rounded-md border",
          error && "border-destructive"
        )}
      >
        {!disabled ? (
          <div className="flex flex-wrap gap-0.5 border-b bg-muted/40 p-1">
            <button
              type="button"
              className={btn}
              data-active={editor.isActive("heading", { level: 1 })}
              onClick={() =>
                editor.chain().focus().toggleHeading({ level: 1 }).run()
              }
              aria-label="h1"
            >
              <Heading1 className="size-4" />
            </button>
            <button
              type="button"
              className={btn}
              data-active={editor.isActive("heading", { level: 2 })}
              onClick={() =>
                editor.chain().focus().toggleHeading({ level: 2 }).run()
              }
              aria-label="h2"
            >
              <Heading2 className="size-4" />
            </button>
            <button
              type="button"
              className={btn}
              data-active={editor.isActive("heading", { level: 3 })}
              onClick={() =>
                editor.chain().focus().toggleHeading({ level: 3 }).run()
              }
              aria-label="h3"
            >
              <Heading3 className="size-4" />
            </button>
            <Sep />
            <button
              type="button"
              className={btn}
              data-active={editor.isActive("bold")}
              onClick={() => editor.chain().focus().toggleBold().run()}
              aria-label="bold"
            >
              <Bold className="size-4" />
            </button>
            <button
              type="button"
              className={btn}
              data-active={editor.isActive("italic")}
              onClick={() => editor.chain().focus().toggleItalic().run()}
              aria-label="italic"
            >
              <Italic className="size-4" />
            </button>
            <button
              type="button"
              className={btn}
              data-active={editor.isActive("underline")}
              onClick={() => editor.chain().focus().toggleUnderline().run()}
              aria-label="underline"
            >
              <UnderlineIcon className="size-4" />
            </button>
            <button
              type="button"
              className={btn}
              data-active={editor.isActive("strike")}
              onClick={() => editor.chain().focus().toggleStrike().run()}
              aria-label="strike"
            >
              <Strikethrough className="size-4" />
            </button>
            <button
              type="button"
              className={btn}
              data-active={editor.isActive("code")}
              onClick={() => editor.chain().focus().toggleCode().run()}
              aria-label="code"
            >
              <Code className="size-4" />
            </button>
            <Sep />
            <button
              type="button"
              className={btn}
              data-active={editor.isActive("bulletList")}
              onClick={() => editor.chain().focus().toggleBulletList().run()}
              aria-label="bullet list"
            >
              <List className="size-4" />
            </button>
            <button
              type="button"
              className={btn}
              data-active={editor.isActive("orderedList")}
              onClick={() => editor.chain().focus().toggleOrderedList().run()}
              aria-label="ordered list"
            >
              <ListOrdered className="size-4" />
            </button>
            <button
              type="button"
              className={btn}
              data-active={editor.isActive("blockquote")}
              onClick={() => editor.chain().focus().toggleBlockquote().run()}
              aria-label="quote"
            >
              <Quote className="size-4" />
            </button>
            <button
              type="button"
              className={btn}
              onClick={() => editor.chain().focus().setHorizontalRule().run()}
              aria-label="divider"
            >
              <Minus className="size-4" />
            </button>
            <Sep />
            <button
              type="button"
              className={btn}
              data-active={editor.isActive({ textAlign: "left" })}
              onClick={() => editor.chain().focus().setTextAlign("left").run()}
              aria-label="align left"
            >
              <AlignLeft className="size-4" />
            </button>
            <button
              type="button"
              className={btn}
              data-active={editor.isActive({ textAlign: "center" })}
              onClick={() =>
                editor.chain().focus().setTextAlign("center").run()
              }
              aria-label="align center"
            >
              <AlignCenter className="size-4" />
            </button>
            <button
              type="button"
              className={btn}
              data-active={editor.isActive({ textAlign: "right" })}
              onClick={() => editor.chain().focus().setTextAlign("right").run()}
              aria-label="align right"
            >
              <AlignRight className="size-4" />
            </button>
            <Sep />
            <button
              type="button"
              className={btn}
              data-active={editor.isActive("link")}
              onClick={setLink}
              aria-label="link"
            >
              <Link2 className="size-4" />
            </button>
            <button
              type="button"
              className={btn}
              onClick={addImage}
              aria-label="image"
            >
              <ImageIcon className="size-4" />
            </button>
            <button
              type="button"
              className={btn}
              onClick={addVideo}
              aria-label="video"
            >
              <Video className="size-4" />
            </button>
            <Sep />
            <button
              type="button"
              className={btn}
              onClick={() => editor.chain().focus().undo().run()}
              aria-label="undo"
            >
              <Undo className="size-4" />
            </button>
            <button
              type="button"
              className={btn}
              onClick={() => editor.chain().focus().redo().run()}
              aria-label="redo"
            >
              <Redo className="size-4" />
            </button>
          </div>
        ) : null}
        <EditorContent editor={editor} />
      </div>
      {error ? (
        <p id={errorId} className="mt-1 text-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}
