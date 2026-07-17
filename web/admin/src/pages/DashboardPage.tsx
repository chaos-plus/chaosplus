import { useEffect, useState } from 'react'
import { Boxes, KeyRound, ShieldCheck, Users } from 'lucide-react'
import { api } from '../api'
import { PageHeader } from '../components'
import { useSession } from '../auth'

export default function DashboardPage() {
  const session = useSession()
  const [counts, setCounts] = useState({ members: 0, roles: 0, menus: 0, permissions: 0 })
  useEffect(() => { Promise.all([api.members(), api.roles(), api.menus(), api.permissions()]).then(([members, roles, menus, permissions]) => setCounts({ members: members.length, roles: roles.length, menus: menus.length, permissions: permissions.length })).catch(() => undefined) }, [])
  return <><PageHeader title="访问控制概览" description={`欢迎，${session.preferred_username ?? session.email ?? session.subject}`} />
    <section className="metrics" aria-label="租户指标">
      <Metric icon={<Users />} label="租户成员" value={counts.members} tone="green" />
      <Metric icon={<KeyRound />} label="角色" value={counts.roles} tone="blue" />
      <Metric icon={<ShieldCheck />} label="权限动作" value={counts.permissions} tone="red" />
      <Metric icon={<Boxes />} label="菜单节点" value={counts.menus} tone="amber" />
    </section>
    <section className="activity-band"><div><h2>授权状态</h2><p>身份由 Zitadel 验证，接口决策由 SpiceDB 实时计算。</p></div><span className="health"><i />运行中</span></section>
  </>
}

function Metric({ icon, label, value, tone }: { icon: React.ReactNode; label: string; value: number; tone: string }) {
  return <div className="metric"><span className={`metric-icon ${tone}`}>{icon}</span><div><small>{label}</small><strong>{value}</strong></div></div>
}
