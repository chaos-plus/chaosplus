import { defineRouteMiddleware } from '@astrojs/starlight/route-data';

// First path segment, e.g. "/mch/login/" -> "mch". "" for the homepage.
const section = (p: string) => p.replace(/^\/+/, '').split('/')[0] ?? '';

// Does this sidebar entry (link or group) belong to the given section?
function inSection(entry: any, sec: string): boolean {
	if (entry.type === 'link') return section(new URL(entry.href, 'https://x').pathname) === sec;
	return entry.entries?.some((e: any) => inSection(e, sec)) ?? false;
}

// Show only the sidebar group for the section the current page lives in, so
// each top-nav 端 gets its own contextual left menu instead of all groups.
export const onRequest = defineRouteMiddleware((context) => {
	const route = context.locals.starlightRoute;
	const sec = section(new URL(context.request.url).pathname);
	route.sidebar = route.sidebar.filter((entry) => inSection(entry, sec));

	// Recompute prev/next within the current section only. Starlight's default
	// Starlight paginates across the whole sidebar by default. Keep navigation
	// inside the current top-level Chaosplus documentation section.
	const flat: any[] = [];
	const collect = (entries: any[]) => {
		for (const e of entries) {
			if (e.type === 'link') flat.push(e);
			else if (e.entries) collect(e.entries);
		}
	};
	collect(route.sidebar);
	const idx = flat.findIndex((l) => l.isCurrent);
	if (idx !== -1) {
		route.pagination = {
			prev: idx > 0 ? flat[idx - 1] : undefined,
			next: idx < flat.length - 1 ? flat[idx + 1] : undefined,
		};
	}
});
