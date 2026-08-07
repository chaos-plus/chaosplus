import fs from 'node:fs/promises';
import path from 'node:path';
import process from 'node:process';
import { topNavigation } from '../src/site-nav.mjs';

const port = Number(process.argv[2] ?? 9334);
const baseURL = process.argv[3] ?? 'http://127.0.0.1:4322';
const screenshotRoot = path.resolve('../../.local/screenshots');
const targets = await fetch(`http://127.0.0.1:${port}/json/list`).then((response) => response.json());
const target = targets.find((candidate) => candidate.type === 'page');
if (!target?.webSocketDebuggerUrl) throw new Error('Chromium page target was not found');

const socket = new WebSocket(target.webSocketDebuggerUrl);
await new Promise((resolve, reject) => {
	socket.addEventListener('open', resolve, { once: true });
	socket.addEventListener('error', reject, { once: true });
});

let sequence = 0;
const pending = new Map();
const consoleErrors = [];
const failedRequests = [];
socket.addEventListener('message', (event) => {
	const message = JSON.parse(event.data);
	if (message.id) {
		const request = pending.get(message.id);
		if (!request) return;
		pending.delete(message.id);
		if (message.error) request.reject(new Error(message.error.message));
		else request.resolve(message.result);
		return;
	}
	if (message.method === 'Runtime.exceptionThrown') {
		consoleErrors.push(message.params.exceptionDetails.text);
	}
	if (message.method === 'Runtime.consoleAPICalled' && message.params.type === 'error') {
		consoleErrors.push(message.params.args.map((argument) => argument.value ?? argument.description).join(' '));
	}
	if (message.method === 'Network.loadingFailed' && !message.params.canceled) {
		failedRequests.push(message.params.errorText);
	}
});

function command(method, params = {}) {
	const id = ++sequence;
	socket.send(JSON.stringify({ id, method, params }));
	return new Promise((resolve, reject) => pending.set(id, { resolve, reject }));
}

async function evaluate(source) {
	const result = await command('Runtime.evaluate', {
		expression: `(async () => { ${source} })()`,
		awaitPromise: true,
		returnByValue: true,
	});
	if (result.exceptionDetails) {
		throw new Error(result.exceptionDetails.exception?.description ?? result.exceptionDetails.text);
	}
	return result.result.value;
}

async function waitFor(source, timeout = 15000) {
	const deadline = Date.now() + timeout;
	while (Date.now() < deadline) {
		if (await evaluate(`return Boolean(${source})`)) return;
		await Bun.sleep(100);
	}
	throw new Error(`Timed out waiting for: ${source}`);
}

