/**
 * 浏览器通知:只在页面不在前台时打扰用户(前台时界面本身已经能看到)。
 * 权限由用户手势触发申请 —— 直接在加载时申请会被多数浏览器忽略。
 */

const PREF_KEY = "platform-notify"

export function notifySupported(): boolean {
  return typeof window !== "undefined" && "Notification" in window
}

export function notifyPermission(): NotificationPermission {
  return notifySupported() ? Notification.permission : "denied"
}

/** 用户是否开启了通知(权限已授予且没有手动关掉)。 */
export function notifyEnabled(): boolean {
  return notifyPermission() === "granted" && localStorage.getItem(PREF_KEY) !== "off"
}

export function setNotifyEnabled(on: boolean): void {
  localStorage.setItem(PREF_KEY, on ? "on" : "off")
}

/** 申请权限,必须由点击等用户手势调用。返回是否可用。 */
export async function requestNotifyPermission(): Promise<boolean> {
  if (!notifySupported()) return false
  if (Notification.permission === "granted") {
    setNotifyEnabled(true)
    return true
  }
  if (Notification.permission === "denied") return false
  const res = await Notification.requestPermission()
  const ok = res === "granted"
  if (ok) setNotifyEnabled(true)
  return ok
}

export interface NotifyOptions {
  title: string
  body: string
  /** 同一 tag 的通知会互相替换,避免同一频道刷屏。 */
  tag?: string
  /** 点击通知后跳转的应用内路径。 */
  url?: string
}

/** 页面在前台时不发通知。点击通知会聚焦窗口并跳转到对应页面。 */
export function notify({ title, body, tag, url }: NotifyOptions): void {
  if (!notifyEnabled()) return
  if (document.visibilityState === "visible") return
  try {
    const n = new Notification(title, { body, tag, icon: "/favicon.ico" })
    n.onclick = () => {
      window.focus()
      if (url) window.location.assign(url)
      n.close()
    }
  } catch {
    // 某些环境(未安装 PWA 的移动端)会直接抛错,通知失败不该影响主流程。
  }
}
