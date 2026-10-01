# Web 与协作

[使用者指南](README.md) · 前一篇：[安全扫描](../scan.md)

Web 提供会话、扫描结果与节点的统一操作界面。子 Agent 将一项任务拆成多个本地推理过程，IOA 则让独立 Agent 通过消息空间协作。这几种能力可以组合使用，但各自保留独立的会话和身份边界。

## 通用 Web Hub

`cyber-web` 负责认证、节点连接、会话、事件、产物和共享配置，通过 AOP 接入不同 profile 的独立执行节点。部署时只需要下载 `cyber-web`；Hub 的快速连接面板会按系统、架构和 profile 生成一行 GitHub 下载并启动命令，不需要先手动安装 `cyber-scan` 或 `cyber-audit`：

```sh
cyber-web --token replace-me
```

访问 `http://127.0.0.1:8080` 并登录后，选择执行节点创建会话并提交任务。文件路径和工具能力来自该节点；scan、audit 和自定义 profile 可以同时连接。快速连接面板提供 scan 与 audit 下载和接入命令。

生成的安装命令只在执行节点所在机器上运行。Linux/macOS 命令使用临时目录下载并清理压缩包，Windows 命令使用 PowerShell 临时目录；节点进程退出后临时文件会被删除。已经安装过节点时，复制“仅上线”命令即可复用现有二进制。

会话、事件、扫描与配置等管理数据默认保存到 `cyber-web.db`，可用 `--db` 指定路径。安全工具的原始产物在服务端归档，浏览器将它们解析成资产视图。因此聊天回答、执行记录和资产面板分别表达不同层面的结果。

## 远程节点

节点通过 `--server-url` 连接。模型配置由 Hub 分发，包含环境变量和 CLI 的有效覆盖值；保存配置只写入编辑过的文件值。产品配置由各节点解析，Hub 保留文件中未注册的扩展：

```sh
# Web 所在主机；替换 access key
cyber-web --addr 0.0.0.0:8080 --token replace-me

# 扫描节点
cyber-scan agent --server-url http://replace-me@192.0.2.10:8080 --node-name scan-1

# 审计节点
cyber-audit --workdir /path/to/repository --server-url http://replace-me@192.0.2.10:8080 --node-name audit-1
```

节点在 Web 中使用 node ID 路由会话与终端操作。Web 连接传输请求和事件，实际工具仍在节点上执行。连接恢复不等于所有旧进程都能恢复；使用前确认节点和会话状态。

扫描服务按节点公布的命令能力选择执行位置；没有扫描节点时立即返回 `FAILED_PRECONDITION`，不会在 Hub 执行扫描。如果节点在扫描排队或执行期间断开，扫描会标记为失败并说明原因。

自定义宿主用 `node.RunWebSocket` 和自己的 `profile.Profile` 接入，无需在 Hub 注册产品名单。源码中的 `cmd/aiscan` 和 `make full` 保留旧版扫描 Web 适配器；发布矩阵使用 `cyber-web`、`cyber-scan`、`cyber-audit`。

## 子 Agent

主 Agent 可以调用 `subagent`，将独立的分析或执行工作交给子 Agent。`sync` 等待结果后返回，`async` 使用新对话在后台执行，`fork` 则继承父对话的完整边界部分后执行新任务。未指定模式时默认 async；如果选择的 Agent 类型声明不在后台运行，则使用 sync。

异步完成后，结果会回到父会话；主 Agent 可以据此继续总结或执行。`list`、`message` 和 `kill` 分别用于查询、发送追加信息和停止子任务。

子 Agent 的上下文独立，不意味着执行环境隔离。它们可能访问同一目录和同一组工具；同时修改相同文件、浏览器会话或远程对象时，任务安排需要避免冲突。类型化 Skill 的定义见[Skills 与知识](knowledge.md)。

## IOA 消息空间

IOA 通过 Space 组织多个 Agent 的消息。Agent 注册身份并加入 Space 后，可以发送任务、情报和结果；接收到的消息被交给已有会话继续处理。

先在一个终端启动服务：

```sh
cyber-scan ioa serve
```

再在另一个已配置模型的终端启动交互 Agent：

```sh
cyber-scan agent --ioa-url http://127.0.0.1:8765 --space lab
```

`--ioa-url` 增加协作连接，不改变本地任务模式。若同时传入 `-p`，仍执行一次性任务；持续协作需要持续存在的会话。`ioa send`、`ioa read` 和 `ioa space` 提供消息操作，配置与服务细节见 [IOA 专题](../ioa.md)。

`ioa space` 切换命令使用的当前空间，自动收信和 handoff 仍使用启动配置的空间。没有会话或无法确定主会话时，自动投递会被拒绝；当前没有可靠离线 outbox，发送失败不能假定会自动补送。

Web Node ID 与 IOA Node ID 属于各自系统。`--server-url` 决定执行节点连接哪个 Web，`--ioa-url` 决定协作客户端连接哪个消息服务；设置其中一个不表示另一种关系也已经建立。

## 应用集成

外部程序可以用 AOP 接入会话与工具，通过管理 API 查询配置和历史；Go 应用可以直接嵌入框架。具体集成方式属于[开发者指南](../developer/hosting.md)，使用者无需了解线协议即可通过 CLI 或 Web 工作。
