import {
  BarChart3,
  CreditCard,
  FileText,
  Key,
  LayoutDashboard,
  Plug,
  ScrollText,
  Shield,
  Users,
  UsersRound,
} from "lucide-react"
import { useTranslations } from "use-intl"

import { Sidebar } from "@workspace/ui/components/layout/sidebar"

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

const sidebarGroups = [
  {
    titleKey: "overview",
    items: [
      {
        href: "/dashboard",
        labelKey: "dashboard",
        icon: <LayoutDashboard className="size-4" />,
      },
      {
        href: "/analytics",
        labelKey: "analytics",
        icon: <BarChart3 className="size-4" />,
      },
      {
        href: "/reports",
        labelKey: "reports",
        icon: <FileText className="size-4" />,
      },
    ],
  },
  {
    titleKey: "management",
    items: [
      {
        href: "/users",
        labelKey: "users",
        icon: <Users className="size-4" />,
        children: [
          {
            href: "/users/roles",
            labelKey: "roles",
            icon: <Shield className="size-4" />,
          },
          {
            href: "/users/permissions",
            labelKey: "permissions",
            icon: <Key className="size-4" />,
          },
          {
            href: "/users/teams",
            labelKey: "teams",
            icon: <UsersRound className="size-4" />,
          },
        ],
      },
      {
        href: "/billing",
        labelKey: "billing",
        icon: <CreditCard className="size-4" />,
      },
    ],
  },
  {
    titleKey: "system",
    items: [
      {
        href: "/integrations",
        labelKey: "integrations",
        icon: <Plug className="size-4" />,
      },
      {
        href: "/api-keys",
        labelKey: "apiKeys",
        icon: <Key className="size-4" />,
      },
      {
        href: "/audit-log",
        labelKey: "auditLog",
        icon: <ScrollText className="size-4" />,
      },
    ],
  },
]

export function SidebarDemo() {
  const t = useTranslations("showcase.demos.sidebar")
  const nav = useTranslations("navigation")

  return (
    <div className="space-y-8">
      <LocalSection title={t("overview")}>
        <div className="overflow-hidden rounded-lg border bg-background">
          <div className="flex min-h-[420px]">
            <Sidebar groups={sidebarGroups} className="block bg-muted/30" />
            <main className="flex min-w-0 flex-1 flex-col gap-4 p-5">
              <div className="space-y-1">
                <p className="text-sm font-medium">{t("overview")}</p>
                <p className="text-sm text-muted-foreground">
                  {t("description")}
                </p>
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                {[nav("users"), nav("billing")].map((label) => (
                  <div
                    key={label}
                    className="rounded-md border bg-card p-4 text-sm font-medium"
                  >
                    {label}
                  </div>
                ))}
              </div>
              <div className="flex-1 rounded-md border border-dashed bg-muted/20 p-4 text-sm text-muted-foreground">
                {t("description")}
              </div>
            </main>
          </div>
        </div>
      </LocalSection>
    </div>
  )
}
