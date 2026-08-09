import { createBrowserRouter, Navigate, Outlet } from "react-router"
import { AuthProvider } from "./components/auth-provider"
import { PendingRoute } from "./components/pending-route"

export const router = createBrowserRouter([
  {
    element: <Outlet />,
    hydrateFallbackElement: <PendingRoute />,
    children: [
      { path: "/register", lazy: async () => ({ Component: (await import("./app/register/page")).default }) },
      { path: "/recover", lazy: async () => ({ Component: (await import("./app/recover/page")).default }) },
      { path: "/verify-email", lazy: async () => ({ Component: (await import("./app/verify-email/page")).default }) },
      { path: "/accept-invitation", lazy: async () => ({ Component: (await import("./app/accept-invitation/page")).default }) },
      {
        element: (
          <AuthProvider>
            <Outlet />
          </AuthProvider>
        ),
        children: [
          { path: "/login", lazy: async () => ({ Component: (await import("./app/login/page")).default }) },
          {
            lazy: async () => ({ Component: (await import("./app/layout")).default }),
            children: [
              { index: true, lazy: async () => ({ Component: (await import("./app/dashboard/page")).default }) },
              // 会话区
              { path: "/sessions", lazy: async () => ({ Component: (await import("./app/sessions/page")).default }) },
              { path: "/sessions/:channelId", lazy: async () => ({ Component: (await import("./app/sessions/page")).default }) },
              // 工作区(需求/任务/测试/缺陷/OKR)
              { path: "/workspace/:type", lazy: async () => ({ Component: (await import("./app/workspace/page")).default }) },
              { path: "/workspace/okrs", lazy: async () => ({ Component: (await import("./app/workspace/okrs")).default }) },
              // 个人中心(PRD D.6)
              { path: "/profile", lazy: async () => ({ Component: (await import("./app/profile/page")).default }) },
              // 团队管理
              { path: "/team/machines", lazy: async () => ({ Component: (await import("./app/team/machines/page")).default }) },
              { path: "/team/machines/:machineId", lazy: async () => ({ Component: (await import("./app/team/machines/detail")).default }) },
              { path: "/team/humans", lazy: async () => ({ Component: (await import("./app/team/humans/page")).default }) },
              { path: "/team/agents", lazy: async () => ({ Component: (await import("./app/team/agents/page")).default }) },
              // 工作流
              { path: "/workflow/runs", lazy: async () => ({ Component: (await import("./app/workflow/runs/page")).default }) },
              { path: "/workflow/runs/:runId", lazy: async () => ({ Component: (await import("./app/workflow/runs/detail")).default }) },
              { path: "/workflow/approvals", lazy: async () => ({ Component: (await import("./app/workflow/approvals/page")).default }) },
            ],
          },
        ],
      },
      { path: "*", element: <Navigate to="/" replace /> },
    ],
  },
])
