# 离线资产盘点摘要

**整理时间**: 2026-06-16 15:56 CST
**方式**: 仅读取当前目录已有文件，未发起任何新扫描

---

## 目录概览

当前目录包含 **两个目标** 的扫描数据：

| 目标 | IP | 扫描深度 | 报告数量 |
|------|-----|----------|----------|
| redhaze.top | 47.86.177.216 | 深度（端口 + Web + API + POC） | 4 份（递进式） |
| ginandjuice.shop | — | 浅度（仅 spray 爬取） | 0 份（无专项报告） |

---

## 目标 1：redhaze.top — 报告演进与合并

### 四份报告对比

| 报告文件 | 阶段 | 发现数 | 核心发现 |
|----------|------|--------|----------|
| `redhaze_top_scan_report.md` | v1·基础扫描 | 1 确认 | `/api/portal/subsystems` 信息泄露（Medium） |
| `redhaze_top_report.md` | v2·API 深度 | 2 确认 | Desk 工单 IDOR + 登录接口用户枚举 |
| `redhaze_security_report.md` | v3·基础设施 | 2 确认 + 6 潜在 | Desk IDOR 含 PoC + 4 个数据库端口暴露 |
| `redhaze_top_security_report.md` | v4·全覆盖 | 13 项（1C+4H+4M+4L） | Ads 未授权创建（严重）+ CMS 注入 + 370 端口暴露 |

### 权威结论（以 v4 `redhaze_top_security_report.md` 为准）

**严重（1 项）：**
- Ads 广告活动创建无需认证（`POST /api/ads/campaigns`），攻击者可注入恶意落地页 URL 用于大规模钓鱼。已发现 Campaign #17 指向 `evil.example/phish` 且状态为 running，疑似已有攻击者利用。

**高危（4 项）：**
- Ads 开放重定向 — 落地页 `target_url` 无白名单校验，与 #1 链式利用
- Desk 工单 IDOR — `GET /api/desk/tickets/{id}` 无认证，泄露 internal_note、assignee_emp_no、tenant_code
- Desk 员工工作台公开暴露 — `/desk/console` 无需登录即可查看全部工单
- 370 个 TCP 端口公网暴露 — 含 SSH/MySQL/PostgreSQL/Redis/MongoDB/ES/Memcached/RDP/FTP

**中危（4 项）：**
- CMS 存储型 HTML 注入 — `POST /api/cms/posts` 的 body_html 未过滤（待审核后触发）
- Desk 渠道工作台公开暴露 — `/desk/vendor`
- Desk 新建工单表单公开 — `/desk/tickets/new`
- Ads 广告数据完全公开 — `/api/ads/campaigns` 无认证返回全部数据

**低危/信息（4 项）：**
- 子系统架构信息泄露
- nginx 版本信息披露
- 多个健康检查端点对外公开
- 运维账号编号泄露（SYS-ROOT001 等）

### 报告间矛盾点

| 矛盾 | v1 结论 | v4 结论 | 判定 |
|------|---------|---------|------|
| 300+ 端口状态 | CDN 全端口拦截（误报） | 真实开放端口 | **未验证** — 缺少协议层确认，v1 的 CDN 判断可能部分正确 |
| 数据库端口数 | 未发现 | 370 个含 4 类数据库 | **未验证** — zombie 弱口令检测全部超时 |
| MySQL 实例数 | 未发现 | 3306/3307/3308/33060 共 4 个 | **未验证** — 可能为 CDN 端口敲击响应 |

---

## 目标 2：ginandjuice.shop — 原始数据待分类

**数据来源**: `scan_results.jsonl`（spray 输出）

**扫描结果**: 77 个 Web 端点，20+ 个产品页（productId=1-18），6 篇博客文章，电商购物车/登录/账户页面

**值得关注的 JS 文件（文件名暗示潜在漏洞点）：**
- `stockCheck.js` — 库存检查逻辑
- `xmlStockCheckPayload.js` — XML 载荷，暗示 XXE/XML 注入测试点
- `searchLogger.js` — 搜索日志记录
- `deparam.js` — 参数反序列化
- `subscribeNow.js` — 订阅功能

**状态**: 零人工分类，无漏洞验证，无专项报告。spray 输出中无风险/漏洞标记（summary 行显示 0 risks 0 vulns）。

---

## 未完成线索（跨目标）

以下线索存在于原始数据中但尚未形成结论：

| 线索 | 目标 | 阻断原因 |
|------|------|----------|
| 数据库弱口令（MySQL/PG/Redis/Mongo） | redhaze.top | zombie 检测超时，需人工验证 |
| Redis 未授权访问 | redhaze.top | 协议交互异常（可能为模拟端口或 WAF） |
| Desk Webhook SSRF + 模板 SSTI | redhaze.top | API 返回 401，需认证后测试 |
| Chat 频道消息注入 | redhaze.top | 匿名发言是否经过 XSS 过滤未验证 |
| 访客注册无速率限制 | redhaze.top | 已确认可批量注册，未评估业务影响 |
| ginandjuice.shop 全量 | ginandjuice.shop | 无任何人工分析 |

---

## 原始工件索引

| 文件 | 类型 | 大小/行数 | 内容说明 |
|------|------|-----------|----------|
| `scan_result.json` | JSON | 16 行 | redhaze.top 快速扫描（14 服务/0 Web/70s） |
| `scan_results.jsonl` | JSONL | 155 行 | ginandjuice.shop spray 输出（77 Web 端点） |
| `redhaze_spray.json` | JSONL | 23 条 | redhaze.top crawl+finger 结果 |
| `redhaze_spray2.json` | JSONL | 23 条 | **与 spray.json 内容重复**，可清理 |
| `redhaze_top_scan_report.md` | Markdown | 131 行 | v1 报告 |
| `redhaze_top_report.md` | Markdown | 99 行 | v2 报告 |
| `redhaze_security_report.md` | Markdown | 119 行 | v3 报告 |
| `redhaze_top_security_report.md` | Markdown | 269 行 | **v4 报告（最权威版本）** |
| `redhaze_deepseek_aiscan.err.log` | 日志 | 445 行 | Deepseek agent 完整执行链 |
| `redhaze_deepseek_report_gen.err.log` | 日志 | 154 行 | 报告生成过程记录 |
| `redhaze_deepseek_aiscan.out.log` | 日志 | — | stdout 日志（未读取） |
| `redhaze_deepseek_report_gen.out.log` | 日志 | — | stdout 日志（未读取） |
| `refer/` | 目录 | 500+ 文件 | pi 项目 Git 克隆（开源编程 agent，与扫描无关） |

---

## 建议操作

1. **以 `redhaze_top_security_report.md`（v4）为权威版本**，v1-v3 中存在已更正的误报（CDN 端口误判）且发现覆盖不全
2. **ginandjuice.shop 需独立分类**：按端点类型分组（产品页、博客、静态资源、API 入口），基于 JS 文件名推断漏洞点
3. **标记未验证项**：端口真实性和数据库认证状态在报告中应标注为"未验证"而非"已确认"
4. **清理重复文件**：`redhaze_spray2.json` 与 `redhaze_spray.json` 内容相同，可删除
5. **reports 归档建议**：保留 v4 报告，v1-v3 可移至 `archive/` 子目录或直接删除
