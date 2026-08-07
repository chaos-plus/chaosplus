import type { ReactNode } from "react"

export function Alert({ children }: { children: ReactNode }) {
  return (
    <div
      className="mb-4 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {children}
    </div>
  )
}
