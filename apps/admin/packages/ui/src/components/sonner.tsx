import { Toaster as Sonner, toast } from "sonner"

import { cn } from "@workspace/ui/lib/utils"

type ToasterProps = React.ComponentProps<typeof Sonner>

function Toaster({ className, ...props }: ToasterProps) {
  return (
    <Sonner
      className={cn("toaster group", className)}
      richColors
      toastOptions={{
        classNames: {
          toast:
            "group toast group-[.toaster]:rounded-[var(--radius-lg)] group-[.toaster]:border-border group-[.toaster]:bg-popover group-[.toaster]:text-popover-foreground group-[.toaster]:shadow-lg",
          title: "group-[.toast]:font-medium",
          description: "group-[.toast]:opacity-80",
          actionButton:
            "group-[.toast]:rounded-[var(--radius-md)] group-[.toast]:border group-[.toast]:border-primary group-[.toast]:bg-primary group-[.toast]:text-primary-foreground group-[.toast]:shadow-sm group-[.toast]:hover:bg-primary/90",
          cancelButton:
            "group-[.toast]:rounded-[var(--radius-md)] group-[.toast]:border group-[.toast]:border-border group-[.toast]:bg-muted group-[.toast]:text-muted-foreground group-[.toast]:hover:bg-accent group-[.toast]:hover:text-accent-foreground",
          success: "toast-success",
          info: "toast-info",
          warning: "toast-warning",
          error: "toast-error",
          loading: "toast-loading",
        },
      }}
      {...props}
    />
  )
}

export { Toaster, toast }
