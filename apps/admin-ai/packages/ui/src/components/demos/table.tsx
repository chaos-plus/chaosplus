import { toast } from "sonner"
import { useTranslations } from "use-intl"
import { Pencil, Trash2 } from "lucide-react"

import { Badge } from "../badge"
import { Button } from "../button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "../table"
import { DemoSection } from "./_section"

export function TableDemo() {
  const t = useTranslations("showcase.demos.table")
  const users = [
    {
      name: "Alice Chen",
      email: "alice@example.com",
      role: t("admin"),
      status: t("active"),
    },
    {
      name: "Bob Smith",
      email: "bob@example.com",
      role: t("editor"),
      status: t("pending"),
    },
    {
      name: "Carol White",
      email: "carol@example.com",
      role: t("viewer"),
      status: t("active"),
    },
    {
      name: "David Kim",
      email: "david@example.com",
      role: t("editor"),
      status: t("offline"),
    },
  ]

  return (
    <div className="space-y-8">
      <DemoSection titleKey="userList">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("user")}</TableHead>
              <TableHead>{t("role")}</TableHead>
              <TableHead className="text-right">{t("status")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {users.map((user) => (
              <TableRow key={user.email}>
                <TableCell>
                  <div className="font-medium">{user.name}</div>
                  <div className="text-xs text-muted-foreground">
                    {user.email}
                  </div>
                </TableCell>
                <TableCell>{user.role}</TableCell>
                <TableCell className="text-right">
                  <Badge
                    variant={
                      user.status === t("active")
                        ? "default"
                        : user.status === t("pending")
                          ? "secondary"
                          : "outline"
                    }
                  >
                    {user.status}
                  </Badge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </DemoSection>

      <div className="space-y-3">
        <h4 className="text-sm font-semibold text-muted-foreground">
          {t("striped")}
        </h4>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("user")}</TableHead>
              <TableHead>{t("role")}</TableHead>
              <TableHead className="text-right">{t("status")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {users.map((user, index) => (
              <TableRow
                key={user.email}
                className={
                  index % 2 === 0
                    ? "bg-muted/30 transition-colors hover:bg-muted/60"
                    : "transition-colors hover:bg-muted/40"
                }
              >
                <TableCell>
                  <div className="font-medium">{user.name}</div>
                  <div className="text-xs text-muted-foreground">
                    {user.email}
                  </div>
                </TableCell>
                <TableCell>{user.role}</TableCell>
                <TableCell className="text-right">
                  <Badge
                    variant={
                      user.status === t("active")
                        ? "default"
                        : user.status === t("pending")
                          ? "secondary"
                          : "outline"
                    }
                  >
                    {user.status}
                  </Badge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      <div className="space-y-3">
        <h4 className="text-sm font-semibold text-muted-foreground">
          {t("withActions")}
        </h4>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("user")}</TableHead>
              <TableHead>{t("role")}</TableHead>
              <TableHead className="text-right">{t("status")}</TableHead>
              <TableHead className="w-[100px]" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {users.map((user) => (
              <TableRow key={user.email}>
                <TableCell>
                  <div className="font-medium">{user.name}</div>
                  <div className="text-xs text-muted-foreground">
                    {user.email}
                  </div>
                </TableCell>
                <TableCell>{user.role}</TableCell>
                <TableCell className="text-right">
                  <Badge
                    variant={
                      user.status === t("active")
                        ? "default"
                        : user.status === t("pending")
                          ? "secondary"
                          : "outline"
                    }
                  >
                    {user.status}
                  </Badge>
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex items-center justify-end gap-1">
                    <Button
                      size="icon"
                      variant="ghost"
                      onClick={() => toast.info(`${t("edit")}: ${user.name}`)}
                    >
                      <Pencil className="size-4" />
                    </Button>
                    <Button
                      size="icon"
                      variant="ghost"
                      onClick={() => toast.info(`${t("delete")}: ${user.name}`)}
                    >
                      <Trash2 className="size-4" />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
