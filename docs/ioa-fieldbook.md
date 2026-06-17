# aiscan + IOA 群狼战术实践

> 这篇来自早期 `aiscan + IOA 实战手册` 的思路整理。原稿里有“群狼战术、云端狼群、RedC + Fleet、Cyberhub 自建武器库”等内容；这里保留方法和架构，去掉真实基础设施、密钥、目标信息和过度攻击化表述，作为可发布版本。

---

## 阅读路径

这套打法不是从“一个 AI 自动给出所有结论”开始，而是按阶段递进：

```text
Level 1          Level 2           Level 3          Level 4            Level 5
单目标速通   ->  批量建图      ->   IOA 协作   ->   私有资源库    ->   云端抢占式集群
scan/report      scan + agent       多 worker       Cyberhub          RedC / Fleet
```

每升一级，多解决一个实际问题：

| 级别 | 解决的问题 | 核心能力 |
| --- | --- | --- |
| 单目标速通 | 快速拿到一个站点的资产、指纹、lead 和报告 | `scan --mode full --report` |
| 批量建图 | 大范围目标先铺面，再挑重点 | `scan -l`、`-F`、分片 |
| IOA 协作 | 多个 agent 共享线索和任务状态 | `ioa serve`、`agent --loop` |
| 私有资源库 | 团队经验沉淀和复用 | Cyberhub 指纹/POC |
| 云端集群 | 短时间覆盖大量目标，跑完即销毁 | 抢占式实例、RedC/Fleet、结果回收 |

---

## Level 1：单目标速通

单目标评估可以先用一条命令完成基础面：

```bash
aiscan scan -i http://target.example \
  --mode full \
  --verify=high \
  --sniper \
  --report \
  -F reports/target.assets.txt
```

这条命令做的是：

| 参数 | 作用 |
| --- | --- |
| `--mode full` | 在 quick 基础上增加 common/bak/active Web 插件探测和默认字典路径探测 |
| `--verify=high` | 对 high/critical lead 做辅助复核 |
| `--sniper` | 基于明确指纹检索公开漏洞和补丁边界 |
| `--report` | 输出 Markdown 报告 |
| `-F` | 写聚合资产报告，便于人工复盘 |

注意：`--verify`、`--sniper` 是辅助能力，不替代人工结论。报告里仍然要写基线、对照、误报排除和影响证明。

---

## Level 2：批量建图，两遍式更稳

面对一个网段、一批域名或一个 SRC 活动，不要直接全量深扫。更稳的节奏是“两遍式”。

第一遍，只建图：

```bash
aiscan scan -l targets.txt \
  --mode quick \
  --timeout 5 \
  -F reports/round1.assets.txt \
  -f reports/round1.events.txt
```

第二遍，从资产报告里挑重点：

```bash
rg -n "ERP|OA|SSO|VPN|admin|console|debug|callback|webhook|template|upload|search" reports/round1.assets.txt \
  > reports/focus-candidates.txt
```

第三步，只对 focus 目标加深：

```bash
aiscan scan -l focus.txt \
  --mode full \
  --verify=high \
  --sniper \
  --report \
  -F reports/focus.assets.txt
```

这个模式的重点是：第一轮追求覆盖和速度，第二轮追求判断，第三轮才追求验证质量。

---

## Level 3：IOA 群狼战术

IOA 的价值不是“自动分布式扫描按钮”，而是让多个 worker 在同一个 space 里共享任务、线索和证据。

### 基础拓扑

```text
                 IOA Server
              space: case-001
                    |
    --------------------------------
    |              |               |
 recon worker   verify worker   coordinator
 quick 建图      低影响复核       派单/去重/报告
```

启动 IOA：

```bash
aiscan ioa serve --ioa-url http://127.0.0.1:8765
```

启动建图 worker：

```bash
aiscan agent --loop \
  --ioa-url http://127.0.0.1:8765 \
  --space case-001 \
  --ioa-node-name recon-1 \
  -s aiscan -s scan \
  -p "只做授权范围内的 quick 建图。汇报资产、指纹、sitemap、异常状态码和高价值接口，不做破坏性验证。"
```

启动复核 worker：

```bash
aiscan agent --loop \
  --ioa-url http://127.0.0.1:8765 \
  --space case-001 \
  --ioa-node-name verify-1 \
  -s aiscan -s scan -s report \
  -p "只复核 high/critical lead。保留基线、对照、误报排除和修复建议，不读取业务数据。"
```

启动协调者：

```bash
aiscan agent --loop \
  --ioa-url http://127.0.0.1:8765 \
  --space case-001 \
  --ioa-node-name coordinator \
  --heartbeat 5 \
  -s aiscan -s ioa \
  -p "只做协调：读取 IOA 消息、分配空闲分片、合并重复发现、维护 focus 列表和报告大纲。"
```

