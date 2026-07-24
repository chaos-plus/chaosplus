import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import LoginPage from './LoginPage'

describe('LoginPage', () => {
  it('renders direct login and the full Zitadel fallback', () => {
    const { rerender } = render(<MemoryRouter><LoginPage mode="login" /></MemoryRouter>)
    expect(screen.getByRole('heading', { name: '登录管理台' })).toBeVisible()
    expect(screen.getByLabelText('账号')).toHaveAttribute('autocomplete', 'username')
    expect(screen.getByLabelText('密码')).toHaveAttribute('autocomplete', 'current-password')
    expect(screen.getByRole('button', { name: '登录' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '使用 Passkey / MFA 安全登录' })).toHaveAttribute('href', expect.stringContaining('/authn/oidc/start'))
    rerender(<MemoryRouter><LoginPage mode="register" /></MemoryRouter>)
    expect(screen.getByRole('heading', { name: '创建管理账号' })).toBeVisible()
    expect(screen.getByRole('link', { name: '前往注册' })).toBeVisible()
  })

  it('directs forced-MFA accounts to the secure login flow', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ code: 409, message: 'additional_verification_required' }), { status: 409, headers: { 'Content-Type': 'application/json' } }))
    render(<MemoryRouter><LoginPage mode="login" /></MemoryRouter>)
    fireEvent.change(screen.getByLabelText('账号'), { target: { value: 'alice' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'secret' } })
    fireEvent.click(screen.getByRole('button', { name: '登录' }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('该账号需要多因素验证，请使用安全登录'))
  })
})
