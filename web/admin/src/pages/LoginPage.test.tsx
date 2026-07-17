import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import LoginPage from './LoginPage'

describe('LoginPage', () => {
	it('renders login and registration entry points without handling credentials', () => {
		const { rerender } = render(<MemoryRouter><LoginPage mode="login" /></MemoryRouter>)
		expect(screen.getByRole('heading', { name: '登录管理台' })).toBeVisible()
		expect(screen.getByRole('link', { name: '使用统一身份登录' })).toHaveAttribute('href', expect.stringContaining('/authn/oidc/start'))
		rerender(<MemoryRouter><LoginPage mode="register" /></MemoryRouter>)
		expect(screen.getByRole('heading', { name: '创建管理账号' })).toBeVisible()
		expect(screen.getByRole('link', { name: '前往注册' })).toBeVisible()
	})
})
