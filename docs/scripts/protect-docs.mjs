#!/usr/bin/env node

import 'dotenv/config';
import {
	createHash,
	createCipheriv,
	createDecipheriv,
	hkdfSync,
	pbkdf2Sync,
	randomBytes,
} from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { buildSync } from 'esbuild';
import { parse, serialize } from 'parse5';

const distRoot = path.resolve('dist');
const docsRoot = path.resolve('src/content/docs');
const password = process.env.DOC_PASSWORD ?? '';
const buildTarget = process.env.DOC_BUILD_TARGET || 'web';
if (!['web', 'portable'].includes(buildTarget)) {
	throw new Error('DOC_BUILD_TARGET must be either "web" or "portable".');
}
const iterations = 600_000;
const credentialSalt = createHash('sha256').update('docs-access-master-key-v2').digest().subarray(0, 16);
const contentKeyInfo = Buffer.from('docs-content-v2');
const unlockTitle = 'Docs';
const accessGateStyles = fs.readFileSync(path.resolve('public/access-gate.css'), 'utf8');
const accessGateScript = fs.readFileSync(path.resolve('public/access-gate.js'), 'utf8');
const serviceWorkerTemplate = fs.readFileSync(path.resolve('scripts/protected-service-worker.js'), 'utf8');
const marker = 'data-docs-protected="v2"';
const portableUrlAttributes = new Set(['action', 'href', 'poster', 'src', 'xlink:href']);
const bundledModuleEntries = new Map();
const bundledInlineModules = new Map();

function listFiles(directory, predicate = () => true) {
	return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
		const target = path.join(directory, entry.name);
		return entry.isDirectory() ? listFiles(target, predicate) : predicate(target) ? [target] : [];
	});
}

