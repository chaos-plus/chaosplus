#!/usr/bin/env node

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { documents, routeFor } from './docs-map.mjs';

const scriptRoot = path.dirname(fileURLToPath(import.meta.url));
const siteRoot = path.resolve(scriptRoot, '..');
const docsRoot = path.resolve(scriptRoot, '../src/content/docs');
const publicationFiles = [
	path.resolve(scriptRoot, '../astro.config.mjs'),
	path.resolve(scriptRoot, '../Dockerfile'),
	path.resolve(scriptRoot, '../nginx.conf'),
	path.resolve(scriptRoot, '../package.json'),
	path.resolve(scriptRoot, '../README.md'),
	...listFiles(path.resolve(scriptRoot, '../public'), new Set(['.css', '.js'])),
	...listFiles(path.resolve(scriptRoot, '../scripts'), new Set(['.js', '.mjs'])),
	...listFiles(path.resolve(scriptRoot, '../src'), new Set(['.astro', '.css'])),
	...listMarkdown(docsRoot),
].filter((file) => file !== fileURLToPath(import.meta.url));
const failures = [];

function listFiles(directory, extensions) {
	return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
		const target = path.join(directory, entry.name);
		return entry.isDirectory()
			? listFiles(target, extensions)
			: extensions.has(path.extname(entry.name)) ? [target] : [];
	});
}

function listMarkdown(directory) {
	return listFiles(directory, new Set(['.md']));
}

for (const file of publicationFiles) {
	const source = fs.readFileSync(file, 'utf8');
	if (/7link|\bmall\b|\bfigma\b/iu.test(source)) failures.push(`Copied SevenLink assumption remains in ${path.relative(siteRoot, file)}`);
}

const routes = new Set(listMarkdown(docsRoot).map((file) => {
	const relative = path.relative(docsRoot, file).split(path.sep).join('/');
	return routeFor(relative);
}));
routes.add('/');

for (const file of listMarkdown(docsRoot)) {
	const source = fs.readFileSync(file, 'utf8');
	for (const match of source.matchAll(/\]\(([^)]+)\)/gu)) {
		const href = match[1];
		if (!href.startsWith('/') || href.startsWith('//')) continue;
		const route = href.split('#', 1)[0];
		const normalized = route.endsWith('/') ? route : `${route}/`;
		if (!routes.has(normalized)) {
			const publicTarget = path.join(siteRoot, 'public', ...normalized.split('/').filter(Boolean));
			if (!(fs.existsSync(publicTarget) && fs.statSync(publicTarget).isFile())) {
				failures.push(`Broken internal link in ${path.relative(docsRoot, file)}: ${href}`);
			}
		}
	}
}

for (const document of documents) {
	const target = path.join(docsRoot, document.target);
	if (!fs.existsSync(target)) failures.push(`Missing synchronized page: ${document.target}`);
}

if (failures.length) {
	console.error(`Documentation check failed (${failures.length}):`);
	for (const failure of failures) console.error(`- ${failure}`);
	process.exit(1);
}

console.log(`Documentation check passed: ${routes.size} routes, ${documents.length} synchronized sources.`);
