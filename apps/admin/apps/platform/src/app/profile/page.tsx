import { useEffect, useState } from "react"
import { Avatar, AvatarFallback } from "@workspace/ui/components/avatar"
import { Button } from "@workspace/ui/components/button"
import { Card } from "@workspace/ui/components/card"
import { Input } from "@workspace/ui/components/input"
import { toast } from "@workspace/ui/components/sonner"

/** PRD D.6 个人中心(最小版)。本地单用户,偏好存本地;邮箱一经设置只读。 */
const STORAGE_KEY = "platform-profile"
const THEME_KEY = "platform-theme"
const LANG_KEY = "platform-lang"

interface Profile {
  email: string
  nickname: string
}

function readProfile(): Profile {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) return JSON.parse(raw) as Profile
  } catch {
    // 坏数据当作未设置,不要让整页崩掉。
  }
  return { email: "", nickname: "" }
}

export default function ProfilePage() {
  const [profile, setProfile] = useState<Profile>(() => readProfile())
  const [emailDraft, setEmailDraft] = useState("")
  const [nicknameDraft, setNicknameDraft] = useState("")
  const [theme, setTheme] = useState(() => localStorage.getItem(THEME_KEY) ?? "system")
  const [lang, setLang] = useState(() => localStorage.getItem(LANG_KEY) ?? "zh")

  useEffect(() => {
    setEmailDraft(profile.email)
    setNicknameDraft(profile.nickname)
  }, [profile])

  const emailLocked = profile.email !== ""

  const save = () => {
    const email = emailLocked ? profile.email : emailDraft.trim()
    if (!emailLocked && email && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
      toast.error("邮箱格式不正确")
      return
    }
    const next = { email, nickname: nicknameDraft.trim() }
    localStorage.setItem(STORAGE_KEY, JSON.stringify(next))
    setProfile(next)
    window.dispatchEvent(new Event("profile-change")) // 顶部头像即时更新
    toast.success("已保存")
  }

  const applyTheme = (value: string) => {
    setTheme(value)
    localStorage.setItem(THEME_KEY, value)
    const root = document.documentElement
    if (value === "system") root.removeAttribute("data-theme")
    else root.setAttribute("data-theme", value)
    root.classList.toggle(
      "dark",
      value === "dark" || (value === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches),
    )
  }

  const initial = (profile.nickname || profile.email || "U").slice(0, 1).toUpperCase()

  return (
    <div className="max-w-2xl space-y-4">
      <div>
        <h1 className="text-xl font-semibold">个人中心</h1>
        <p className="text-sm text-muted-foreground">本地单用户模式,资料与偏好保存在这台设备上。</p>
      </div>

      <Card className="p-4">
        <div className="flex items-center gap-3">
          <Avatar className="size-12">
            <AvatarFallback>{initial}</AvatarFallback>
          </Avatar>
          <div>
            <p className="font-medium">{profile.nickname || "未设置昵称"}</p>
            <p className="text-sm text-muted-foreground">{profile.email || "未设置邮箱"}</p>
          </div>
        </div>

        <div className="mt-4 grid gap-3">
          <div className="grid gap-1.5">
            <label htmlFor="pf-email" className="text-sm font-medium">邮箱</label>
            <Input
              id="pf-email"
              value={emailDraft}
              onChange={(e) => setEmailDraft(e.target.value)}
              disabled={emailLocked}
              placeholder="you@example.com"
            />
            <p className="text-xs text-muted-foreground">{emailLocked ? "邮箱设置后不可修改。" : "设置后将不可修改。"}</p>
          </div>
          <div className="grid gap-1.5">
            <label htmlFor="pf-nick" className="text-sm font-medium">昵称</label>
            <Input id="pf-nick" value={nicknameDraft} onChange={(e) => setNicknameDraft(e.target.value)} />
          </div>
          <div>
            <Button className="cursor-pointer" onClick={save}>
              保存
            </Button>
          </div>
        </div>
      </Card>

      <Card className="p-4">
        <p className="font-medium">偏好</p>
        <div className="mt-3 grid gap-3 sm:grid-cols-2">
          <div className="grid gap-1.5">
            <label htmlFor="pf-theme" className="text-sm font-medium">主题</label>
            <select
              id="pf-theme"
              value={theme}
              onChange={(e) => applyTheme(e.target.value)}
              className="h-9 cursor-pointer rounded-md border border-input bg-transparent px-2 text-sm"
            >
              <option value="system">跟随系统</option>
              <option value="light">浅色</option>
              <option value="dark">深色</option>
            </select>
          </div>
          <div className="grid gap-1.5">
            <label htmlFor="pf-lang" className="text-sm font-medium">语言</label>
            <select
              id="pf-lang"
              value={lang}
              onChange={(e) => {
                setLang(e.target.value)
                localStorage.setItem(LANG_KEY, e.target.value)
              }}
              className="h-9 cursor-pointer rounded-md border border-input bg-transparent px-2 text-sm"
            >
              <option value="zh">简体中文</option>
            </select>
            <p className="text-xs text-muted-foreground">v1 仅简体中文,其他语言随 i18n 一起放出。</p>
          </div>
        </div>
      </Card>

      <Card className="p-4">
        <p className="font-medium">修改密码</p>
        <p className="mt-1 text-sm text-muted-foreground">
          当前为本地单用户模式,没有账号体系,因此不提供密码修改。接入登录后这里会开放。
        </p>
      </Card>
    </div>
  )
}
