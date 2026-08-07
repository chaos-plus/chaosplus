// ponytail: old docs/*.md sources live directly in docs/src/content/docs/ now
export const documents = [
	{ source: 'README.md', target: 'overview.md', title: '项目说明' },
];

export function routeFor(target) {
	return `/${target.replace(/(?:index)?\.md$/u, '').replace(/\/$/u, '')}/`;
}
