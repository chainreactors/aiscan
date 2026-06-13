# IOA：Intelligent Operation Architecture

IOA 是 aiscan 的多 agent 协作架构。它通过一个轻量 HTTP server 和消息空间（Space），让多个 aiscan 实例以自治 agent 的身份协同工作——每个 agent 独立决策，通过消息交换情报、分配任务、汇报结果。

---

## 目录

- [核心概念](#核心概念)
- [架构总览](#架构总览)
- [数据模型](#数据模型)
- [Loop Worker 生命周期](#loop-worker-生命周期)
- [消息处理](#消息处理)
- [Heartbeat 机制](#heartbeat-机制)
- [Peer 消息](#peer-消息)
- [IOA 工具](#ioa-工具)
- [多 Worker 协作模式](#多-worker-协作模式)
- [使用指南](#使用指南)
- [配置参考](#配置参考)

---

## 核心概念

| 概念 | 说明 |
| --- | --- |
| **Space** | 协作空间。一次渗透测试、一个目标网段可以是一个 Space。所有参与的 Node 在同一个 Space 中交换消息 |
| **Node** | 自治工作节点。每个 `aiscan agent --loop` 实例注册为一个 Node，拥有独立的 LLM agent 和工具集 |
| **Message** | Space 中的消息。可以是任务分派、情报共享、结果汇报或协调指令 |
| **Ref** | 消息引用。通过 `refs.nodes` 定向发送给特定节点，通过 `refs.messages` 建立会话线程 |
| **Task** | 约定为可执行任务的消息。当前由 agent 通过 `ioa_read`/heartbeat 主动读取和判断，不由 IOA Server 自动推送执行 |

---

## 架构总览

```
┌──────────────────────────────────────────────────────────┐
│                      IOA Server                          │
│              HTTP API + in-memory store                  │
│  ┌─────────────────────────────────────────────────────┐ │
│  │                   Space: case-1                     │ │
│  │                                                     │ │
│  │  msg1: [scanner-1] joined, skills: scan,gogo       │ │
│  │  msg2: [recon-1] joined, skills: spray,neutron     │ │
│  │  msg3: → scanner-1: "扫描 10.0.0.0/24 端口"        │ │
│  │  msg4: [scanner-1] accepted task                    │ │
│  │  msg5: → recon-1: "对 Web 目标做指纹识别"           │ │
│  │  msg6: [scanner-1] result: 发现 12 个服务...        │ │
│  │  msg7: [recon-1] result: 识别到 nginx, tomcat...    │ │
│  └─────────────────────────────────────────────────────┘ │
└──────────┬───────────────────┬───────────────────────────┘
           │ ioa_* tools       │ ioa_* tools
     ┌─────▼──────┐      ┌────▼───────┐
     │  scanner-1 │      │  recon-1   │
     │  (Node)    │      │  (Node)    │
     │            │      │            │
     │  LLM Agent │      │  LLM Agent │
     │  gogo      │      │  spray     │
     │  zombie    │      │  neutron   │
     └────────────┘      └────────────┘
```

IOA Server 本身不做决策——它只是消息总线。所有智能都在 Node 端的 LLM agent 中。

---

## 数据模型

### Space

Space 是消息的容器，类似聊天室。一个 Space 对应一次协作任务。

```bash
# 创建/获取 Space
aiscan ioa spaces --ioa-url http://127.0.0.1:8765
```

### Node

Node 是注册到 IOA Server 的工作节点。注册时携带元数据：

```json
{
  "client": "aiscan",
  "hostname": "scanner-host",
  "capabilities": ["scan", "gogo", "spray"]
}
```

Node 的执行策略由本地 agent 决定。常见做法是通过 `ioa_read` 查看定向消息或全局消息，认领自己要处理的任务，再用 `ioa_send` 回报结果。

### Message

Message 是 Space 中的通信单元，结构化为 `SwarmMessage`：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `content` | string | 消息文本（自然语言任务描述或结果） |
| `targets` | []string | 相关目标（IP、URL 等） |
| `task` | bool | 是否为任务分派 |

### Ref（引用）

Ref 是消息路由和线程化的核心：

| 引用类型 | 说明 |
| --- | --- |
| `refs.nodes` | 定向发送。只有指定的 Node 会处理此消息 |
| `refs.messages` | 线程引用。建立消息之间的上下文关联 |
| `refs.spaces` | 跨 Space 引用 |

---

## Loop Worker 生命周期

`aiscan agent --loop` 启动一个持久运行的 agent，并接入 IOA 工具：

### 1. 启动

```
注册 Node → 加入 Space → 注册 ioa_* 工具 → 执行初始 prompt/等待 heartbeat
```

Node 启动时会向 IOA Server 注册身份，并在配置了 `--space` 时默认加入该 Space。加入后 `ioa_send`/`ioa_read` 会默认操作这个 Space。

### 2. 消息读取

Loop Worker 不再内置 SSE/Poll 自动路由器。当前模型是让 LLM agent 主动使用 IOA 工具读取消息：

- `ioa_read`：读取定向给当前 Node 的消息
- `ioa_read all --limit N`：读取 Space 最近消息，用于全局判断
- `ioa_read thread --id <message_id>`：读取某条消息的上下文线程

### 3. 任务执行

收到任务消息后，agent 应主动判断是否处理、是否认领以及如何回报：

```
ioa_read → 判断任务归属 → ioa_send claim/ack → 执行工具 → ioa_send result/loot
```

- Agent 执行期间拥有完整的 LLM 工具链（扫描器、文件操作、IOA 工具等）
- 执行结果可以通过 `ioa_send reply --to <message_id>` 引用原始 Task 消息，形成线程
- 多个任务的排队、去重和抢占由 agent prompt/协调协议控制

### 4. 关闭

- Ctrl+C 会取消当前 agent 运行
- 如果没有初始任务、heartbeat 或其他 inbox producer，agent 会在完成当前轮后退出

---

## 消息处理

当前 aiscan 不再把 IOA 消息自动推入 agent inbox。推荐处理规则如下：

```
读取 IOA 消息
  │
  ├─ 是自己发的？ → 跳过
  │
  ├─ refs.nodes 指定了其他节点？ → 跳过或仅作为上下文参考
  │
  ├─ 已处理过的历史消息？ → 跳过
  │
  ├─ task=true 或 refs.nodes 包含自己？
  │   ├─ 发送 claim/ack
  │   └─ 执行并 reply/result
  │
  └─ 普通消息
      → 作为情报、进度或协调上下文
```

**定向消息**通过 `refs.nodes` 表示目标 Node。**广播消息**（无 `refs.nodes`）需要各 Node 根据 claim、scope 和上下文自行避免重复工作。

---

## Heartbeat 机制

Heartbeat 让 Worker 在没有任务时也能主动审视协作上下文并采取行动。

```bash
aiscan agent --loop --heartbeat 5 --space case-1 \
  -p "持续观察上下文，协调各节点扫描进度"
```

### 工作方式

每隔 N 分钟：

1. Runtime 读取当前 SpaceInfo（节点列表、消息计数等）和 Space 中最近 50 条消息
2. 构造结构化 prompt，包含：Space 信息、当前 Node、初始任务/targets、节点状态、近期消息上下文
3. 将 prompt 注入 agent inbox，由 Agent 判断下一步行动
4. Agent 可以：执行本地工具、发送 IOA 消息协调其他 Node、或决定无需行动

### 适用场景

- **协调者角色**：一个 Node 专门负责审视全局进度，分配任务
- **持续监控**：定期检查是否有新情报需要处理
- **自愈**：发现某个 Node 长时间无响应时主动接管

---

## Peer 消息

Peer 消息不会逐条自动推入当前 Agent 上下文。启用 heartbeat 时，runtime 会把最近 IOA 消息批量带入 heartbeat prompt；未启用 heartbeat 的 worker 仍需要在初始 prompt 中主动调用 `ioa_read` 拉取其他 Node 的情报、结果和 blocker。

---

## IOA 工具

当 Agent 连接 IOA 时，以下工具自动注册到 Agent 的工具集中：

| 工具 | 说明 |
| --- | --- |
| `ioa_send` | 向 Space 发送消息（任务分派、情报共享、结果汇报） |
| `ioa_read` | 读取 Space 中的消息（支持过滤） |
| `ioa_space` | 获取或创建 Space |

### ioa_send 示例

Agent 可以通过 `ioa_send` 给其他 Node 分配任务：

```json
{
  "space_id": "space-001",
  "content": "对 10.0.0.5:8080 的 Tomcat 执行 critical 级别 POC 检测",
  "targets": ["10.0.0.5:8080"],
  "refs": {
    "nodes": ["scanner-node-id"]
  }
}
```

---

## 多 Worker 协作模式

### 模式一：手动分工

人工为每个 Node 设定明确的职责分工：

```bash
# 终端 1：IOA Server
aiscan ioa serve

# 终端 2：端口扫描 Worker
aiscan agent --loop --space pentest-001 \
  --ioa-node-name port-scanner \
  -s gogo -p "负责端口和服务发现"

# 终端 3：Web 探测 Worker
aiscan agent --loop --space pentest-001 \
  --ioa-node-name web-recon \
  -s spray -s neutron -p "负责 Web 指纹识别和漏洞检测"

# 终端 4：弱口令 Worker
aiscan agent --loop --space pentest-001 \
  --ioa-node-name cred-tester \
  -s zombie -p "负责弱口令检测"
```

然后通过 IOA 消息发起任务：

```bash
# 通过交互式 agent 发送任务
aiscan agent --ioa-url http://127.0.0.1:8765
> 在 pentest-001 中给 port-scanner 分配任务：扫描 10.0.0.0/24 全端口
```

### 模式二：协调者 + 执行者

一个 Heartbeat Worker 作为协调者，自动分配任务给其他 Worker：

```bash
# 协调者（每 5 分钟审视一次）
aiscan agent --loop --space pentest-001 \
  --heartbeat 5 \
  --ioa-node-name coordinator \
  -p "你是协调者。审视当前进度，给空闲的 scanner 和 recon 节点分配下一步任务。目标网段：10.0.0.0/24"

# 执行者们
aiscan agent --loop --space pentest-001 --ioa-node-name scanner -s scan
aiscan agent --loop --space pentest-001 --ioa-node-name recon -s spray -s neutron
```

### 模式三：One-shot Agent 接入 IOA

非 loop 模式的 Agent 也可以接入 IOA，用于临时参与协作或查看状态：

```bash
# 临时参与，执行完退出
aiscan agent --ioa-url http://127.0.0.1:8765 \
  -p "在 pentest-001 中查看当前进度，补充对 10.0.0.5 的深度扫描" \
  -i 10.0.0.5
```

---

## 使用指南

### 启动 IOA Server

```bash
# 默认 http://127.0.0.1:8765，使用内存 store
aiscan ioa serve

# 自定义监听地址
aiscan ioa serve --ioa-url http://0.0.0.0:8765
```

当前 aiscan CLI 不暴露 IOA 数据库路径配置；`ioa serve` 默认使用内存 store，进程重启后消息和节点状态不会保留。

### 启动 Loop Worker

```bash
aiscan agent --loop [OPTIONS]
```

| 参数 | 说明 | 默认值 |
| --- | --- | --- |
| `--ioa-url` | IOA Server 地址 | `http://127.0.0.1:8765` |
| `--space` | 加入的 Space 名 | `default` |
| `--ioa-node-name` | 节点名称 | 自动生成 |
| `--ioa-node-id` | 使用已有节点 ID | — |
| `--heartbeat` | Heartbeat 间隔（分钟），0 禁用 | `0` |
| `-p` | 节点意图描述 | — |
| `-s` | 加载的 skill | — |
| `--timeout` | 单次任务超时 | `3600` |

### 查询 IOA 状态

```bash
# 列出 Space
aiscan ioa spaces --ioa-url http://127.0.0.1:8765

# 列出 Space 中的消息
aiscan ioa messages pentest-001 --ioa-url http://127.0.0.1:8765

# 查看消息上下文（线程）
aiscan ioa context pentest-001 <message-id> --ioa-url http://127.0.0.1:8765

# 列出所有节点
aiscan ioa nodes --ioa-url http://127.0.0.1:8765

# 列出 Space 内的节点
aiscan ioa nodes pentest-001 --ioa-url http://127.0.0.1:8765

# JSON 输出
aiscan ioa spaces --ioa-url http://127.0.0.1:8765 --json
```

### 交互式 REPL 中的 IOA 命令

在 `aiscan agent` 交互模式下：

```
aiscan> /spaces
aiscan> /messages pentest-001
aiscan> /context pentest-001 msg-123
aiscan> /nodes pentest-001
```

---

## 配置参考

### CLI 参数

| 参数 | 说明 |
| --- | --- |
| `--ioa-url` | IOA Server URL |
| `--ioa-node-id` | 已有节点 ID |
| `--ioa-node-name` | 注册节点名 |
| `--space` | Space 名称 |
| `--json` | IOA 查询 JSON 输出 |
| `--loop` | 启用 Loop Worker 模式 |
| `--heartbeat` | Heartbeat 间隔（分钟） |

### 配置文件

```yaml
ioa:
  url: "http://127.0.0.1:8765"
  node_name: "my-scanner"
  space: "default"
```

### 环境变量

IOA 相关参数暂无独立环境变量，通过配置文件或 CLI 参数指定。
