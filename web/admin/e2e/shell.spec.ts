import { expect, test } from '@playwright/test'

test('login and registration screens are responsive', async ({ page }) => {
  await page.goto('/login')
  await expect(page.getByRole('heading', { name: '登录管理台' })).toBeVisible()
  await expect(page.getByRole('link', { name: '使用统一身份登录' })).toHaveAttribute('href', /authn\/oidc\/start/)
  await page.getByRole('link', { name: '创建账号' }).click()
  await expect(page.getByRole('heading', { name: '创建管理账号' })).toBeVisible()
  await expect(page.locator('body')).not.toHaveCSS('overflow-x', 'scroll')
})