function splitUrl(value) {
	const match = value.match(/^([^?#]*)(\?[^#]*)?(#.*)?$/u);
	return match ? { pathname: match[1], search: match[2] ?? '', hash: match[3] ?? '' } : null;
}

function resolveOutputTarget(urlPathname) {
	let decodedPathname;
	try {
		decodedPathname = decodeURI(urlPathname);
	} catch {
		decodedPathname = urlPathname;
	}

	const requested = decodedPathname.replace(/^\/+/, '');
	const normalized = path.posix.normalize(requested || 'index.html');
	if (normalized === '..' || normalized.startsWith('../')) return null;

	const candidates = requested === ''
		? ['index.html']
		: requested.endsWith('/')
			? [path.posix.join(normalized, 'index.html')]
			: [normalized, `${normalized}.html`, path.posix.join(normalized, 'index.html')];
	return candidates.find((candidate) => {
		const target = path.join(distRoot, ...candidate.split('/'));
		return fs.existsSync(target) && fs.statSync(target).isFile();
	}) ?? candidates[0];
}

function rewriteRootUrl(value, relativePath) {
	if (!value.startsWith('/') || value.startsWith('//')) return value;
	const parts = splitUrl(value);
	if (!parts) return value;
	const target = resolveOutputTarget(parts.pathname);
	if (!target) return value;

	const currentDirectory = path.posix.dirname(relativePath);
	let rewritten = path.posix.relative(currentDirectory, target);
	if (!rewritten.startsWith('.')) rewritten = `./${rewritten}`;
	return `${rewritten}${parts.search}${parts.hash}`;
}

function rewriteSrcset(value, relativePath) {
	return value
		.split(',')
		.map((candidate) => {
			const match = candidate.trim().match(/^(\S+)(\s+.*)?$/u);
			if (!match) return candidate.trim();
			return `${rewriteRootUrl(match[1], relativePath)}${match[2] ?? ''}`;
		})
		.join(', ');
}

function rewriteCssUrls(value, relativePath) {
	return value.replace(/url\((['"]?)(\/(?!\/)[^)'"\s]+)\1\)/gu, (_match, quote, url) =>
		`url(${quote}${rewriteRootUrl(url, relativePath)}${quote})`);
}

function rewriteHtmlForOffline(html, relativePath) {
	const document = parse(html);
	const visit = (node) => {
		for (const attribute of node.attrs ?? []) {
			if (portableUrlAttributes.has(attribute.name)) {
				attribute.value = rewriteRootUrl(attribute.value, relativePath);
			} else if (attribute.name === 'srcset') {
				attribute.value = rewriteSrcset(attribute.value, relativePath);
			} else if (attribute.name === 'style') {
				attribute.value = rewriteCssUrls(attribute.value, relativePath);
			}
		}
		for (const child of node.childNodes ?? []) visit(child);
	};
	visit(document);
	return serialize(document);
}

function getAttribute(node, name) {
	return node.attrs?.find((attribute) => attribute.name === name);
}

function removeAttribute(node, name) {
	if (node.attrs) node.attrs = node.attrs.filter((attribute) => attribute.name !== name);
}

function setAttribute(node, name, value) {
	const attribute = getAttribute(node, name);
	if (attribute) attribute.value = value;
	else (node.attrs ??= []).push({ name, value });
}

function relativeOutputUrl(relativePath, outputPath) {
	let value = path.posix.relative(path.posix.dirname(relativePath), outputPath);
	if (!value.startsWith('.')) value = `./${value}`;
	return value;
}

function resolveLocalScript(source, relativePath) {
	const parts = splitUrl(source);
	if (!parts || /^[a-z][a-z\d+.-]*:/iu.test(parts.pathname) || parts.pathname.startsWith('//')) return null;
	let decodedPathname;
	try {
		decodedPathname = decodeURI(parts.pathname);
	} catch {
		decodedPathname = parts.pathname;
	}
	const outputPath = path.posix.normalize(path.posix.join(path.posix.dirname(relativePath), decodedPathname));
	if (outputPath === '..' || outputPath.startsWith('../')) return null;
	const absolutePath = path.join(distRoot, ...outputPath.split('/'));
	return fs.existsSync(absolutePath) && fs.statSync(absolutePath).isFile()
		? { absolutePath, outputPath }
		: null;
}

function buildClassicBundle(options) {
	const result = buildSync({
		...options,
		bundle: true,
		format: 'iife',
		platform: 'browser',
		target: 'esnext',
		minify: true,
		sourcemap: false,
		write: false,
	});
	if (result.outputFiles.length !== 1) throw new Error('Expected one bundled JavaScript output file.');
	return result.outputFiles[0].text;
}

function bundleModuleEntries(sources, relativePath) {
	const localEntries = sources.map((source) => {
		const local = resolveLocalScript(source, relativePath);
		if (!local) throw new Error(`${relativePath} contains a module script that cannot be bundled for offline use: ${source}`);
		return local;
	});
	const cacheKey = localEntries.map(({ outputPath }) => outputPath).join('\0');
	if (bundledModuleEntries.has(cacheKey)) return bundledModuleEntries.get(cacheKey);

	const hash = createHash('sha256').update(cacheKey).digest('hex').slice(0, 16);
	const entryPath = path.join(distRoot, '_astro', `offline-entry-${hash}.mjs`);
	const outputPath = `_astro/offline-${hash}.js`;
	const absoluteOutputPath = path.join(distRoot, ...outputPath.split('/'));
	const imports = localEntries
		.map(({ absolutePath }) => `import ${JSON.stringify(path.relative(path.dirname(entryPath), absolutePath).split(path.sep).join('/').replace(/^(?!\.)/u, './'))};`)
		.join('\n');
	fs.writeFileSync(entryPath, `${imports}\n`);
	try {
		const code = buildClassicBundle({ entryPoints: [entryPath] });
		fs.writeFileSync(absoluteOutputPath, code);
	} finally {
		fs.rmSync(entryPath, { force: true });
	}
	bundledModuleEntries.set(cacheKey, outputPath);
	return outputPath;
}

function bundleInlineModule(source, relativePath) {
	const cacheKey = `${path.posix.dirname(relativePath)}\0${source}`;
	if (bundledInlineModules.has(cacheKey)) return bundledInlineModules.get(cacheKey);
	const code = buildClassicBundle({
		stdin: {
			contents: source,
			loader: 'js',
			resolveDir: path.dirname(path.join(distRoot, ...relativePath.split('/'))),
			sourcefile: `${path.basename(relativePath)}-inline-module.js`,
		},
	});
	bundledInlineModules.set(cacheKey, code);
	return code;
}

function bundleOfflineModules(html, relativePath) {
	const document = parse(html);
	const externalModules = [];
	const visit = (node) => {
		if (node.tagName === 'script' && getAttribute(node, 'type')?.value.toLowerCase() === 'module') {
			const sourceAttribute = getAttribute(node, 'src');
			if (sourceAttribute) {
				externalModules.push({ node, sourceAttribute });
			} else {
				removeAttribute(node, 'type');
				const sourceNode = node.childNodes?.find((child) => child.nodeName === '#text');
				if (sourceNode) sourceNode.value = bundleInlineModule(sourceNode.value, relativePath);
			}
		}
		for (const child of node.childNodes ?? []) visit(child);
	};
	visit(document);
	if (externalModules.length > 0) {
		const outputPath = bundleModuleEntries(
			externalModules.map(({ sourceAttribute }) => sourceAttribute.value),
			relativePath,
		);
		const [first, ...duplicates] = externalModules;
		removeAttribute(first.node, 'type');
		first.sourceAttribute.value = relativeOutputUrl(relativePath, outputPath);
		setAttribute(first.node, 'defer', '');
		for (const { node } of duplicates) {
			node.parentNode.childNodes = node.parentNode.childNodes.filter((child) => child !== node);
		}
	}
	return serialize(document);
}

function disableModulePreloadHelpers() {
	const assetRoot = path.join(distRoot, '_astro');
	if (!fs.existsSync(assetRoot)) return;
	for (const file of fs.readdirSync(assetRoot)) {
		if (/^preload-helper\..*\.js$/u.test(file)) {
			fs.writeFileSync(
				path.join(assetRoot, file),
				'export const t = (loader) => Promise.resolve().then(loader);\n',
			);
		}
	}
}

function makeHtmlPortable() {
	disableModulePreloadHelpers();
	const htmlFiles = listFiles(distRoot, (file) => file.endsWith('.html'));
	for (const file of htmlFiles) {
		const relativePath = path.relative(distRoot, file).split(path.sep).join('/');
		const html = fs.readFileSync(file, 'utf8');
		const portableHtml = rewriteHtmlForOffline(html, relativePath);
		fs.writeFileSync(file, bundleOfflineModules(portableHtml, relativePath));
	}
	return htmlFiles;
}

function assertPortableHtml(html, file) {
	const document = parse(html);
	const failures = [];
	const visit = (node) => {
		if (node.tagName === 'script' && getAttribute(node, 'type')?.value.toLowerCase() === 'module') {
			failures.push('script[type="module"]');
		}
		for (const attribute of node.attrs ?? []) {
			if (portableUrlAttributes.has(attribute.name) && /^\/(?!\/)/u.test(attribute.value)) {
				failures.push(`${attribute.name}=${JSON.stringify(attribute.value)}`);
			}
			if (attribute.name === 'srcset' && /(?:^|,\s*)\/(?!\/)/u.test(attribute.value)) {
				failures.push(`srcset=${JSON.stringify(attribute.value)}`);
			}
			if (attribute.name === 'style' && /url\((['"]?)\/(?!\/)/u.test(attribute.value)) {
				failures.push(`style=${JSON.stringify(attribute.value)}`);
			}
		}
		for (const child of node.childNodes ?? []) visit(child);
	};
	visit(document);
	if (failures.length > 0) {
		throw new Error(`${file} contains root-relative URLs: ${failures.slice(0, 5).join(', ')}`);
	}
}

function deriveContentKey(masterKey, buildSalt) {
	return Buffer.from(hkdfSync('sha256', masterKey, buildSalt, contentKeyInfo, 32));
}

function encryptHtml(html, key, buildSalt, relativePath, buildId) {
	const iv = randomBytes(12);
	const cipher = createCipheriv('aes-256-gcm', key, iv);
	cipher.setAAD(Buffer.from(relativePath));
	const encrypted = Buffer.concat([cipher.update(html, 'utf8'), cipher.final()]);
	return {
		version: 2,
		buildId,
		target: buildTarget,
		keyInfo: contentKeyInfo.toString('utf8'),
		iterations,
		path: relativePath,
		credentialSalt: credentialSalt.toString('base64'),
		buildSalt: buildSalt.toString('base64'),
		iv: iv.toString('base64'),
		ciphertext: Buffer.concat([encrypted, cipher.getAuthTag()]).toString('base64'),
	};
}

function escapeHtml(value) {
	return value.replace(/[&<>"']/gu, (character) => ({
		'&': '&amp;',
		'<': '&lt;',
		'>': '&gt;',
		'"': '&quot;',
		"'": '&#39;',
	})[character]);
}

function renderShell(payload) {
	const escapedTitle = escapeHtml(unlockTitle);
	const serializedPayload = JSON.stringify(payload).replace(/</gu, '\\u003c');
	const inlineScript = accessGateScript.replace(/<\/script/giu, '<\\/script');
	return `<!doctype html>
<html lang="zh-CN" ${marker}>
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<meta name="robots" content="noindex,nofollow,noarchive">
	<meta name="referrer" content="no-referrer">
	<title>受保护文档 | ${escapedTitle}</title>
	<style>${accessGateStyles}</style>
</head>
<body>
	<div class="access-loading" id="access-loading" role="status" aria-live="polite">
		<span class="access-spinner" aria-hidden="true"></span>
		<span>正在检查访问状态...</span>
	</div>
	<main class="access-shell" hidden>
		<section class="access-panel" aria-labelledby="access-title">
			<div class="access-brand"><span class="access-brand-mark" aria-hidden="true">DOC</span><span>${escapedTitle}</span></div>
			<h1 id="access-title">受保护文档</h1>
			<p class="access-intro">此站点内容已加密保护，请验证访问密码。</p>
			<form id="access-form" novalidate>
				<label class="access-label" for="access-password">访问密码</label>
				<input class="access-input" id="access-password" name="password" type="password" autocomplete="current-password" spellcheck="false" aria-describedby="access-status">
				<label class="access-show-password"><input id="access-show-password" type="checkbox">显示密码</label>
				<label class="access-remember"><input id="access-remember" type="checkbox" checked>在此设备保持解锁</label>
				<p class="access-status" id="access-status" role="status" aria-live="polite"></p>
				<button class="access-submit" id="access-submit" type="submit">解锁文档</button>
			</form>
		</section>
	</main>
	<script id="docs-encrypted-payload" type="application/json">${serializedPayload}</script>
	<script>${inlineScript}</script>
</body>
</html>
`;
}

function readPayload(html, file) {
	const match = html.match(/<script id="docs-encrypted-payload" type="application\/json">([^<]+)<\/script>/);
	if (!html.includes(marker) || !match) throw new Error(`${file} is not a protected document.`);
	return JSON.parse(match[1]);
}

function decryptPayload(payload, key) {
	const combined = Buffer.from(payload.ciphertext, 'base64');
	const tag = combined.subarray(combined.length - 16);
	const ciphertext = combined.subarray(0, combined.length - 16);
	const decipher = createDecipheriv('aes-256-gcm', key, Buffer.from(payload.iv, 'base64'));
	decipher.setAAD(Buffer.from(payload.path));
	decipher.setAuthTag(tag);
	return Buffer.concat([decipher.update(ciphertext), decipher.final()]).toString('utf8');
}

function sourceProbes() {
	return listFiles(docsRoot, (file) => file.endsWith('.md')).flatMap((file) => {
		const source = fs.readFileSync(file, 'utf8');
		const frontmatter = source.match(/^---\r?\n([\s\S]*?)\r?\n---/u)?.[1] ?? '';
		return [...frontmatter.matchAll(/^(?:title|description):\s*["']?(.+?)["']?\s*$/gmu)]
			.map((match) => match[1].trim())
			.filter((value) => value.length >= 12);
	});
}

function auditNoPlaintext() {
	const outputFiles = listFiles(distRoot);
	const probes = [...new Set(sourceProbes())];
	for (const file of outputFiles) {
		const content = fs.readFileSync(file);
		if (content.includes(Buffer.from(password))) {
			throw new Error(`DOC_PASSWORD found in ${path.relative(process.cwd(), file)}.`);
		}
		for (const probe of probes) {
			if (content.includes(Buffer.from(probe))) {
				throw new Error(`Plaintext document metadata found in ${path.relative(process.cwd(), file)}: ${probe}`);
			}
		}
	}
	if (fs.existsSync(path.join(distRoot, 'pagefind'))) {
		throw new Error('Protected output must not contain a Pagefind index.');
	}
	const sourceMaps = outputFiles.filter((file) => file.endsWith('.map'));
	if (sourceMaps.length > 0) throw new Error('Protected output must not contain source maps.');
}

function checkPortableOutput() {
	if (!fs.existsSync(distRoot)) throw new Error('Missing dist; run the build first.');
	const htmlFiles = listFiles(distRoot, (file) => file.endsWith('.html'));
	if (htmlFiles.length === 0) throw new Error('No HTML files found in dist.');
	for (const file of htmlFiles) {
		assertPortableHtml(fs.readFileSync(file, 'utf8'), path.relative(process.cwd(), file));
	}
	console.log(`Offline path check passed: ${htmlFiles.length} HTML files contain no root-relative local URLs.`);
}

function checkProtectedOutput() {
	if (!fs.existsSync(distRoot)) throw new Error('Missing dist; run the protected build first.');
	const htmlFiles = listFiles(distRoot, (file) => file.endsWith('.html'));
	if (htmlFiles.length === 0) throw new Error('No HTML files found in dist.');
	let buildSalt;
	let checkedBuildId;
	let checkedTarget;
	let key;
	const masterKey = pbkdf2Sync(password, credentialSalt, iterations, 32, 'sha256');
	const ivs = new Set();
	for (const file of htmlFiles) {
		const shell = fs.readFileSync(file, 'utf8');
		assertPortableHtml(shell, path.relative(process.cwd(), file));
		const payload = readPayload(shell, path.relative(process.cwd(), file));
		const relativePath = path.relative(distRoot, file).split(path.sep).join('/');
		if (
			payload.version !== 2
			|| !/^[a-f\d]{32}$/u.test(payload.buildId)
			|| !['web', 'portable'].includes(payload.target)
			|| payload.keyInfo !== contentKeyInfo.toString('utf8')
			|| payload.iterations !== iterations
			|| payload.path !== relativePath
			|| payload.credentialSalt !== credentialSalt.toString('base64')
		) {
			throw new Error(`${path.relative(process.cwd(), file)} has invalid protection metadata.`);
		}
		checkedBuildId ??= payload.buildId;
		checkedTarget ??= payload.target;
		if (payload.buildId !== checkedBuildId || payload.target !== checkedTarget) {
			throw new Error(`${path.relative(process.cwd(), file)} does not match the protected build identity.`);
		}
		if (Buffer.from(payload.iv, 'base64').length !== 12 || ivs.has(payload.iv)) {
			throw new Error(`${path.relative(process.cwd(), file)} has an invalid or reused IV.`);
		}
		ivs.add(payload.iv);
		buildSalt ??= payload.buildSalt;
		if (payload.buildSalt !== buildSalt || Buffer.from(payload.buildSalt, 'base64').length !== 16) {
			throw new Error(`${path.relative(process.cwd(), file)} does not use the shared build salt.`);
		}
		key ??= deriveContentKey(masterKey, Buffer.from(buildSalt, 'base64'));
		const decrypted = decryptPayload(payload, key);
		if (!/^<!doctype html>/i.test(decrypted) || !decrypted.includes('</html>')) {
			throw new Error(`${path.relative(process.cwd(), file)} did not decrypt to a complete HTML document.`);
		}
		if (payload.target === 'portable') {
			assertPortableHtml(decrypted, path.relative(process.cwd(), file));
		} else if (
			!decrypted.includes('name="astro-view-transitions-enabled"')
			|| !decrypted.includes('src="/protected-router.js"')
		) {
			throw new Error(`${path.relative(process.cwd(), file)} does not include the protected web router.`);
		}
	}
	if (checkedTarget === 'web') {
		const manifest = JSON.parse(fs.readFileSync(path.join(distRoot, 'docs-build.json'), 'utf8'));
		const serviceWorker = fs.readFileSync(path.join(distRoot, 'service-worker.js'), 'utf8');
		if (manifest.version !== 1 || manifest.buildId !== checkedBuildId || !serviceWorker.includes(checkedBuildId)) {
			throw new Error('Protected web runtime does not match the encrypted build identity.');
		}
	}
	auditNoPlaintext();
	console.log(`Protected ${checkedTarget} output check passed: ${htmlFiles.length} HTML files decrypted; no Pagefind, source maps, or document metadata leaks found.`);
}

const protectionEnabled = Boolean(password);

if (process.argv.includes('--require')) {
	console.log(
		protectionEnabled
			? `DOC_PASSWORD is set; protected ${buildTarget} build enabled.`
			: `DOC_PASSWORD is not set; building an unprotected ${buildTarget} site.`,
	);
	process.exit(0);
}

if (!protectionEnabled) {
	if (!process.argv.includes('--check')) {
		if (!fs.existsSync(distRoot)) throw new Error('Missing dist; run astro build first.');
		if (buildTarget === 'portable') {
			makeHtmlPortable();
			checkPortableOutput();
		}
	}
	console.log(
		process.argv.includes('--check')
			? 'DOC_PASSWORD is not set; protected output check skipped.'
			: `DOC_PASSWORD is not set; leaving ${buildTarget} build output unencrypted.`,
	);
	process.exit(0);
}

if (process.argv.includes('--check')) {
	checkProtectedOutput();
	process.exit(0);
}

if (!fs.existsSync(distRoot)) throw new Error('Missing dist; run astro build first.');
const pagefindRoot = path.join(distRoot, 'pagefind');
if (fs.existsSync(pagefindRoot)) fs.rmSync(pagefindRoot, { recursive: true, force: true });

const htmlFiles = buildTarget === 'portable'
	? makeHtmlPortable()
	: listFiles(distRoot, (file) => file.endsWith('.html'));
const buildSalt = randomBytes(16);
const buildId = randomBytes(16).toString('hex');
const masterKey = pbkdf2Sync(password, credentialSalt, iterations, 32, 'sha256');
const key = deriveContentKey(masterKey, buildSalt);
if (buildTarget === 'web') {
	fs.writeFileSync(
		path.join(distRoot, 'docs-build.json'),
		`${JSON.stringify({ version: 1, buildId })}\n`,
	);
	fs.writeFileSync(
		path.join(distRoot, 'service-worker.js'),
		serviceWorkerTemplate.replace('__DOCS_BUILD_ID__', buildId),
	);
}
for (const file of htmlFiles) {
	const relativePath = path.relative(distRoot, file).split(path.sep).join('/');
	const payload = encryptHtml(fs.readFileSync(file, 'utf8'), key, buildSalt, relativePath, buildId);
	fs.writeFileSync(file, renderShell(payload));
}

checkProtectedOutput();
