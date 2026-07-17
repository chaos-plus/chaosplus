import type { FormEvent, ReactNode } from 'react'
import { X } from 'lucide-react'

export function PageHeader({ title, description, action }: { title: string; description?: string; action?: ReactNode }) {
  return <header className="page-header"><div><h1>{title}</h1>{description && <p>{description}</p>}</div>{action}</header>
}

export function Badge({ status }: { status: string }) {
  return <span className={`badge badge-${status}`}>{status === 'active' ? '启用' : status === 'disabled' ? '停用' : status}</span>
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="empty">{children}</div>
}

export function Dialog({ title, open, onClose, children }: { title: string; open: boolean; onClose: () => void; children: ReactNode }) {
  if (!open) return null
  return <div className="dialog-backdrop" role="presentation" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
    <section className="dialog" role="dialog" aria-modal="true" aria-label={title}>
      <header><h2>{title}</h2><button className="icon-button" onClick={onClose} aria-label="关闭"><X size={18} /></button></header>
      {children}
    </section>
  </div>
}

export function FormActions({ busy, onCancel }: { busy: boolean; onCancel: () => void }) {
  return <div className="form-actions"><button type="button" className="button secondary" onClick={onCancel}>取消</button><button className="button primary" disabled={busy}>{busy ? '保存中' : '保存'}</button></div>
}

export function submitJSON(event: FormEvent<HTMLFormElement>): Record<string, string> {
  event.preventDefault()
  return Object.fromEntries(new FormData(event.currentTarget).entries()) as Record<string, string>
}