### 五种协作模式

| 模式 | 适合场景 | 分工方式 |
| --- | --- | --- |
| 钳形协作 | 同时覆盖网络层和 Web 层 | 一个 worker 做服务发现，一个 worker 做 Web 指纹和路径 |
| 三段流水线 | 大量目标持续流入 | recon 发现目标，scanner 建图，verify 复核高危 lead |
| 指挥官模式 | worker 多、任务多 | coordinator 只派单和去重，不直接扫描 |
| 双重验证 | 报告交付前 | 两个 worker 用不同路径独立确认同一发现 |
| 盯梢模式 | 活动期间持续监控新增暴露面 | heartbeat 定期读取上下文，发现新增资产后派单 |

在公开文档里，不建议把 IOA prompt 写成攻击动作清单。更好的写法是“低影响验证、边界确认、证据归档、重复发现合并”。

---

## Level 4：Cyberhub 私有资源库

Cyberhub 解决的是团队经验复用问题。每次项目积累的新指纹、新模板、新误报规则，都应该沉淀成资源，而不是留在个人笔记里。

```text
项目发现
  -> 提炼指纹 / POC / 误报条件
  -> 提交 Cyberhub
  -> 团队 aiscan 自动拉取
  -> 下次遇到同类资产自动命中
```

典型用法：

```bash
aiscan scan -i http://target.example \
  --cyberhub-url http://127.0.0.1:9000 \
  --cyberhub-key "$CYBERHUB_KEY" \
  --mode full \
  --report
```

查询资源：

```bash
aiscan cyberhub list finger --limit 20
aiscan cyberhub search finger nginx
aiscan cyberhub list poc --severity critical,high
aiscan cyberhub search poc spring --tag rce -j
```

实践建议：

| 资源 | 应该沉淀什么 |
| --- | --- |
| 指纹 | 产品名、版本边界、favicon、标题、特征路径、响应头 |
| POC | 可验证但低影响的检查逻辑、严重度、适用版本 |
| 误报规则 | 常见 WAF/CDN/跳转页/默认页导致的误判条件 |
| 报告片段 | 修复建议、影响描述、复核 checklist |

---

## Level 5：云端抢占式集群

当目标规模很大，本地机器会成为瓶颈。旧稿里提到的“狼组 RedC 活动”指的是用 WgpSec 狼组开源的 RedC 管理云上红队基础设施，再把 aiscan worker 部署到抢占式实例上执行短期扫描任务。

这里不写真实云资源和脚本，只保留架构思路：

```text
目标列表
  -> 切分为小 chunk
  -> RedC / Terraform 创建抢占式实例
  -> 部署 aiscan worker
  -> worker 加入 IOA space
  -> 执行 chunk
  -> 回传 reports/
  -> 销毁实例
```

为什么适合抢占式实例：

| 特性 | 对应策略 |
| --- | --- |
| 成本低 | 扫描是短期批处理，跑完即可销毁 |
| 可能被回收 | chunk 粒度要小，单个 chunk 失败可重派 |
| 横向扩展快 | 多 worker 同时消费任务 |
| 结果易丢 | 每个 worker 必须本地落盘并定期回传 |

一个更安全的执行模型：

```text
coordinator
  -> 只维护 scope、chunk、worker 状态
  -> 不直接做扫描

worker
  -> 只扫描被分配的 chunk
  -> 只做低影响验证
  -> 每个 chunk 输出 events/assets/report

collector
  -> 拉取 worker 结果
  -> 合并去重
  -> 生成最终报告
```

关键原则：

- scope 必须由确定性代码控制，LLM 不负责扩大范围。
- chunk 要小于抢占式实例平均存活时间。
- worker 退出、实例回收、LLM 失败都应该能重试。
- IOA space 只保存协作上下文，最终证据必须落盘。
- 云资源用完即销毁，避免遗留成本和暴露面。

---

## 这篇和其他文档的关系

| 文档 | 定位 |
| --- | --- |
| `docs/best-practices.md` | 从 RedHaze/K3Cloud case 讲发现路径，偏文章 |
| `docs/ioa-fieldbook.md` | 从旧稿“群狼战术/狼组 RedC”抽象出的 IOA 和集群实践 |
| `docs/usage.md` | 完整命令和参数手册 |
| `docs/ioa.md` | IOA 协作模型细节 |
| `docs/configuration.md` | LLM、Cyberhub、Proxy、IOA 配置 |

一句话：`best-practices.md` 讲“一个 case 怎么被带出来”，这篇讲“多 worker、多资源、多云实例怎么组织起来”。
