export const documents = [
	{ source: 'README.md', target: 'overview.md', title: '项目说明' },
	{ source: 'docs/iam-platform-architecture.md', target: 'architecture/iam-platform.md', title: 'IAM 平台架构' },
	{ source: 'docs/module-structure.md', target: 'architecture/modules.md', title: '模块组织' },
	{ source: 'docs/plugin-system.md', target: 'architecture/plugin-system.md', title: 'WASM 插件系统' },
	{ source: 'docs/authz-design.md', target: 'iam/authorization.md', title: '授权设计' },
	{ source: 'docs/iam-authorization-writes.md', target: 'iam/authorization-writes.md', title: '授权写入链路' },
	{ source: 'docs/access-governance.md', target: 'iam/access-governance.md', title: '访问治理' },
	{ source: 'docs/scim-provisioning.md', target: 'iam/scim-provisioning.md', title: 'SCIM 2.0 预配' },
	{ source: 'docs/iam-admin-console.md', target: 'frontend/admin-console.md', title: 'IAM 管理端' },
	{ source: 'docs/deployment.md', target: 'operations/deployment.md', title: '部署' },
	{ source: 'docs/acceptance-admin-console.md', target: 'quality/admin-acceptance.md', title: '管理端验收' },
];

export function routeFor(target) {
	return `/${target.replace(/(?:index)?\.md$/u, '').replace(/\/$/u, '')}/`;
}
