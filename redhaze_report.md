# redhaze.top 安全评估报告

## 概述
- **目标**: redhaze.top (IP: 47.86.177.216)
- **服务器**: nginx/1.24.0 (Ubuntu)
- **构建版本**: v0.6.4+0acb4ea
- **扫描时间**: 2026-06-18 13:01-13:07 UTC+8
- **发现漏洞**: 12 个 (2 Critical, 5 High, 3 Medium, 2 Low)

## 目标拓扑
| 子域名 | 服务 | 路径 | 认证 |
|--------|------|------|------|
| id.redhaze.top | RedHaze ID 门户 | /home, /portal | 部分需认证 |
| bbs.redhaze.top | RedHaze CMS | /cms | 部分需认证 |
| chat.redhaze.top | RedHaze Chat | /chat | 无认证 |
| mall.redhaze.top | RedHaze Mall | /mall | 部分需认证 |
| desk.redhaze.top | RedHaze Desk | /desk | 无认证 |
| ads.redhaze.top | RedHaze Ads | /ads | 部分需认证 |

---

## 漏洞清单

### 🔴 Critical

#### [VULN-01] 未授权存储型 HTML 注入 / 页面篡改 (Ads Creative API)
- **URL**: POST https://ads.redhaze.top/api/ads/creatives
- **描述**: 广告创意 API 无需任何认证即可创建包含任意 HTML 的创意内容。创建的创意会立即渲染在所有访问该广告系列的落地页上。
- **PoC**:
```
POST /api/ads/creatives HTTP/1.1
Content-Type: application/json

{"campaign_id":1,"title":"DEFACEMENT","body_html":"<div style=\"position:fixed;top:0;left:0;width:100%;background:red;z-index:99999;padding:20px\"><h1>DEFACED</h1></div>","image_url":"/static/img/ads/jet-1.jpg","cta_label":"Test"}
```
- **验证**: 落地页 `https://ads.redhaze.top/api/ads/click?creative_id=21` 上成功渲染 `<h1>DEFACED BY AISCAN</h1>`
- **影响**: 攻击者可在高流量广告落地页注入任意内容，实现页面篡改、钓鱼攻击或恶意软件分发
- **修复**: 对 `/api/ads/creatives` 端点添加认证检查，并对 body_html 进行输出编码

#### [VULN-02] 未授权存储型 XSS (Ads Creative API)
- **URL**: POST https://ads.redhaze.top/api/ads/creatives
- **描述**: 同一接口接受 `<img src=x onerror=alert(1)>` 和 `<svg onload=alert(1)>` 等 XSS payload
- **PoC**: 创意 ID 19 (`<img src=x onerror=alert("AISCAN_XSS_VERIFIED")>`) 和 ID 20 (`<svg onload=alert("AISCAN_SVG_XSS")>`) 已成功创建并渲染在落地页
- **影响**: 攻击者可执行任意 JavaScript，窃取用户 Cookie/Token、重定向到钓鱼页面
- **修复**: 对 body_html 字段进行严格的 HTML 净化（如使用 bluemonday）

### 🟠 High

#### [VULN-03] 未授权访问后台管理页面 (Desk Admin)
- **URL**: https://desk.redhaze.top/desk/admin
- **描述**: Webhook 注册和通知模板管理页面无需认证即可访问
- **证据**: 页面标题"运维管理"，显示 Webhook 注册表单和模板编辑表单
- **影响**: 虽然 API 端返回 401，但 UI 层面的暴露已泄露内部功能结构
- **修复**: 在路由层添加认证中间件

#### [VULN-04] 未授权访问员工工单控制台 (Desk Console)
- **URL**: https://desk.redhaze.top/desk/console
- **描述**: 完整工单列表（含客户PII、员工分配、SLA信息）对匿名用户可见
- **证据**: 页面显示 8+ 条工单，包含客户标识符（G-cust0001等）、员工编号（EMP00002等）
- **影响**: 客户个人信息、内部工单流程完全暴露
- **修复**: 添加认证检查

