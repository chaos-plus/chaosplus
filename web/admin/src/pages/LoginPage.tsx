import { ArrowRight, Check, ShieldCheck } from 'lucide-react'
import { Link, useLocation } from 'react-router-dom'
import { oidcURL } from '../api'

export default function LoginPage({ mode }: { mode: 'login' | 'register' }) {
  const location = useLocation()
  const returnURL = `${window.location.origin}${(location.state as { from?: string } | null)?.from ?? '/'}`
  const register = mode === 'register'
  return <main className="auth-page">
    <section className="auth-panel">
      <div className="auth-brand"><span className="auth-mark"><ShieldCheck size={30} /></span><strong>Chaosplus</strong></div>
      <div className="auth-copy"><span className="eyebrow">IDENTITY & ACCESS</span><h1>{register ? '创建管理账号' : '登录管理台'}</h1><p>{register ? '通过 Chaosplus Identity 建立安全身份。' : '使用统一身份进入租户管理空间。'}</p></div>
      <a className="button primary auth-action" href={oidcURL(mode, returnURL)}>{register ? '前往注册' : '使用统一身份登录'}<ArrowRight size={18} /></a>
      <div className="auth-switch">{register ? <>已有账号？<Link to="/login">返回登录</Link></> : <>还没有账号？<Link to="/register">创建账号</Link></>}</div>
      <div className="auth-trust"><span><Check size={15} />统一会话</span><span><Check size={15} />多因素认证</span><span><Check size={15} />实时授权</span></div>
    </section>
    <aside className="auth-visual" aria-hidden="true"><div className="access-map"><span className="map-label">PLATFORM</span><div className="map-node node-platform">平台控制面</div><div className="map-line line-one" /><div className="map-node node-tenant">品牌租户</div><div className="map-line line-two" /><div className="map-row"><div className="map-node">商户</div><div className="map-node">部门</div></div><div className="map-row small"><div className="map-node">店铺</div><div className="map-node accent">权限关系</div></div></div></aside>
  </main>
}
