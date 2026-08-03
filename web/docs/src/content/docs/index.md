---
title: Chaosplus IAM
description: Chaosplus 自研身份与访问管理平台的架构、开发、部署与验收入口
template: splash
---

<div class="home">

<section class="hero">
	<div class="hero__inner">
		<span class="hero__eyebrow">CHAOSPLUS · SELF-HOSTED IAM</span>
		<h1 class="hero__title">Chaosplus IAM</h1>
		<p class="hero__tagline">面向自托管场景的纯自研身份与访问管理平台。统一承载身份、认证、会话、OAuth/OIDC、租户授权和安全审计，并为后续 tenant → entity → business resources 层级保留清晰边界。</p>
		<div class="hero__actions">
			<a class="btn btn--primary" href="/architecture/iam-platform/">阅读平台架构 <span aria-hidden="true">→</span></a>
			<a class="btn btn--ghost" href="/frontend/admin-console/">查看管理端</a>
		</div>
	</div>
	<div class="hero__media">
		<img src="/img/chaosplus-admin.png" alt="Chaosplus IAM 管理端身份控制面板" width="1440" height="1000" loading="eager" />
	</div>
</section>

<section class="flow" aria-label="IAM 核心链路">
	<span class="flow__node"><em>ID</em>身份</span>
	<span class="flow__node"><em>AUTHN</em>认证</span>
	<span class="flow__node"><em>OAUTH</em>协议</span>
	<span class="flow__node"><em>AUTHZ</em>授权</span>
	<span class="flow__node"><em>AUDIT</em>审计</span>
</section>

<h2 class="section-title">工程文档</h2>

<div class="home-grid">
	<a class="home-card" href="/architecture/iam-platform/">
		<span class="home-ico" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect width="7" height="7" x="3" y="3" rx="1"/><rect width="7" height="7" x="14" y="3" rx="1"/><rect width="7" height="7" x="3" y="14" rx="1"/><rect width="7" height="7" x="14" y="14" rx="1"/></svg></span>
		<span class="home-badge">ARCH</span>
		<strong>IAM 平台架构</strong>
		<span class="home-desc">目标边界、领域模型、数据所有权与演进路线</span>
		<span class="home-arrow" aria-hidden="true">→</span>
	</a>
	<a class="home-card" href="/architecture/modules/">
		<span class="home-ico" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2 2 7l10 5 10-5-10-5Z"/><path d="m2 17 10 5 10-5"/><path d="m2 12 10 5 10-5"/></svg></span>
		<span class="home-badge">CODE</span>
		<strong>模块组织</strong>
		<span class="home-desc">Go 模块职责、依赖方向、组合根与代码落点</span>
		<span class="home-arrow" aria-hidden="true">→</span>
	</a>
	<a class="home-card" href="/iam/authorization/">
		<span class="home-ico" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M20 13c0 5-3.5 7.5-8 9-4.5-1.5-8-4-8-9V5l8-3 8 3v8Z"/><path d="m9 12 2 2 4-4"/></svg></span>
		<span class="home-badge">AUTHZ</span>
		<strong>授权设计</strong>
		<span class="home-desc">租户 RBAC、权限目录、数据范围与策略执行</span>
		<span class="home-arrow" aria-hidden="true">→</span>
	</a>
	<a class="home-card" href="/iam/authorization-writes/">
		<span class="home-ico" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><circle cx="7.5" cy="15.5" r="5.5"/><path d="m21 2-9.6 9.6"/><path d="m15.5 7.5 3 3L22 7l-3-3"/></svg></span>
		<span class="home-badge">WRITE</span>
		<strong>授权写入链路</strong>
		<span class="home-desc">管理命令、事务边界、并发控制与审计要求</span>
		<span class="home-arrow" aria-hidden="true">→</span>
	</a>
	<a class="home-card" href="/frontend/admin-console/">
		<span class="home-ico" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect width="20" height="14" x="2" y="3" rx="2"/><path d="M8 21h8"/><path d="M12 17v4"/></svg></span>
		<span class="home-badge">ADMIN</span>
		<strong>IAM 管理端</strong>
		<span class="home-desc">管理工作流、接口消费、响应式行为与验收口径</span>
		<span class="home-arrow" aria-hidden="true">→</span>
	</a>
	<a class="home-card" href="/operations/deployment/">
		<span class="home-ico" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M4.5 16.5c-1.5 1.26-2 5-2 5s3.74-.5 5-2c.71-.84.7-2.13-.09-2.91a2.18 2.18 0 0 0-2.91-.09Z"/><path d="m12 15-3-3a22 22 0 0 1 2-3.95A12.67 12.67 0 0 1 22 2c0 2.72-.78 7.5-6.05 11a22.35 22.35 0 0 1-3.95 2Z"/><path d="M9 12H4s.55-3.03 2-4c1.62-1.08 5 0 5 0"/><path d="M12 15v5s3.03-.55 4-2c1.08-1.62 0-5 0-5"/></svg></span>
		<span class="home-badge">OPS</span>
		<strong>部署与验收</strong>
		<span class="home-desc">三种数据库配置、容器运行、质量闸门与真实验证</span>
		<span class="home-arrow" aria-hidden="true">→</span>
	</a>
</div>

</div>
