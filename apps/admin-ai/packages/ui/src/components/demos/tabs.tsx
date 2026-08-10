import { useTranslations } from "use-intl"
import { Bell, Lock, User } from "lucide-react"

import { Input } from "../input"
import { Label } from "../label"
import { Switch } from "../switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "../tabs"
import { DemoSection } from "./_section"

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

export function TabsDemo() {
  const t = useTranslations("showcase.demos.tabs")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="default">
        <Tabs defaultValue="account">
          <TabsList>
            <TabsTrigger value="account">{t("account")}</TabsTrigger>
            <TabsTrigger value="password">{t("password")}</TabsTrigger>
            <TabsTrigger value="notifications">
              {t("notifications")}
            </TabsTrigger>
          </TabsList>
          <TabsContent value="account" className="space-y-2">
            <Label htmlFor="demo-name">{t("displayName")}</Label>
            <Input id="demo-name" defaultValue="Alice Chen" />
          </TabsContent>
          <TabsContent value="password" className="space-y-2">
            <Label htmlFor="demo-password">{t("newPassword")}</Label>
            <Input id="demo-password" type="password" />
          </TabsContent>
          <TabsContent value="notifications">
            <div className="flex items-center justify-between">
              <Label htmlFor="demo-push">{t("push")}</Label>
              <Switch id="demo-push" />
            </div>
          </TabsContent>
        </Tabs>
      </DemoSection>

      <LocalSection title={t("withIcons")}>
        <Tabs defaultValue="overview">
          <TabsList>
            <TabsTrigger value="overview">
              <span className="flex items-center gap-2">
                <User className="size-4" />
                {t("overview")}
              </span>
            </TabsTrigger>
            <TabsTrigger value="security">
              <span className="flex items-center gap-2">
                <Lock className="size-4" />
                {t("security")}
              </span>
            </TabsTrigger>
            <TabsTrigger value="alerts">
              <span className="flex items-center gap-2">
                <Bell className="size-4" />
                {t("notifications")}
              </span>
            </TabsTrigger>
          </TabsList>
          <TabsContent value="overview" className="space-y-2">
            <Label htmlFor="demo-fullname">{t("displayName")}</Label>
            <Input id="demo-fullname" defaultValue="Alice Chen" />
          </TabsContent>
          <TabsContent value="security" className="space-y-2">
            <Label htmlFor="demo-new-password">{t("newPassword")}</Label>
            <Input id="demo-new-password" type="password" />
          </TabsContent>
          <TabsContent value="alerts">
            <div className="flex items-center justify-between">
              <Label htmlFor="demo-push-icons">{t("push")}</Label>
              <Switch id="demo-push-icons" />
            </div>
          </TabsContent>
        </Tabs>
      </LocalSection>

      <LocalSection title={t("disabled")}>
        <Tabs defaultValue="account">
          <TabsList>
            <TabsTrigger value="account">{t("account")}</TabsTrigger>
            <TabsTrigger value="password">{t("password")}</TabsTrigger>
            <TabsTrigger value="disabledTab" disabled>
              {t("disabledTab")}
            </TabsTrigger>
          </TabsList>
          <TabsContent value="account" className="space-y-2">
            <Label htmlFor="demo-name-dis">{t("displayName")}</Label>
            <Input id="demo-name-dis" defaultValue="Alice Chen" />
          </TabsContent>
          <TabsContent value="password" className="space-y-2">
            <Label htmlFor="demo-password-dis">{t("newPassword")}</Label>
            <Input id="demo-password-dis" type="password" />
          </TabsContent>
        </Tabs>
      </LocalSection>
    </div>
  )
}
