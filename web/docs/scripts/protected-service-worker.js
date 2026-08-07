'use strict';

const buildId = '__DOCS_BUILD_ID__';
const pageCacheName = `docs-pages-${buildId}`;
const assetCacheName = `docs-assets-${buildId}`;

self.addEventListener('message', (event) => {
	if (event.data?.type === 'SKIP_WAITING') void self.skipWaiting();
});

self.addEventListener('activate', (event) => {
	event.waitUntil((async () => {
		const keep = new Set([pageCacheName, assetCacheName]);
		const names = await caches.keys();
		await Promise.all(names.filter((name) => name.startsWith('docs-') && !keep.has(name)).map((name) => caches.delete(name)));
		await self.clients.claim();
	})());
});

async function cacheFirst(request) {
	const cache = await caches.open(assetCacheName);
	const cached = await cache.match(request);
	if (cached) return cached;
	const response = await fetch(request);
	if (response.ok) await cache.put(request, response.clone());
	return response;
}

async function staleWhileRevalidate(request) {
	const cache = await caches.open(pageCacheName);
	const cached = await cache.match(request);
	const network = fetch(request).then(async (response) => {
		if (response.ok) await cache.put(request, response.clone());
		return response;
	});
	if (!cached) return network;
	void network.catch(() => undefined);
	return cached;
}

self.addEventListener('fetch', (event) => {
	const { request } = event;
	if (request.method !== 'GET') return;
	const url = new URL(request.url);
	if (url.origin !== self.location.origin) return;
	if (url.pathname === '/service-worker.js' || url.pathname === '/docs-build.json') return;
	if (url.pathname.startsWith('/_astro/')) {
		event.respondWith(cacheFirst(request));
		return;
	}
	if (request.mode === 'navigate' || url.pathname === '/' || url.pathname.endsWith('/') || url.pathname.endsWith('.html')) {
		event.respondWith(staleWhileRevalidate(request));
	}
});
