# 红幕科技 RedHaze (redhaze.top) 安全评估报告

**评估日期**: 2026-06-18
**目标**: https://redhaze.top (47.86.177.216)
**技术栈**: nginx/1.24.0, Go 后端, 自定义前端
**子系统**: ID 身份中心 / Mall 商城 / CMS 内容中心 / Chat 消息中心 / Desk 工单 / Ads 广告

---

## 确认漏洞汇总 (16个)

### CRITICAL (严重)

#### 1. [Mall] 存储型 XSS - 商品评论 (Stored XSS via Product Reviews)
- **端点**: POST `/api/mall/products/{id}/reviews`
- **描述**: 商品评论的 `body_html` 字段接受任意 HTML 并直接渲染，未做任何过滤
- **PoC**: `{"rating":5,"body_html":"<script>alert('XSS_TEST_aiscan_8f2a')</script>"}` → 评论 ID 10 已成功存储
- **影响**: 可注入任意 JavaScript，窃取用户 Cookie/Session，钓鱼攻击

#### 2. [Chat] DOM-based XSS - WebSocket 消息渲染 (DOM XSS via Chat Messages)
- **端点**: WS `/ws/chat/{slug}` 和 POST `/api/chat/channels/{slug}/messages`
- **描述**: 聊天消息通过 `innerHTML = evt.body` 直接渲染，完全无过滤
- **PoC**: `{"body":"<img src=x onerror=alert('CHAT_XSS_8f2a')>"}` → 消息 ID 25 已成功发布到 free-lobby
- **影响**: WebSocket 实时 XSS，所有频道访问者受影响

#### 3. [Desk] 工单未授权访问 + IDOR (Unauthenticated Ticket Access + IDOR)
- **端点**: `https://desk.redhaze.top/desk/tickets/{id}`
- **描述**: 所有工单（ID 1-11）无需认证即可访问，包含敏感内部数据
- **泄露数据**: 
  - 内部备注(InternalNote): "vip refund pre-approved", "merged target"
  - 租户代码(TenantCode): ENT-D4E5F6, ENT-A1B2C3
  - 员工编号(AssigneeEmpNo): EMP00001, EMP00002, EMP00003, EMP00100
  - 完整Go结构体调试输出
- **影响**: 所有工单数据完全暴露，可枚举访问

#### 4. [Desk] 内部备注泄露漏洞 D-007 (Internal Note Leakage)
- **端点**: `https://desk.redhaze.top/desk/tickets/{id}`
- **描述**: 标记为 `internal_only` 的内部备注对所有访问者可见。表单明确标注: "内部备注（不应对客户可见 — 但 D-007 让它可见）"
- **影响**: 所有内部评注泄露，包括VIP退款预批等敏感商业决策

### HIGH (高危)

#### 5. [Mall] 供应商伪造 (Vendor Spoofing)
- **端点**: POST `/api/mall/products`
- **描述**: 创建商品时未校验 `supplier_vendor_no` 与当前登录供应商的一致性
- **PoC**: 以 VND-CN-099 身份创建了 `supplier_vendor_no: "VND-CN-001"` 的商品 (ID 34)
- **影响**: 攻击者可冒用任何供应商身份上架商品，破坏商家信誉

#### 6. [Mall] 价格篡改 (Price Manipulation)
- **端点**: POST `/api/mall/products` 和 `/api/mall/cart/items`
- **描述**: "HACKED PRODUCT 4" 标价 ¥0.01 可正常加入购物车并结算
- **PoC**: 订单 MO-2026-243268 已成功以 ¥0.01 总价创建
- **影响**: 攻击者可以任意低价购买高价商品

#### 7. [Desk] 未授权访问运维管理面板 (Unauthenticated Admin Console)
- **端点**: `https://desk.redhaze.top/desk/admin`
- **描述**: 运维管理面板无需认证即可访问，显示 Webhook 注册和通知模板编辑表单
- **影响**: 暴露管理功能入口，可配合 SSRF/SSTI 攻击

### MEDIUM (中危)

#### 8. [Desk] SSRF via Webhook (Server-Side Request Forgery)
- **端点**: POST `/api/desk/webhooks`
- **描述**: Webhook 表单接受任意 `target_url` 输入，API 虽需认证但页面完全未保护
- **PoC**: `{"ticket_id":1,"target_url":"http://127.0.0.1:80/","secret":"test"}`
- **影响**: 可攻击内网服务（API 认证后可利用）

