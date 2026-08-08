import { Card, CardContent, CardHeader, CardTitle } from "@workspace/ui/components/card"
import { useAuth } from "../../../components/auth"

export default function HumansPage() {
  const { session } = useAuth()
  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">人类 Human</h1>
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">当前登录成员</CardTitle>
        </CardHeader>
        <CardContent className="space-y-1 text-sm">
          {session ? (
            <>
              <div>用户名: {session.preferred_username ?? session.subject}</div>
              <div>邮箱: {session.email ?? "—"}</div>
              <div>组织: {session.organization_id ?? "—"}</div>
            </>
          ) : (
            <div>未登录</div>
          )}
        </CardContent>
      </Card>
      <p className="text-sm text-muted-foreground">
        人类成员由 IAM 身份体系提供;频道可添加人类/Agent 成员,见「会话区」。
      </p>
    </div>
  )
}