async function inspect(name, pathname, width, height) {
	await command('Emulation.setDeviceMetricsOverride', {
		width,
		height,
		deviceScaleFactor: 1,
		mobile: width < 600,
	});
	await command('Page.navigate', { url: new URL(pathname, baseURL).href });
	await waitFor(`document.readyState === 'complete'`);
	await waitFor(`document.fonts.status === 'loaded'`);
	await waitFor(`[...document.images].every((image) => image.complete)`);
	await Bun.sleep(200);

	const result = await evaluate(`
		const topNav = document.querySelector('.top-nav');
		const mobileNav = document.querySelector('.mobile-navigation');
		const navHeader = document.querySelector('.nav-header');
		const activeNavigation = topNav?.querySelector('[aria-current="page"]');
		const homeCard = document.querySelector('.home-card');
		const styleSheets = [...document.styleSheets].filter((sheet) => sheet.href);
		return {
			path: location.pathname,
			width: innerWidth,
			scrollWidth: document.documentElement.scrollWidth,
			horizontalOverflow: document.documentElement.scrollWidth > innerWidth,
			topNavigationCount: topNav?.querySelectorAll('a').length ?? 0,
			topNavigationLabels: [...(topNav?.querySelectorAll('a') ?? [])].map((link) => link.textContent.trim()),
			topNavigationDisplay: topNav ? getComputedStyle(topNav).display : null,
			mobileNavigationDisplay: mobileNav ? getComputedStyle(mobileNav).display : null,
			headerLayout: navHeader ? getComputedStyle(navHeader).display : null,
			activeNavigationRadius: activeNavigation ? getComputedStyle(activeNavigation).borderRadius : null,
			activeNavigationBackground: activeNavigation ? getComputedStyle(activeNavigation).backgroundColor : null,
			customRadius: getComputedStyle(document.documentElement).getPropertyValue('--radius').trim(),
			homeCardRadius: homeCard ? getComputedStyle(homeCard).borderRadius : null,
			headerVisible: Boolean(document.querySelector('header.header')),
			sidebarVisible: Boolean(document.querySelector('.sidebar-pane')) && getComputedStyle(document.querySelector('.sidebar-pane')).display !== 'none',
			tableOfContentsVisible: Boolean(document.querySelector('.right-sidebar-container')) && getComputedStyle(document.querySelector('.right-sidebar-container')).display !== 'none',
			styleSheetCount: styleSheets.length,
		};
	`);

	if (result.horizontalOverflow) {
		throw new Error(`${name}: horizontal overflow ${result.scrollWidth}px at ${result.width}px`);
	}
	if (!result.headerVisible || result.styleSheetCount === 0) {
		throw new Error(`${name}: documentation shell or stylesheet is missing`);
	}
	if (result.topNavigationCount !== topNavigation.length) {
		throw new Error(`${name}: expected ${topNavigation.length} top navigation links, got ${result.topNavigationCount}`);
	}
	const expectedLabels = topNavigation.map((item) => item.label);
	if (JSON.stringify(result.topNavigationLabels) !== JSON.stringify(expectedLabels)) {
		throw new Error(`${name}: top navigation labels do not match the configured shell`);
	}
	if (
		result.headerLayout !== 'flex'
		|| Number.parseFloat(result.customRadius) !== 0.625
		|| Number.parseFloat(result.activeNavigationRadius ?? '0') < 100
		|| result.activeNavigationBackground === 'rgba(0, 0, 0, 0)'
	) {
		throw new Error(`${name}: copied navigation theme styles are not active: ${JSON.stringify({
			headerLayout: result.headerLayout,
			customRadius: result.customRadius,
			activeNavigationRadius: result.activeNavigationRadius,
			activeNavigationBackground: result.activeNavigationBackground,
		})}`);
	}
	if (pathname === '/' && Number.parseFloat(result.homeCardRadius ?? '0') === 0) {
		throw new Error(`${name}: copied home-page card styles are not active`);
	}
	if (width >= 800 && result.topNavigationDisplay === 'none') {
		throw new Error(`${name}: desktop top navigation is hidden`);
	}
	if (width < 800 && (result.topNavigationDisplay !== 'none' || result.mobileNavigationDisplay === 'none')) {
		throw new Error(`${name}: mobile navigation did not replace desktop navigation`);
	}

	const capture = await command('Page.captureScreenshot', {
		format: 'png',
		fromSurface: true,
		captureBeyondViewport: false,
	});
	await fs.mkdir(screenshotRoot, { recursive: true });
	await Bun.write(path.join(screenshotRoot, `${name}.png`), Buffer.from(capture.data, 'base64'));
	return result;
}

await Promise.all([
	command('Network.enable'),
	command('Page.enable'),
	command('Runtime.enable'),
]);

const results = [];
for (const viewport of [
	['docs-home-desktop', '/', 1440, 1000],
	['docs-home-mobile', '/', 390, 844],
	['docs-architecture-desktop', '/architecture/iam-platform/', 1440, 1000],
	['docs-architecture-mobile', '/architecture/iam-platform/', 390, 844],
]) {
	results.push(await inspect(...viewport));
}

socket.close();
if (consoleErrors.length > 0) throw new Error(`Console errors: ${consoleErrors.join('; ')}`);
if (failedRequests.length > 0) throw new Error(`Failed requests: ${failedRequests.join('; ')}`);
console.log(JSON.stringify({ results, consoleErrors, failedRequests }, null, 2));
