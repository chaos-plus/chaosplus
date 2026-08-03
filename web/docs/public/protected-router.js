(() => {
	'use strict';

	if (globalThis.__docsProtectedRuntime) return;
	globalThis.__docsProtectedRuntime = true;

	const encoder = new TextEncoder();
	const persistentKey = 'docs-access-v2';
	const sessionKey = 'docs-access-session-v2';
	const activeBuildKey = 'docs-active-build-v1';
	const originalFetch = globalThis.fetch.bind(globalThis);
	const contentKeys = new Map();
	let updateAvailable = false;
	let registration;
	let reloading = false;

	function decodeBase64(value) {
		const binary = atob(value);
		const bytes = new Uint8Array(binary.length);
		for (let index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index);
		return bytes;
	}

	function readStoredCredential(storage, key) {
		try {
			return JSON.parse(storage.getItem(key));
		} catch {
			return null;
		}
	}

	function getCredential(payload) {
		const persistent = readStoredCredential(localStorage, persistentKey);
		const session = readStoredCredential(sessionStorage, sessionKey);
		const credential = [persistent, session].find((candidate) =>
			candidate
			&& candidate.version === payload.version
			&& candidate.credentialSalt === payload.credentialSalt
			&& typeof candidate.masterKey === 'string');
		if (!credential) throw new Error('Missing protected-doc credential.');
		return credential;
	}

	async function getContentKey(payload) {
		if (contentKeys.has(payload.buildSalt)) return contentKeys.get(payload.buildSalt);
		const credential = getCredential(payload);
		const material = await crypto.subtle.importKey(
			'raw',
			decodeBase64(credential.masterKey),
			'HKDF',
			false,
			['deriveKey'],
		);
		const key = await crypto.subtle.deriveKey(
			{
				name: 'HKDF',
				hash: 'SHA-256',
				salt: decodeBase64(payload.buildSalt),
				info: encoder.encode(payload.keyInfo),
			},
			material,
			{ name: 'AES-GCM', length: 256 },
			false,
			['decrypt'],
		);
		contentKeys.set(payload.buildSalt, key);
		return key;
	}

	function outputPath(url) {
		let pathname;
		try {
			pathname = decodeURI(url.pathname);
		} catch {
			pathname = url.pathname;
		}
		const relative = pathname.replace(/^\/+/, '');
		if (!relative) return 'index.html';
		if (relative.endsWith('/')) return `${relative}index.html`;
		if (relative.endsWith('.html')) return relative;
		return `${relative}/index.html`;
	}

	function showUpdateNotice() {
		updateAvailable = true;
		const notice = document.getElementById('docs-update-notice');
		if (notice) notice.hidden = false;
	}

	async function decryptResponse(response, requestedUrl) {
		const contentType = response.headers.get('content-type') ?? '';
		if (!contentType.toLowerCase().includes('text/html')) return response;

		const shell = await response.clone().text();
		if (!shell.includes('data-docs-protected="v2"')) return response;
		const page = new DOMParser().parseFromString(shell, 'text/html');
		const payloadNode = page.getElementById('docs-encrypted-payload');
		if (!payloadNode) return response;
		const payload = JSON.parse(payloadNode.textContent);
		const finalUrl = response.url ? new URL(response.url) : requestedUrl;
		if (payload.path !== outputPath(finalUrl)) throw new Error('Protected page path mismatch.');

		const activeBuild = sessionStorage.getItem(activeBuildKey);
		if (activeBuild && payload.buildId !== activeBuild) showUpdateNotice();
		const combined = decodeBase64(payload.ciphertext);
		const decrypted = await crypto.subtle.decrypt(
			{
				name: 'AES-GCM',
				iv: decodeBase64(payload.iv),
				additionalData: encoder.encode(payload.path),
				tagLength: 128,
			},
			await getContentKey(payload),
			combined,
		);
		const html = new TextDecoder().decode(decrypted);
		if (!/^<!doctype html>/i.test(html)) throw new Error('Invalid protected document.');

		const headers = new Headers(response.headers);
		headers.delete('content-encoding');
		headers.delete('content-length');
		headers.delete('etag');
		const result = new Response(html, {
			status: response.status,
			statusText: response.statusText,
			headers,
		});
		try {
			Object.defineProperties(result, {
				redirected: { value: response.redirected },
				url: { value: response.url },
			});
		} catch {
			// Redirect metadata is only needed for non-canonical URLs.
		}
		return result;
	}

	globalThis.fetch = async (input, init) => {
		const requestedUrl = new URL(input instanceof Request ? input.url : input, location.href);
		const response = await originalFetch(input, init);
		const method = input instanceof Request ? input.method : (init?.method ?? 'GET');
		if (requestedUrl.origin !== location.origin || method.toUpperCase() !== 'GET') {
			return response;
		}
		try {
			return await decryptResponse(response, requestedUrl);
		} catch {
			return response;
		}
	};

	async function checkBuildVersion() {
		try {
			const response = await originalFetch('/docs-build.json', { cache: 'no-store' });
			if (!response.ok) return;
			const current = sessionStorage.getItem(activeBuildKey);
			const latest = await response.json();
			if (current && latest.buildId !== current) {
				showUpdateNotice();
				void registration?.update();
			}
		} catch {
			// Update checks are best-effort and must not interrupt document reading.
		}
	}

	function watchWorker(worker) {
		if (!worker) return;
		worker.addEventListener('statechange', () => {
			if (worker.state === 'installed' && navigator.serviceWorker.controller) showUpdateNotice();
		});
	}

	async function registerWorker() {
		if (!('serviceWorker' in navigator)) return;
		try {
			registration = await navigator.serviceWorker.register('/service-worker.js');
			if (registration.waiting && navigator.serviceWorker.controller) showUpdateNotice();
			watchWorker(registration.installing);
			registration.addEventListener('updatefound', () => watchWorker(registration.installing));
			await checkBuildVersion();
		} catch {
			// The encrypted router remains fully functional without offline caching.
		}
	}

	document.addEventListener('astro:page-load', () => {
		if (updateAvailable) showUpdateNotice();
	});
	document.addEventListener('click', async (event) => {
		if (!event.target?.closest?.('#docs-update-refresh')) return;
		registration ??= await navigator.serviceWorker?.getRegistration();
		if (registration?.waiting) registration.waiting.postMessage({ type: 'SKIP_WAITING' });
		else location.reload();
	});
	navigator.serviceWorker?.addEventListener('controllerchange', () => {
		if (reloading) return;
		reloading = true;
		location.reload();
	});
	window.addEventListener('focus', checkBuildVersion);
	setInterval(checkBuildVersion, 5 * 60 * 1000);
	void registerWorker();
})();
