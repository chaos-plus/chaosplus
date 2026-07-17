export interface Envelope<T> {
  code: number
  message: string
  data: T
  meta: { page?: { offset: number; limit: number; count: number; total: number } }
}

export interface Session {
  subject: string
  spicedb_subject: string
  issuer: string
  preferred_username?: string
  email?: string
  organization_id?: string
}

export interface Permission {
  resource: string
  verb: string
  code: string
  scope: string
  summary: string
  menu?: boolean
}

export interface Role {
  id: string
  tenant_id: string
  name: string
  description: string
  created_at: string
  updated_at: string
}

export interface Member {
  tenant_id: string
  subject: string
  display_name: string
  email?: string
  status: 'active' | 'disabled'
  role_ids?: string[]
  created_at: string
  updated_at: string
  disabled_at?: string
}

export interface Menu {
  id: string
  tenant_id: string
  parent_id?: string
  label: string
  route?: string
  icon?: string
  sort_order: number
  permission_code?: string
  status: 'active' | 'disabled'
  children?: Menu[]
}
