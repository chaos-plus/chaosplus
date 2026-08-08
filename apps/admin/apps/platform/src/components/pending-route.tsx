export function PendingRoute() {
  return (
    <div
      className="grid min-h-svh place-items-center bg-background text-sm text-muted-foreground"
      aria-busy="true"
      aria-live="polite"
    >
      正在加载
    </div>
  )
}
