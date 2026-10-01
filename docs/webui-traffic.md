# WebUI 工具时间线与可观测数据

WebUI 顶栏的可观测入口统一展示当前会话的工具、流量、文件、录制、CSTX、命令、进程及其他 extension 事件，支持分类、搜索、实时追加和历史恢复。切换会话后只展示该会话的事件；子会话数据沿已有根会话事件归档展示。

面板分为“会话活动”和“资产库”。默认活动视图合并同一次调用或操作的开始/完成阶段，省略成功工具生命周期与工具结果的重复信息；失败和不同的嵌套操作仍然保留。“原始事件”展示完整记录，事件标识与关联信息在详情的元信息中展开。手机端使用列表与详情切换，支持返回及清除筛选。

资产库复用已有 CSTX 归档与浏览器存储，包含历史会话和导入资产，保留执行范围、对比、类型筛选、搜索、导入、导出及发送到对话。它是跨会话资产集合，单条 CSTX 调用卡片仍只解析该次原始 Artifact。工具注册表位于 Agent 管理的工具页，与终端共享 Agent 选择，直接读取所选 Agent 的 `hello.tools` 原生工具声明和 Bash 命令目录，复用 viewer 的工具定义卡片展示输入参数、用法及别名。

时间线直接按原生 `aop.operation.Ref` 的显式 `call_id` 关联观察记录，并隔离 session、emitter 和 turn。观察记录作为对应工具调用中的专用卡片：HTTP 请求/响应、文件操作、录制截图/视频、CSTX 资产及操作状态。不猜测最近一次调用；无法关联的已知观察事件保留独立时间线项。先收到观察事件、后收到调用时，原有独立项会归入该调用。操作开始和完成阶段在工具卡片中合并展示，顶部保留原始事件记录。

数据直接复用 traffic 机制：`ProxyHub` 捕获完成 → `FlowCompletedObserved` hook → observe 发布原生 `aop.traffic.Flow` extension → 已有 AOP 会话事件存储、`ListEvents` 和 `WatchEvents` → `@cyber/traffic` 渲染。没有新增流量 API、传输 DTO 或第二条广播链路。

带 session runtime 的 aiscan profile 默认观察 `tools,commands,processes,files,http`。显式 `--observe` 仍按指定种类工作。Record 和 CSTX 继续复用现有原生工具结果和 Artifact/Loot 事件。捕获继续遵循已有 traffic 配置及过滤规则，纯 relay 模式不产生捕获结果。

统一事件投影、调用关联及可观测面板由 `@cyber/viewer` 提供，具体卡片复用 `@cyber/traffic`、`@cyber/file-manager`、`@cyber/cstx-easm` 和 viewer 的录制组件；WebUI 仅负责抽屉入口、翻译和媒体地址。直接保留原生 Event 与 payload，没有新的传输 DTO。CSTX 资产按原始 Artifact 独立、按需解析，避免混入其他调用或会话的资产。

流量保留原生 header 列表及 body bytes，显示时保留重复 header、解码 UTF-8，二进制正文展示有限长度的十六进制预览，大文本预览限制为 128 KiB。捕获错误和已收到的响应可同时显示。

验证：`go test ./pkg/exts/observe ./tools/proxy ./cmd/aiscan` 包含真实 HTTP 经代理捕获、工具/文件/命令/进程事件及调用关联校验；viewer 和 traffic 单元测试覆盖去重、关联顺序、会话隔离、历史替换和正文转换。`npm run test:e2e:record` 包含 WebUI 的二进制 WebSocket 更新、刷新恢复、会话切换、分类/搜索、时间线卡片、CSTX 解析和窄屏展示，以及 record 和通用组件回归。