#### [VULN-05] IDOR - 工单遍历
- **URL**: https://desk.redhaze.top/desk/tickets/{1..11}
- **描述**: 通过递增 ID 可匿名访问所有工单详情
- **证据**: 成功访问工单 1-11，包括 closed/cancelled 状态工单
- **影响**: 跨客户、跨员工数据泄露，内部审批备注暴露
- **修复**: 实施基于会话的访问控制，验证请求者身份与工单归属关系

#### [VULN-06] 敏感凭证泄露 (Ads Admin 页面)
- **URL**: https://ads.redhaze.top/ads/admin
- **描述**: 页面源码直接暴露了所有系统账号列表和 Cookie 名称
- **泄露内容**:
  - 渠道伙伴账号: VND-CN-001, VND-CN-002, VND-EU-007
  - 管理员账号: SYS-ROOT001, SYS-NET-OPS-002, SYS-DBA-003
  - Session Cookie: sess_ven, sess_sys, sess_emp, sess_guest
- **影响**: 攻击者可针对已知账号进行定向爆破或会话劫持
- **修复**: 从页面中移除凭据信息，不向前端暴露内部账号体系

#### [VULN-07] 内部备注泄露
- **URL**: https://desk.redhaze.top/desk/tickets/*
- **描述**: 工单调试 JSON 中包含 InternalNote 字段，如"vip refund pre-approved 800"
- **证据**: 工单 TKT-2026-0002 的 InternalNote 字段显示"vip refund pre-approved 800"
- **影响**: 内部运营决策、退款预批等敏感信息泄露
- **修复**: 移除调试 JSON 输出，确保内部备注字段不在公开页面渲染

### 🟡 Medium

#### [VULN-08] 未授权 CMS 内容投稿
- **URL**: https://bbs.redhaze.top/cms/compose
- **描述**: 任何人可访问投稿页面，支持 HTML 正文提交
- **影响**: 垃圾内容泛滥，潜在的存储型 XSS（需审核后展示）

#### [VULN-09] 未授权聊天室访问
- **URL**: https://chat.redhaze.top/chat/channels/free-lobby
- **描述**: 聊天频道无需认证即可查看完整历史消息和发送消息
- **影响**: 信息泄露，潜在的社工攻击

#### [VULN-10] API 子系统枚举
- **URL**: https://id.redhaze.top/api/portal/subsystems
- **描述**: 无需认证返回完整子系统列表、内部路径和健康检查端点
- **影响**: 攻击面枚举，加速渗透

### 🔵 Low

#### [VULN-11] 调试信息泄露
- **URL**: 每个工单详情页
- **描述**: "调试 · 工单原始 JSON" 直接打印 Go 结构体，暴露内部数据模型

#### [VULN-12] 版本信息泄露
- **证据**: 页面底部均显示 "build v0.6.4+0acb4ea"
- **影响**: 攻击者可针对特定版本寻找已知漏洞

---

## 风险评分汇总

| 严重度 | 数量 | 漏洞编号 |
|--------|------|----------|
| Critical | 2 | VULN-01, VULN-02 |
| High | 5 | VULN-03, VULN-04, VULN-05, VULN-06, VULN-07 |
| Medium | 3 | VULN-08, VULN-09, VULN-10 |
| Low | 2 | VULN-11, VULN-12 |

## 优先修复建议

1. **立即**: 对 `/api/ads/creatives` 添加认证和 HTML 净化 (VULN-01, VULN-02)
2. **立即**: 对 `/desk/*` 路由添加认证中间件 (VULN-03, VULN-04, VULN-05)
3. **紧急**: 从 ads/admin 页面移除凭据信息 (VULN-06)
4. **紧急**: 移除工单页面的调试 JSON 输出 (VULN-07, VULN-11)
5. **尽快**: 对 CMS compose 和 Chat 频道添加访问控制 (VULN-08, VULN-09)
