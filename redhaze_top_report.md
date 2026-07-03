## Summary

对 redhaze.top（红幕科技 RedHaze Group）进行全面安全测试，发现 6 个子系统、多个 API 端点及身份认证入口。确认 **1 个高危信息泄露漏洞**（Desk 工单系统未授权访问）、**1 个中危用户枚举漏洞**（登录接口差异化响应），以及多项需要进一步验证的安全风险。整体攻击面较大，当前最突出的问题是 Desk 子系统缺乏访问控制，导致内部工单、员工标识、租户编码等敏感数据对外暴露。

## Critical Loots

### 1. RedHaze Desk 未授权访问 — 工单数据与内部评论泄露 (High)

**目标**: desk.redhaze.top

**描述**: RedHaze Desk 工单系统的员工工作台(/desk/console)、运维管理(/desk/admin)、以及所有工单详情页(/desk/tickets/{id})无需任何身份认证即可访问。页面不仅展示工单标题、状态、SLA、关联订单号、提交人标识，还通过 HTML `<details>` 标签暴露 Go 语言 struct 调试输出，内含 `InternalNote` 等内部备注字段。内部评论（标记为 "internal-only"）同样对未认证用户可见。

**影响**: 攻击者可遍历工单 ID 获取所有工单数据，包括员工工号 (EMP00001-EMP00100)、客户标识 (G-cust0001 等)、租户编码 (ENT-D4E5F6)、订单号 (MO-2026-100001 等) 及内部预审批备注 ("vip refund pre-approved")。可用于社会工程攻击、权限提升尝试的侦察。

**PoC**:
```
# 无需认证，直接访问员工工作台
curl -sk https://desk.redhaze.top/desk/console

# 遍历工单 ID，获取敏感数据
curl -sk https://desk.redhaze.top/desk/tickets/1
curl -sk https://desk.redhaze.top/desk/tickets/2

# 访问运维管理页面（含 webhook 注册和模板编辑表单）
curl -sk https://desk.redhaze.top/desk/admin
```

### 2. 身份认证接口用户枚举 (Medium)

**目标**: id.redhaze.top/api/portal/auth/*

**描述**: 员工、企业租户、系统运维的登录接口对不同错误条件返回差异化消息，可被用于用户/租户枚举：
- `{"message":"staff credential mismatch"}` — 员工号存在，密码错误
- `{"message":"tenant credential mismatch"}` — 租户编码存在，密码错误
- `{"message":"operator not found"}` — 运维身份不存在

**PoC**:
```
# 枚举员工号 (EMP00001 存在)
curl -sk -X POST https://id.redhaze.top/api/portal/auth/employee/login \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "emp_no=EMP00001&password=wrong"

# 枚举租户 (ENT-000001 存在)
curl -sk -X POST https://id.redhaze.top/api/portal/auth/enterprise/login \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "tenant_code=ENT-000001&password=wrong"
```

## Potential Risks (Unverified)

- **Desk Webhook SSRF**: 运维管理页面的 webhook 注册表单允许指定任意 `target_url`，API 虽返回 "login required"，但若绕过认证可能存在 SSRF 风险 — 需进一步测试
- **Desk 通知模板注入**: 编辑通知模板功能使用 Go `html/template` 语法，虽 html/template 对 XSS 安全，但如果后端使用了 text/template 可能存在 SSTI — 需认证后测试
- **CMS/Ads/Chat 子系统未授权功能**: bbs.redhaze.top、ads.redhaze.top、chat.redhaze.top 均可匿名访问，CMS 允许未登录投稿，Chat 提供公开频道 — 此设计可能是预期行为但增加了攻击面
- **访客注册无验证码**: `/api/portal/auth/guest/register` 无速率限制或 CAPTCHA 保护，可批量注册

## Services & Fingerprints

| 服务 | 域名/IP | 指纹 |
|------|--------|------|
| nginx | 47.86.177.216:443 | nginx/1.24.0 (Ubuntu) |
| SSH | 47.86.177.216:22 | OpenSSH |
| FTP | 47.86.177.216:21 | (未识别版本) |
| RedHaze ID (SSO) | id.redhaze.top | build v0.6.4+0acb4ea |
| RedHaze Mall | mall.redhaze.top | 电商系统 |
| RedHaze Desk | desk.redhaze.top | 工单系统 (Go) |
| RedHaze Ads | ads.redhaze.top | 广告平台 |
| RedHaze Chat | chat.redhaze.top | 实时消息 |
| RedHaze CMS | bbs.redhaze.top | 内容管理/BBS |

**API 端点枚举**:
- `POST /api/portal/auth/{role}/login` (enterprise/employee/guest/sysop/vendor)
- `POST /api/portal/auth/guest/register`
- `GET /api/portal/subsystems` (无需认证)
- `GET /api/portal/health`
- `POST /api/desk/tickets`
- `POST /api/desk/tickets/{id}/comments`
- `POST /api/desk/webhooks`
- `POST /api/desk/templates`
- `GET /api/mall/health`
- `GET /api/ads/health`
- `GET /api/chat/health`
- `GET /api/cms/health`

## Weak Credentials

未发现弱口令。测试了常见组合 (admin/admin123, root/password, admin/password) 均失败。但访客注册功能可被滥用创建任意账户（已验证成功注册 G-d2255ca8）。

## Recommendations

1. **紧急**: 为 Desk 子系统所有页面添加身份认证检查，包括 `/desk/console`、`/desk/admin`、`/desk/tickets/{id}`。移除调试 JSON 输出 (`<details>` 块) 或限制为仅认证运维人员可见。

2. **高优先级**: 统一登录接口的错误响应为通用消息（如 "invalid credentials"），防止用户/租户枚举。

3. **中优先级**: 为访客注册接口添加速率限制和 CAPTCHA 验证，防止批量账号创建。

4. **建议**: 审查所有子系统（CMS/Chat/Ads/Mall/Desk）的 API 授权逻辑，确保 `/api/*` 端点均验证会话身份。对 Desk 的 webhook 和模板功能进行 SSRF/SSTI 专项测试。

5. **运维**: 考虑为 nginx 1.24.0 应用安全补丁（当前存在 CVE-2023-44487 HTTP/2 Rapid Reset 漏洞），并在 WAF 层面配置合理的速率限制（当前扫描触发了连接封锁，表明已有一定防护但可进一步优化）。
