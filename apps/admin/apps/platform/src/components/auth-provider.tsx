import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react"
import { iamApi, type LoginResult, type Session } from "../lib/iam-api"
import { getPasskeyCredential } from "../lib/webauthn"
import { AuthContext, type AuthStatus } from "./auth"

export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<Session | null>(null)
  const [status, setStatus] = useState<AuthStatus>("loading")

  const refresh = useCallback(async () => {
    try {
      setSession(await iamApi.session())
      setStatus("authenticated")
    } catch {
      setSession(null)
      setStatus("anonymous")
    }
  }, [])

  useEffect(() => {
    // 平台是本地单用户工具,无登录体系:跳过挂载时的会话探测,直接匿名。
    // 避免对不存在的 /api/authn/session 发请求产生 404 噪音。
    setStatus("anonymous")
  }, [])

  const finishLogin = useCallback(
    async (result: LoginResult) => {
      if (result.status !== "authenticated") return result
      await refresh()
      window.location.assign(result.return_url)
      return result
    },
    [refresh]
  )

  const login = useCallback(
    async (loginName: string, password: string, returnUrl: string) => {
      const result = await iamApi.login({
        login_name: loginName.trim(),
        password,
        return_url: returnUrl,
      })
      return finishLogin(result)
    },
    [finishLogin]
  )

  const verifyLoginMfa = useCallback(
    async (challengeId: string, code: string) =>
      finishLogin(
        await iamApi.verifyLoginMfa({ challenge_id: challengeId, code })
      ),
    [finishLogin]
  )

  const loginWithPasskey = useCallback(
    async (returnUrl: string) => {
      const options = await iamApi.beginPasskeyLogin(returnUrl)
      const credential = await getPasskeyCredential(options.options)
      return finishLogin(
        await iamApi.finishPasskeyLogin({
          challenge_id: options.challenge_id,
          credential,
        })
      )
    },
    [finishLogin]
  )

  const logout = useCallback(async () => {
    await iamApi.logout().catch(() => undefined)
    setSession(null)
    setStatus("anonymous")
    window.location.assign("/login")
  }, [])

  const value = useMemo(
    () => ({
      session,
      status,
      login,
      verifyLoginMfa,
      loginWithPasskey,
      logout,
      refresh,
    }),
    [session, status, login, verifyLoginMfa, loginWithPasskey, logout, refresh]
  )
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
