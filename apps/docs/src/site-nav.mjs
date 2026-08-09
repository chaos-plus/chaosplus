export const topNavigation = [
	{ label: '首页', title: '首页', href: '/' },
	{ label: '总览', title: '项目总览', href: '/overview/' },
	{ label: '架构', title: 'IAM 平台架构', href: '/architecture/iam-platform/' },
	{ label: 'IAM', title: '身份与权限', href: '/iam/authorization/' },
	{ label: '管理端', title: 'IAM 管理端', href: '/frontend/admin-console/' },
	{ label: '部署', title: '部署与运行', href: '/operations/deployment/' },
	{ label: '质量', title: '质量与验收', href: '/quality/admin-acceptance/' },
	{ label: '技能', title: '开发技能与质量闸门', href: '/skills/' },
];

export const sidebar = [
	{ label: '开始', items: [
		{ label: '文档总览', slug: 'index' },
		{ label: '项目说明', slug: 'overview' },
		{ label: '开发技能与质量闸门', slug: 'skills' },
	] },
	{ label: '架构', items: [
		{ label: 'IAM 平台架构', slug: 'architecture/iam-platform' },
		{ label: '模块组织', slug: 'architecture/modules' },
	] },
	{ label: '身份与权限', items: [
		{ label: '授权设计', slug: 'iam/authorization' },
		{ label: '授权写入链路', slug: 'iam/authorization-writes' },
		{ label: '访问治理', slug: 'iam/access-governance' },
		{ label: 'SCIM 2.0 预配', slug: 'iam/scim-provisioning' },
	] },
	{ label: '管理端', items: [
		{ label: 'IAM 管理端', slug: 'frontend/admin-console' },
	] },
	{ label: '运行与验收', items: [
		{ label: '部署', slug: 'operations/deployment' },
		{ label: '管理端验收', slug: 'quality/admin-acceptance' },
	] },
];