#### 9. [Desk] SSTI - Go Template Injection (Server-Side Template Injection)
- **端点**: POST `/api/desk/templates`
- **描述**: 通知模板的 `body_template` 字段使用 Go `html/template` 语法，支持任意模板注入
- **影响**: 可注入恶意模板语法执行代码

#### 10. [Mall] 未验证供应商注册 (Unauthenticated Supplier Registration)
- **端点**: POST `/api/mall/auth/supplier/register`
- **描述**: 任何人可注册为供应商，无需KYC验证
- **PoC**: VND-CN-099 已成功注册并登录
- **影响**: 恶意攻击者可轻松获得供应商权限

### LOW-MEDIUM (低-中危)

#### 11. [Mall] API 限流绕过 (Rate Limiting Bypass)
- **描述**: 添加 `X-Mall-Crawler-Bypass: 1` 请求头即可绕过所有 API 频率限制
- **影响**: 可进行无限制的自动化攻击

#### 12. [CMS] 存储型 XSS - 投稿 (Stored XSS via CMS Posts)
- **端点**: POST `/api/cms/posts`
- **描述**: CMS 投稿的 `body_html` 字段支持 HTML 并无过滤
- **影响**: 可注入恶意脚本，对所有访问者执行

#### 13. [Desk] 存储型 XSS - 工单评论 (Stored XSS via Ticket Comments)
- **端点**: POST `/api/desk/tickets/{id}/comments`
- **描述**: 工单评论的 `body_html` 字段无过滤
- **影响**: XSS 在所有查看工单的用户浏览器中执行

#### 14. [Mall] 开放重定向 (Open Redirect)
- **端点**: `/api/mall/payment/return?next={url}`
- **描述**: `next` 参数可控，可用于钓鱼重定向
- **影响**: 钓鱼攻击辅助

#### 15. [Ads] 广告活动未授权访问 (Unauthenticated Campaign Access)
- **端点**: `https://ads.redhaze.top/ads/campaigns/{id}`
- **描述**: 所有广告活动（含测试数据如 UNAUTH_TEST, VENDOR_POC, PROXY_POC_TEST）无需认证即可访问
- **影响**: 业务数据和测试用例完全暴露

#### 16. [Desk] 未授权工单创建 (Unauthenticated Ticket Creation)
- **端点**: `https://desk.redhaze.top/desk/tickets/new`
- **描述**: 无需登录即可创建工单，可滥用为垃圾信息攻击
- **影响**: 工单系统可被滥用

---

## 未验证线索 (需要进一步验证)

- **支付回调绕过**: `/api/mall/payment/callback` 使用 GW-DEMO 硬编码 token，需浏览器交互验证
- **跨子系统 Session 复用**: 各子系统使用相同 Cookie 名但 domain 隔离，可能存在跨域会话利用
- **WebSocket 未授权**: Chat WebSocket 端点无认证，理论上可注入任意消息
- **CMS IDOR 编辑**: CMS 帖子的编辑/删除端点未测试

---

## 攻击面总结

| 子系统 | URL | 认证 | 主要风险 |
|--------|-----|------|---------|
| ID 门户 | id.redhaze.top | 多身份登录 | 无直接漏洞 |
| Mall 商城 | mall.redhaze.top | sess_guest/sess_ven | XSS, 价格篡改, 供应商伪造 |
| CMS/BBS | bbs.redhaze.top | 无 | XSS, 未授权发帖 |
| Chat 消息 | chat.redhaze.top | 无 | DOM XSS (WebSocket) |
| Desk 工单 | desk.redhaze.top | 无 | IDOR, 内部泄露, SSRF, SSTI |
| Ads 广告 | ads.redhaze.top | 无(只读) | 数据暴露 |

---

## 修复建议 (按优先级)

1. **全局**: 在所有 HTML 渲染点添加输出编码（HTML Entity Encoding），使用 `textContent` 替代 `innerHTML`
2. **Mall**: 校验 `supplier_vendor_no` 与认证会话一致性；添加价格合理性校验
3. **Desk**: 添加认证中间件，所有端点要求有效 Session；移除调试 JSON 输出；修复 D-007 内部备注泄露
4. **Chat**: 消息渲染使用 `textContent` 或 DOMPurify 过滤
5. **全局**: 移除 `X-Mall-Crawler-Bypass` 后门头或添加严格的合作伙伴验证
6. **Desk**: Webhook URL 添加白名单校验；Template 渲染使用沙箱模式
7. **Mall**: 供应商注册添加邮箱验证或管理员审核流程
