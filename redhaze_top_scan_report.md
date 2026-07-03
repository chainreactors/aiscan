# redhaze.top 安全扫描报告

**扫描时间**: 2026-06-15 20:13 - 20:25 PDT
**目标**: redhaze.top (解析至 47.86.177.216)
**扫描范围**: 端口扫描 (3000-10000)、Web 服务探测、子域名枚举、API 审计

---

## Summary

对 redhaze.top 进行了全面安全扫描，涵盖端口发现、服务识别、子域名枚举、Web 路径探测和 API 审计。目标是一个名为「红幕科技 RedHaze Group」的企业 SaaS 门户系统，包含 6 个子系统（ID 身份中心、广告平台、消息中心、CMS 内容中心、工单系统、商城）。发现 **1 个确认的信息泄露漏洞** 和若干需要关注的配置问题。未发现可直接利用的高危远程代码执行漏洞。

扫描统计数据：
- 发现子域名：7 个（ads, bbs, chat, desk, id, mall, www）
- 开放 TCP 端口（疑似 CDN 全端口拦截）：约 300+ 个
- Web 端点探测：35+ 路径
- API 端点发现：2 个可匿名访问
- 确认漏洞：1 个（中等严重性）
- 潜在风险：2 个

---

## Critical Loots

### [confirmed] 未授权 API 信息泄露 — `/api/portal/subsystems`

- **目标**: https://id.redhaze.top/api/portal/subsystems
- **严重性**: 中 (Medium)
- **类型**: 敏感信息泄露 (Information Disclosure)
- **状态**: **[verified]** 无需任何认证即可获取完整系统架构信息

**描述**: API 端点 `/api/portal/subsystems` 在未认证的情况下返回所有 6 个子系统的完整信息，包括内部名称、中文描述、API 路径、健康检查端点、公网 URL 和功能说明。

**泄露数据**:
```json
{
  "subsystems": [
    {"id":"ads", "name":"RedHaze Ads", "health_path":"/api/ads/health", "public_url":"https://ads.redhaze.top"},
    {"id":"chat","name":"RedHaze Chat", "health_path":"/api/chat/health","public_url":"https://chat.redhaze.top"},
    {"id":"cms", "name":"RedHaze CMS",   "health_path":"/api/cms/health", "public_url":"https://bbs.redhaze.top"},
    {"id":"desk","name":"RedHaze Desk",  "health_path":"/api/desk/health","public_url":"https://desk.redhaze.top"},
    {"id":"id",  "name":"RedHaze ID",    "health_path":"/api/portal/health","public_url":"https://id.redhaze.top"},
    {"id":"mall","name":"RedHaze Mall",  "health_path":"/api/mall/health","public_url":"https://mall.redhaze.top"}
  ],
  "total": 6
}
```

**影响**: 攻击者无需任何认证即可获取整个企业 SaaS 架构拓扑图，为后续针对性攻击提供精确的内部分布图。每个子系统的功能描述（如「含访客投稿审核流程」「跨子系统退款」等）进一步暴露了业务流程细节。

**PoC**:
```bash
curl -s "https://id.redhaze.top/api/portal/subsystems" | jq .
```

**修复建议**: 对该 API 端点添加认证中间件，仅允许已认证的内部服务或授权用户访问。

---

## Potential Risks (Unverified)

### [unverified] 未认证的健康检查端点暴露

- **目标**: `https://id.redhaze.top/api/portal/health`, `https://ads.redhaze.top/api/ads/health`, `https://mall.redhaze.top/api/mall/health`, `https://bbs.redhaze.top/api/cms/health`
- **严重性**: 低 (Low)
- **描述**: 多个子系统的健康检查端点无需认证即可访问，返回 `{"status":"ok","subsystem":"xxx"}`。虽然健康检查本身敏感度较低，但可被用于服务发现和存活探测。
- **验证步骤**: 手动确认是否需要认证保护。

### [unverified] 门户页暴露内部版本信息

- **目标**: `https://id.redhaze.top/portal`
- **描述**: 门户页面 HTML 中包含构建版本信息 `build v0.6.4+0acb4ea`。虽然当前版本号不直接导致漏洞，但可能帮助攻击者识别使用的框架和版本范围。
- **验证步骤**: 确认该版本号是否可关联到已知 CVE。

---

## Services & Fingerprints

| 服务 | 详情 |
|------|------|
| Web 服务器 | nginx/1.24.0 (Ubuntu) — HTTP/2 |
| 主站 | https://id.redhaze.top/home — 红幕科技 RedHaze Group · 全球综合集团门户 |
| 统一门户 | https://id.redhaze.top/portal — RedHaze ID 五类身份认证入口 |
| CDN/WAF | 疑似腾讯云/阿里云 CDN（全端口 TCP 握手响应，3000-10000 范围均返回 open） |
| SSL 证书 | SAN: ads, bbs, chat, desk, id, mall, redhaze.top, www |
| 后端框架 | 未知（非 Spring Boot actuator；非标准 REST 模式） |

### 子系统清单（从 API 泄露获取）

| ID | 名称 | 中文名 | URL |
|----|------|--------|-----|
| id | RedHaze ID | 红雾身份中心 | https://id.redhaze.top |
| ads | RedHaze Ads | 红雾广告 | https://ads.redhaze.top |
| chat | RedHaze Chat | 红雾消息中心 | https://chat.redhaze.top |
| cms | RedHaze CMS | 红雾内容中心 | https://bbs.redhaze.top |
| desk | RedHaze Desk | 红雾工单 | https://desk.redhaze.top |
| mall | RedHaze Mall | 红雾商城 | https://mall.redhaze.top |

### 开放端口（CDN 全端口拦截特征）

以下端口均返回 TCP "open"，判定为 CDN/WAF 的端口敲击响应，非真实服务：
- 端口范围 3000-10000 几乎全部报 open
- gogo 猜测到的服务类型（mysql:3307, squid:3128, zookeeper:3888, jboss:3873, nats:4222, sybase:5000）均为 CDN 端口响应导致的误报

---

## Weak Credentials

未进行弱口令扫描（需要 zombie 支持，且目标无明显登录 API 端点暴露）。

---

## Dismissed Leads

以下路径已探测并确认不存在（404）：
- `/admin`, `/login`, `/register`, `/console`, `/dashboard`, `/graphql`
- `/actuator`, `/swagger-ui.html`, `/api-docs`, `/swagger`
- `/api/portal/register`, `/api/portal/login`, `/api/portal/guest`
- `/api/v1/*`, `/api/v2/*`
- `/.env`, `/robots.txt`, `/sitemap.xml`, `/openapi.json`, `/.well-known/security.txt`
- CORS: OPTIONS 请求返回 405，无 Access-Control-Allow-Origin 头，无 CORS 漏洞

---

## Recommendations

1. **立即修复 API 信息泄露** — 为 `/api/portal/subsystems` 添加认证机制，限制仅内部服务或已认证用户访问。该 API 暴露了完整的系统架构拓扑。
2. **添加安全响应头** — 当前缺失 CSP、HSTS (Strict-Transport-Security)、X-Frame-Options、X-Content-Type-Options、Referrer-Policy。建议为所有响应添加基础安全头。
3. **隐藏服务器版本信息** — 在 nginx 配置中设置 `server_tokens off;` 以移除 `nginx/1.24.0` 版本的披露。
4. **审查健康检查端点的访问控制** — 评估是否需要为 `/api/*/health` 端点添加认证。
5. **移除前端构建版本信息** — 移除 `/portal` 页面中嵌入的 `v0.6.4+0acb4ea` 构建版本号。
