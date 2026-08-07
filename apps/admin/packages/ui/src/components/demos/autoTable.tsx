import { toast } from "sonner"
import { useTranslations } from "use-intl"
import { Pencil, Trash2 } from "lucide-react"

import { Badge } from "../badge"
import { AutoTable } from "../auto-table/auto-table"
import type { TableOptions } from "../auto-table/types"
import { DemoSection } from "./_section"

export function AutoTableDemo() {
  const t = useTranslations("showcase.demos.autoTable")
  const users = [
    {
      id: "1",
      name: "Alice Chen",
      email: "alice@example.com",
      role: t("admin"),
      status: t("active"),
    },
    {
      id: "2",
      name: "Bob Smith",
      email: "bob@example.com",
      role: t("editor"),
      status: t("pending"),
    },
    {
      id: "3",
      name: "Carol White",
      email: "carol@example.com",
      role: t("viewer"),
      status: t("active"),
    },
    {
      id: "4",
      name: "David Kim",
      email: "david@example.com",
      role: t("editor"),
      status: t("offline"),
    },
  ]

  const statusBadge = (val: unknown) => (
    <Badge
      variant={
        val === t("active")
          ? "default"
          : val === t("pending")
            ? "secondary"
            : "outline"
      }
    >
      {String(val)}
    </Badge>
  )

  const baseColumns: TableOptions<(typeof users)[0]>["columns"] = [
    { field: "name", name: t("user") },
    { field: "email", name: t("email") },
    { field: "role", name: t("role") },
    { field: "status", name: t("status"), align: "right", format: statusBadge },
  ]

  const options: TableOptions<(typeof users)[0]> = {
    data: users,
    columns: baseColumns,
    actions: [
      { name: t("edit"), icon: Pencil, onclick: () => toast.info(t("edit")) },
      {
        name: t("delete"),
        icon: Trash2,
        onclick: () => toast.info(t("delete")),
      },
    ],
    selectable: true,
    selection: "multiple",
  }

  const sortOptions: TableOptions<(typeof users)[0]> = {
    data: users,
    columns: [
      { field: "name", name: t("user"), sortable: true },
      { field: "email", name: t("email"), sortable: true },
      { field: "role", name: t("role"), sortable: true },
      {
        field: "status",
        name: t("status"),
        align: "right",
        sortable: true,
        format: statusBadge,
      },
    ],
    defaultSort: { field: "name", direction: "asc" },
  }

  const emptyOptions: TableOptions<(typeof users)[0]> = {
    data: [],
    columns: baseColumns,
  }

  // Sticky demo: wide columns + many rows so header, left/right columns, and pagination all pin.
  const stickyRows = Array.from({ length: 14 }, (_, i) => ({
    ...users[i % users.length]!,
    id: String(i + 1),
  }))
  const stickyOptions: TableOptions<(typeof users)[0]> = {
    data: stickyRows,
    columns: [
      { field: "name", name: t("user"), width: "240px" },
      { field: "email", name: t("email"), width: "280px" },
      { field: "role", name: t("role"), width: "220px" },
      {
        field: "status",
        name: t("status"),
        width: "220px",
        format: statusBadge,
      },
    ],
    actions: [
      { name: t("edit"), icon: Pencil, onclick: () => toast.info(t("edit")) },
      {
        name: t("delete"),
        icon: Trash2,
        onclick: () => toast.info(t("delete")),
      },
    ],
    selectable: true,
    sticky: {
      header: true,
      pagination: true,
      left: 1,
      right: 1,
      maxHeight: 260,
    },
  }

  return (
    <div className="space-y-8">
      <DemoSection titleKey="configDriven">
        <AutoTable {...options} />
      </DemoSection>

      <div className="space-y-3">
        <h4 className="text-sm font-semibold text-muted-foreground">
          {t("withSort")}
        </h4>
        <p className="text-xs text-muted-foreground">{t("sorted")}</p>
        <AutoTable {...sortOptions} />
      </div>

      <div className="space-y-3">
        <h4 className="text-sm font-semibold text-muted-foreground">
          Sticky (粘性表头 / 首列 / 操作列 / 分页)
        </h4>
        <AutoTable {...stickyOptions} />
      </div>

      <div className="space-y-3">
        <h4 className="text-sm font-semibold text-muted-foreground">
          {t("emptyState")}
        </h4>
        <AutoTable {...emptyOptions} />
      </div>
    </div>
  )
}
