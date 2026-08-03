#!/usr/bin/env node

import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { parse } from 'parse5';
import { topNavigation } from '../src/site-nav.mjs';

const distRoot = path.resolve('dist');
const sourceRoot = path.resolve('src/content/docs');
const protectedBuild = Boolean(process.env.DOC_PASSWORD);
const failures = [];

function listFiles(directory) {
	return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
		const target = path.join(directory, entry.name);
		return entry.isDirectory() ? listFiles(target) : [target];
	});
}

function attribute(node, name) {
	return node.attrs?.find((entry) => entry.name === name)?.value;
}

function hasClass(node, name) {
	return (attribute(node, 'class') ?? '').split(/\s+/u).includes(name);
}

function localTarget(value, htmlPath) {
	if (!value || value.startsWith('#') || value.startsWith('//')) return null;
	if (/^(?:data|https?|mailto|tel):/iu.test(value)) return null;
	if (/^(?:javascript|vbscript):/iu.test(value)) return { unsafe: true, value };
	let pathname;
	try {
		pathname = decodeURI(value.split(/[?#]/u, 1)[0]).replace(/\\/gu, '/');
	} catch {
		return { invalid: true, value };
	}
	if (!pathname) return null;
	const currentDirectory = path.posix.dirname(path.relative(distRoot, htmlPath).split(path.sep).join('/'));
	const relative = pathname.startsWith('/')
		? path.posix.normalize(pathname.slice(1))
		: path.posix.normalize(path.posix.join(currentDirectory, pathname));
	if (relative === '..' || relative.startsWith('../')) return { invalid: true, value };
	return { relative, value };
}

function targetExists(relative) {
	const candidates = relative.endsWith('/')
		? [path.posix.join(relative, 'index.html')]
		: [relative, `${relative}.html`, path.posix.join(relative, 'index.html')];
	return candidates.some((candidate) => {
		const target = path.join(distRoot, ...candidate.split('/'));
		return fs.existsSync(target) && fs.statSync(target).isFile();
	});
}

function checkReference(value, htmlPath, context) {
	const target = localTarget(value, htmlPath);
	if (!target) return;
	const page = path.relative(distRoot, htmlPath).split(path.sep).join('/');
	if (target.unsafe) failures.push(`${page}: unsafe ${context} URL ${JSON.stringify(value)}`);
	else if (target.invalid) failures.push(`${page}: invalid ${context} URL ${JSON.stringify(value)}`);
	else if (!targetExists(target.relative)) failures.push(`${page}: missing ${context} target ${JSON.stringify(value)}`);
}

function checkHtml(htmlPath) {
	const document = parse(fs.readFileSync(htmlPath, 'utf8'));
	let language = '';
	let title = '';
	let mermaidCount = 0;
	let hasHeader = false;
	let hasMobileNavigation = false;
	let stylesheetCount = 0;
	const navigation = [];
	const mobileNavigation = [];
	const visit = (node) => {
		if (node.tagName === 'html') language = attribute(node, 'lang') ?? '';
		if (node.tagName === 'header' && hasClass(node, 'header')) hasHeader = true;
		if (node.tagName === 'details' && hasClass(node, 'mobile-navigation')) {
			hasMobileNavigation = true;
			const mobileNav = node.childNodes?.find((child) => child.tagName === 'nav');
			for (const child of mobileNav?.childNodes ?? []) {
				if (child.tagName !== 'a') continue;
				mobileNavigation.push({
					href: attribute(child, 'href'),
					label: child.childNodes?.map((entry) => entry.value ?? '').join('').trim(),
				});
			}
		}
		if (node.tagName === 'link' && attribute(node, 'rel') === 'stylesheet') stylesheetCount += 1;
		if (node.tagName === 'nav' && hasClass(node, 'top-nav')) {
			for (const child of node.childNodes ?? []) {
				if (child.tagName !== 'a') continue;
				navigation.push({
					href: attribute(child, 'href'),
					label: child.childNodes?.map((entry) => entry.value ?? '').join('').trim(),
				});
			}
		}
		if (node.tagName === 'title') {
			title = node.childNodes?.map((child) => child.value ?? '').join('').trim() ?? '';
		}
		if (node.tagName === 'pre' && (attribute(node, 'class') ?? '').split(/\s+/u).includes('mermaid')) {
			mermaidCount += 1;
		}
		for (const name of ['href', 'src', 'poster']) {
			const value = attribute(node, name);
			if (value) checkReference(value, htmlPath, name);
		}
		const srcset = attribute(node, 'srcset');
		if (srcset) {
			for (const candidate of srcset.split(',')) {
				checkReference(candidate.trim().split(/\s+/u, 1)[0], htmlPath, 'srcset');
			}
		}
		for (const child of node.childNodes ?? []) visit(child);
	};
	visit(document);
	const page = path.relative(distRoot, htmlPath).split(path.sep).join('/');
	if (language !== 'zh-CN') failures.push(`${page}: expected html lang=zh-CN, got ${JSON.stringify(language)}`);
	if (!title) failures.push(`${page}: missing document title`);
	if (!hasHeader) failures.push(`${page}: missing Starlight header shell`);
	if (!hasMobileNavigation) failures.push(`${page}: missing mobile top navigation`);
	if (stylesheetCount === 0) failures.push(`${page}: missing built stylesheet`);
	const expectedNavigation = topNavigation.map(({ href, label }) => ({ href, label }));
	if (JSON.stringify(navigation) !== JSON.stringify(expectedNavigation)) {
		failures.push(`${page}: top navigation does not match src/site-nav.mjs`);
	}
	if (JSON.stringify(mobileNavigation) !== JSON.stringify(expectedNavigation)) {
		failures.push(`${page}: mobile navigation does not match src/site-nav.mjs`);
	}
	return mermaidCount;
}

if (!fs.existsSync(distRoot)) throw new Error('Missing dist; run astro build first.');
const htmlFiles = listFiles(distRoot).filter((file) => file.endsWith('.html'));
if (htmlFiles.length === 0) throw new Error('No HTML files found in dist.');
const mermaidCount = htmlFiles.reduce((count, file) => count + checkHtml(file), 0);
const expectedMermaidCount = listFiles(sourceRoot)
	.filter((file) => file.endsWith('.md') || file.endsWith('.mdx'))
	.reduce((count, file) => {
		const source = fs.readFileSync(file, 'utf8');
		return count + (source.match(/^[\t ]*```mermaid(?:[\t ]+[^\r\n]*)?[\t ]*$/gmu)?.length ?? 0);
	}, 0);
if (mermaidCount !== expectedMermaidCount) {
	failures.push(`expected ${expectedMermaidCount} Mermaid diagrams from source, found ${mermaidCount} in built HTML`);
}

const pagefindRoot = path.join(distRoot, 'pagefind');
if (protectedBuild && fs.existsSync(pagefindRoot)) failures.push('protected build must not contain a Pagefind index');
if (!protectedBuild && !fs.existsSync(pagefindRoot)) failures.push('public build is missing its Pagefind index');

if (failures.length > 0) {
	console.error(`Built documentation check failed (${failures.length}):`);
	for (const failure of failures) console.error(`- ${failure}`);
	process.exit(1);
}

console.log(`Built documentation check passed: ${htmlFiles.length} HTML files, ${mermaidCount} Mermaid diagrams, all local targets resolved.`);
