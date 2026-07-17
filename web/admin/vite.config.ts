import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: { port: 5173 },
  preview: { port: 4173 },
	test: {
		include: ['src/**/*.test.{ts,tsx}'],
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
		coverage: {
			provider: 'v8', reporter: ['text', 'html'], include: ['src/api.ts'],
			thresholds: { statements: 90, lines: 90, functions: 90, branches: 80 },
		},
  },
})
