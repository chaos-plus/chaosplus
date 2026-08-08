export interface MembershipDates {
  starts_at?: string
  ends_at?: string
}

export function membershipState(
  member: MembershipDates,
  now = Date.now()
): "待生效" | "有效" | "已结束" {
  if (member.starts_at && Date.parse(member.starts_at) > now) return "待生效"
  if (member.ends_at && Date.parse(member.ends_at) <= now) return "已结束"
  return "有效"
}

export function toDateTimeLocal(value?: string): string {
  if (!value) return ""
  const date = new Date(value)
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 16)
}

export function toISOString(value: string): string | undefined {
  if (!value) return undefined
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString()
}

export function membershipWindow(member: MembershipDates): string {
  const format = (value?: string) =>
    value
      ? new Intl.DateTimeFormat("zh-CN", {
          dateStyle: "medium",
          timeStyle: "short",
        }).format(new Date(value))
      : "不限"
  return `${format(member.starts_at)} - ${format(member.ends_at)}`
}
