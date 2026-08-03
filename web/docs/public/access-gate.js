(() => {
	'use strict';

	const encoder = new TextEncoder();
	const payload = JSON.parse(document.getElementById('docs-encrypted-payload').textContent);
	const persistentKey = 'docs-access-v2';
	const sessionKey = 'docs-access-session-v2';
	const activeBuildKey = 'docs-active-build-v1';
	const keyInfo = encoder.encode(payload.keyInfo);
	const form = document.getElementById('access-form');
	const passwordInput = document.getElementById('access-password');
	const showPassword = document.getElementById('access-show-password');
	const rememberAccess = document.getElementById('access-remember');
	const submitButton = document.getElementById('access-submit');
	const status = document.getElementById('access-status');
	const loading = document.getElementById('access-loading');
	const shell = document.querySelector('.access-shell');
	let busy = false;

	function decodeBase64(value) {
		const binary = atob(value);
		const bytes = new Uint8Array(binary.length);
		for (let index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index);
		return bytes;
	}

	function encodeBase64(bytes) {
		let binary = '';
		for (const byte of bytes) binary += String.fromCharCode(byte);
		return btoa(binary);
	}

	function setStatus(message, state = 'idle') {
		status.textContent = message;
		status.dataset.state = state;
		passwordInput.setAttribute('aria-invalid', state === 'error' ? 'true' : 'false');
	}

	function setBusy(value) {
		busy = value;
		loading.hidden = !value;
		shell.hidden = value;
		form.setAttribute('aria-busy', String(value));
		passwordInput.disabled = value;
		showPassword.disabled = value;
		rememberAccess.disabled = value;
		submitButton.disabled = value;
		submitButton.textContent = value ? '正在解锁...' : '解锁文档';
	}

	function readStoredCredential(storage, key) {
		try {
			return JSON.parse(storage.getItem(key));
		} catch {
			try {
				storage.removeItem(key);
			} catch {
				// Storage can be unavailable for restricted local-file profiles.
			}
			return null;
		}
	}

	function writeStoredCredential(storage, key, credential) {
		try {
			storage.setItem(key, JSON.stringify(credential));
			return true;
		} catch {
			return false;
		}
	}

	function removeStoredCredential(storage, key) {
		try {
			storage.removeItem(key);
		} catch {
			// Storage can be unavailable for restricted local-file profiles.
		}
	}

	function clearCachedCredential() {
		removeStoredCredential(localStorage, persistentKey);
		removeStoredCredential(sessionStorage, sessionKey);
	}

	function validCredential(credential) {
		return credential
			&& credential.version === payload.version
			&& credential.credentialSalt === payload.credentialSalt
			&& typeof credential.masterKey === 'string';
	}

	function cacheCredential(masterKey) {
		const credential = {
			version: payload.version,
			credentialSalt: payload.credentialSalt,
			masterKey: encodeBase64(masterKey),
		};
		writeStoredCredential(sessionStorage, sessionKey, credential);
		if (rememberAccess.checked) {
			writeStoredCredential(localStorage, persistentKey, credential);
		} else {
			removeStoredCredential(localStorage, persistentKey);
		}
	}

	function getCachedCredential() {
		const persistent = readStoredCredential(localStorage, persistentKey);
		if (validCredential(persistent)) return persistent;
		const session = readStoredCredential(sessionStorage, sessionKey);
		return validCredential(session) ? session : null;
	}

	async function deriveMasterKey(password) {
		const material = await crypto.subtle.importKey(
			'raw',
			encoder.encode(password),
			'PBKDF2',
			false,
			['deriveBits'],
		);
		return new Uint8Array(await crypto.subtle.deriveBits(
			{
				name: 'PBKDF2',
				salt: decodeBase64(payload.credentialSalt),
				iterations: payload.iterations,
				hash: 'SHA-256',
			},
			material,
			256,
		));
	}

	async function deriveContentKey(masterKey) {
		const material = await crypto.subtle.importKey('raw', masterKey, 'HKDF', false, ['deriveKey']);
		return crypto.subtle.deriveKey(
			{
				name: 'HKDF',
				hash: 'SHA-256',
				salt: decodeBase64(payload.buildSalt),
				info: keyInfo,
			},
			material,
			{ name: 'AES-GCM', length: 256 },
			false,
			['decrypt'],
		);
	}

	async function decryptPage(key) {
		const decrypted = await crypto.subtle.decrypt(
			{
				name: 'AES-GCM',
				iv: decodeBase64(payload.iv),
				additionalData: encoder.encode(payload.path),
				tagLength: 128,
			},
			key,
			decodeBase64(payload.ciphertext),
		);
		const html = new TextDecoder().decode(decrypted);
		if (!/^<!doctype html>/i.test(html)) throw new Error('Invalid document');
		return html;
	}

	async function preloadStyles(html) {
		const page = new DOMParser().parseFromString(html, 'text/html');
		const stylesheets = [...new Set(
			[...page.querySelectorAll('link[rel~="stylesheet"][href]')]
				.map((link) => new URL(link.getAttribute('href'), location.href).href),
		)];
		await Promise.all(stylesheets.map((href) => new Promise((resolve) => {
			const link = document.createElement('link');
			const timeout = setTimeout(resolve, 3000);
			const finish = () => {
				clearTimeout(timeout);
				resolve();
			};
			link.rel = 'stylesheet';
			link.media = 'print';
			link.href = href;
			link.addEventListener('load', finish, { once: true });
			link.addEventListener('error', finish, { once: true });
			document.head.append(link);
		})));
	}

	async function openPage(html) {
		await preloadStyles(html);
		try {
			sessionStorage.setItem(activeBuildKey, payload.buildId);
		} catch {
			// Session storage can be unavailable for restricted local-file profiles.
		}
		document.open();
		document.write(html);
		document.close();
	}

	async function unlockWithPassword(password) {
		setBusy(true);
		setStatus('正在验证访问权限...');
		try {
			const masterKey = await deriveMasterKey(password);
			const html = await decryptPage(await deriveContentKey(masterKey));
			cacheCredential(masterKey);
			await openPage(html);
		} catch {
			clearCachedCredential();
			passwordInput.value = '';
			setStatus('密码不正确，请重试。', 'error');
			setBusy(false);
			passwordInput.focus();
		}
	}

	async function unlockFromCache() {
		const cached = getCachedCredential();
		if (!cached) return false;

		setBusy(true);
		setStatus('正在恢复访问权限...');
		try {
			const masterKey = decodeBase64(cached.masterKey);
			const html = await decryptPage(await deriveContentKey(masterKey));
			writeStoredCredential(sessionStorage, sessionKey, cached);
			await openPage(html);
			return true;
		} catch {
			clearCachedCredential();
			setBusy(false);
			setStatus('访问密码已更新，请重新输入密码。', 'error');
			return false;
		}
	}

	showPassword.addEventListener('change', () => {
		passwordInput.type = showPassword.checked ? 'text' : 'password';
	});

	form.addEventListener('submit', (event) => {
		event.preventDefault();
		if (busy) return;
		if (!passwordInput.value) {
			setStatus('请输入访问密码。', 'error');
			passwordInput.focus();
			return;
		}
		void unlockWithPassword(passwordInput.value);
	});

	if (!globalThis.crypto?.subtle) {
		setBusy(false);
		setStatus('当前浏览器不支持安全解密，请升级后重试。', 'error');
		passwordInput.disabled = true;
		showPassword.disabled = true;
		rememberAccess.disabled = true;
		submitButton.disabled = true;
		return;
	}

	void unlockFromCache().then((unlocked) => {
		if (!unlocked) {
			setBusy(false);
			passwordInput.focus();
		}
	});
})();
