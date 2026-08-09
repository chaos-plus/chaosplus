import { execSync } from 'node:child_process';
import { readFileSync } from 'node:fs';

export interface PageMeta {
	words: number;
	minutes: number;
	created: string;
	updated: string;
	author: string;
	hash: string;
}

// Build-time only: word count / reading time from the source file, plus
// created / updated / author / commit-hash / full history from `git log`.
export function getPageMeta(file: string): PageMeta {
	let words = 0;
	let minutes = 1;
	try {
		const raw = readFileSync(file, 'utf8').replace(/^---[\s\S]*?\n---/, '');
		const cjk = (raw.match(/[一-鿿]/g) || []).length;
		const en = (raw.replace(/[一-鿿]/g, ' ').match(/[A-Za-z0-9]+/g) || []).length;
		words = cjk + en;
		minutes = Math.max(1, Math.round(cjk / 400 + en / 200)); // ~400 字/min · ~200 词/min
	} catch {}

	const git = (args: string) => {
		try {
			return execSync(`git log ${args} -- "${file}"`, { encoding: 'utf8' }).trim();
		} catch {
			return '';
		}
	};
	const updated = git('-1 --format=%ad --date=short');
	const author = git('-1 --format=%an');
	const hash = git('-1 --format=%h');
	const createdLog = git('--follow --format=%ad --date=short');
	const created = createdLog ? (createdLog.split('\n').pop() as string) : '';

	return { words, minutes, created, updated, author, hash };
}
