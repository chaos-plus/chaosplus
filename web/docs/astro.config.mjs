// @ts-check
import 'dotenv/config';
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import mermaid from 'astro-mermaid';
import { sidebar } from './src/site-nav.mjs';

const protectedBuild = Boolean(process.env.DOC_PASSWORD);
const site = process.env.DOC_SITE_URL;
const sitemapDisabled = protectedBuild || !site;
const sitemapPlaceholder = { name: '@astrojs/sitemap', hooks: {} };

export default defineConfig({
	...(site ? { site } : {}),
	integrations: [
		...(sitemapDisabled ? [sitemapPlaceholder] : []),
		mermaid({ theme: 'default', autoTheme: true }),
		starlight({
			title: 'Chaosplus Docs',
			pagefind: protectedBuild ? false : true,
			defaultLocale: 'root',
			locales: {
				root: { label: '简体中文', lang: 'zh-CN' },
			},
			components: {
				Head: './src/components/Head.astro',
				Header: './src/components/Header.astro',
				MobileTableOfContents: './src/components/MobileTableOfContents.astro',
				PageTitle: './src/components/PageTitle.astro',
				Sidebar: './src/components/Sidebar.astro',
				TableOfContents: './src/components/TableOfContents.astro',
			},
			routeMiddleware: './src/sidebar-filter.ts',
			customCss: ['./src/styles/custom.css'],
			sidebar,
		}),
	],
});
