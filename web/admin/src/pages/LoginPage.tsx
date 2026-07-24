import { useState, type FormEvent } from 'react'
import { ArrowRight, Check, Eye, EyeOff, LockKeyhole, LogIn, ShieldCheck, UserRound } from 'lucide-react'
import { Link, useLocation } from 'react-router-dom'
import { api, ApiError, oidcURL } from '../api'

export default function LoginPage({ mode }: { mode: 'login' | 'register' }) {
  const location = useLocation()
  const returnURL = `${window.location.origin}${(location.state as { from?: string } | null)?.from ?? '/'}`
  const register = mode === 'register'
  const [passwordVisible, setPasswordVisible] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    setError('')
    const form = new FormData(event.currentTarget)
    try {
      const result = await api.login({
        login_name: String(form.get('login_name') ?? '').trim(),
        password: String(form.get('password') ?? ''),
        return_url: returnURL,
      })
      window.location.assign(result.return_url)
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 409) setError('该账号需要多因素验证，请使用安全登录')
      else if (cause instanceof ApiError && cause.status === 401) setError('账号或密码不正确')
      else setError('身份服务暂时不可用，请稍后重试')
    } finally {
      setBusy(false)
    }
  }

  return <main className="auth-page">
    <section className="auth-panel">
      <div className="auth-brand"><span className="auth-mark"><ShieldCheck size={30} /></span><strong>Chaosplus</strong></div>
      <div className="auth-copy"><span className="eyebrow">IDENTITY & ACCESS</span><h1>{register ? '创建管理账号' : '登录管理台'}</h1><p>{register ? '通过 Chaosplus Identity 建立安全身份。' : '使用组织账号进入租户管理空间。'}</p></div>
      {register ? <a className="button primary auth-action" href={oidcURL(mode, returnURL)}>前往注册<ArrowRight size={18} /></a> : <>
        <form className="auth-form" onSubmit={submit}>
          <div className="auth-field"><label htmlFor="login-name">账号</label><span className="auth-input"><UserRound size={18} /><input id="login-name" name="login_name" autoComplete="username" required maxLength={200} placeholder="name@example.com" /></span></div>
          <div className="auth-field"><label htmlFor="password">密码</label><span className="auth-input"><LockKeyhole size={18} /><input id="password" name="password" type={passwordVisible ? 'text' : 'password'} autoComplete="current-password" required maxLength={200} /><button type="button" className="password-toggle" onClick={() => setPasswordVisible((value) => !value)} aria-label={passwordVisible ? '隐藏密码' : '显示密码'}>{passwordVisible ? <EyeOff size={18} /> : <Eye size={18} />}</button></span></div>
          {error && <div className="auth-error" role="alert">{error}</div>}
          <button className="button primary auth-submit" type="submit" disabled={busy}>{busy ? <span className="button-spinner" /> : <LogIn size={18} />}{busy ? '正在验证' : '登录'}</button>
        </form>
        <div className="auth-divider"><span>或</span></div>
        <a className="button secondary auth-federated" href={oidcURL('login', returnURL)}><ShieldCheck size={18} />使用 Passkey / MFA 安全登录</a>
      </>}
      <div className="auth-switch">{register ? <>已有账号？<Link to="/login">返回登录</Link></> : <>还没有账号？<Link to="/register">创建账号</Link></>}</div>
      <div className="auth-trust"><span><Check size={15} />统一会话</span><span><Check size={15} />多因素认证</span><span><Check size={15} />实时授权</span></div>
    </section>
    <aside className="auth-visual" aria-hidden="true"><div className="access-map"><span className="map-label">PLATFORM</span><div className="map-node node-platform">平台控制面</div><div className="map-line line-one" /><div className="map-node node-tenant">品牌租户</div><div className="map-line line-two" /><div className="map-row"><div className="map-node">商户</div><div className="map-node">部门</div></div><div className="map-row small"><div className="map-node">店铺</div><div className="map-node accent">权限关系</div></div></div></aside>
  </main>
}
