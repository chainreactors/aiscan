# cyber-audit 功能审计与修复验收（2026-09-27）

本次完成七轮功能修复，每轮单独提交。Windows amd64 上的真实 DeepSeek 复测证明：模型能自行读取文件、选用原子工具、保存报告、恢复上下文，并根据校验反馈补修产物。规则与外部扫描器提供辅助证据，流程由模型驱动。该结论只涉及功能完整性，不评价业务漏洞发现能力、召回率或误报率。

## 修复提交

| 轮次 | Commit | 已复现并修复的问题 |
| --- | --- | --- |
| 1 | `b541fb7a` | 单次任务恢复时打开空会话；缺少统一报告预检及补修闭环；突发事件记录溢出；导入目录误用父仓库 revision；帮助与模型配置说明不一致。统一推荐共享 CYBER 变量，保留旧别名并脱敏。 |
| 2 | `a6de858b` | 空白 coverage 字段、重复冲突检查记录及空白确认说明被接受；外部链接被当作报告内证据；校验失败混合磁盘错误时误标为 incomplete。 |
| 3 | `f55cc0cc` | 一次性输出绕过调用方 writer；JSON 写入错误被忽略；补修轮次的 token 用量漏计；补修失败后保留旧成功答案。 |
| 4 | `156fdaf4` | 全局参数放在 doctor 前后含义不同；失效模型配置阻塞工具维护；doctor 的 JSON 开关失效；冲突参数和非法超时先触发工具准备。 |
| 5 | `977d38f1` | 配置、目录、任务参数、工具准备、报告目录和参数解析失败时，机器输出为空。现在输出结构化启动错误，并保留原始错误及输出错误。 |
| 6 | `a0be54d3` | 小范围 glob 仍遍历无关或更深子目录，导致大依赖目录耗尽遍历额度。现在只遍历可能匹配的目录，保留取消、额度及 path.Match 匹配语义。 |
| 7 | `518c5858` | 空 log.md 导致 OKF panic；删除 log.md 却通过报告校验。现在返回明确错误，能在原会话反馈并补修。 |

共享模型配置使用 `CYBER_API_KEY`、`CYBER_BASE_URL`、`CYBER_MODEL`、`CYBER_PROVIDER`，无需为不同二进制分别设置密钥。命令行设置优先于 CYBER 环境变量，再优先于配置文件与兼容变量。工具维护无需有效模型配置，数据目录仍遵循命令行、CYBER_DATA_DIR、配置文件的优先级。

## 真实模型复测

端点为 `https://api.chainreactors.cn/v1`，模型为 `deepseek-v4.1-flash`。使用用户授权凭据，计量转发器转发真实响应，子进程仅收到本地占位 token。没有伪造模型响应或向模型提供修复后的报告。密钥未写入源码、配置或报告；扫描 120 个相关文本产物未发现该密钥。

样本为此前准备的 OWASP/NodeGoat 固定快照（`c5cb68a7084e4ae7dcc60e6a98768720a81841e8`）。本次只检查文件访问、rg/AST、OSV/proton 调用、报告写入、恢复和补修。未运行样本服务、安装样本依赖或分析业务漏洞；37 个源文件的哈希均未变化，三份 findings.json 均为空。

| 运行 | 源码版本 | API 请求 | 耗时 / 秒 | 工具调用 / 返回 | 报告状态 | 退出码 |
| --- | --- | ---: | ---: | ---: | --- | ---: |
| functional | `977d38f1` | 14 | 110.204 | 17 / 17 | completed | 0 |
| resume-repair | `977d38f1`，内嵌工具包 | 18 | 120.047 | 17 / 17 | completed | 0 |
| functional-final | `a0be54d3`，内嵌工具包 | 16 | 117.547 | 26 / 26 | completed | 0 |

三次 JSON 结果均为 `is_error=false`，合计 48 次 API 请求、835,348 input tokens、25,855 output tokens（含缓存及三次启动探针）。每次启动探针消耗 31 input / 16 output tokens，不属于正式会话；正式会话 JSON 的输入、输出和总 token 数均逐项匹配转发器计量，包含补修用量。探针的 length 结束不代表任务截断。

恢复测试首个正式请求携带 38 条消息及旧任务。转发器缓冲真实 SSE 后集中交付；在首次最终响应交付前，将一个 coverage.checks 状态从 completed 改成 reused。后续 4 次真实请求包含补修反馈，最终模型修复该字段并通过校验。记录器保存 1,059 个事件，17 个工具调用均有返回。

两次完整功能运行各出现一次不支持的 `glob **` 调用，模型收到提示后自行改用支持的路径查询或 rg。递归 `**` 仍不是 glob 的接口能力，不能将本次第六轮修复解释为新增递归通配符。没有将工具错误抹去或计为成功调用。

第七轮修改之后未重复付费模型调用；已通过空/缺失日志的完整会话补修回归，并使用最终构建重新校验以上三份真实报告及单文件流程。

## 自动验证与构建

所有验证使用 Go 1.26.1、`GOWORK=off`。工作区存在其他并行修改，因此使用 Git index 导出的独立目录 `.tmp/audit-rounds/index-check` 编译测试，避免混入尚未提交的接口重构。提交仅包含本任务修改，其他工作区变更保留。

已通过：

- audit 模块完整 `go test ./...` 与 `go vet ./...`。
- `pkg/console`、`pkg/config`、`cmd/agent`、`cmd/aiscan`、`cmd/harness` 完整包测试。
- `agent/session`、`core/eventbus`、`core/events`、`exts/telemetry`、`pkg/harness` 完整包测试。
- `tools/files` 与 `tools/okf` 完整包测试和 vet。glob 回归构造了 20,001 个无关文件，修复前的五种小范围查询均失败，修复后均通过；直接查询超限目录仍返回额度错误。
- Windows 外部 junction 证据拒绝测试。内部相对 symlink 的断言受本机创建符号链接权限限制，未在此平台执行。
- 最终内嵌工具 exe 的 `TestSingleFileRelease`：只复制单个可执行文件，空 PATH/工具目录、外网代理阻断，验证安装、幂等、删除恢复、工具执行及报告完成，外部请求为 0。
- 最终构建对三份真实报告执行 `cyber-audit validate`，均退出 0；未改写原始报告。
- 每轮暂存区 `git diff --cached --check`。

最终可执行文件为 `.tmp/audit-rounds/cyber-audit-verified.exe`，SHA-256：

```text
a245e67a5f02d844d3db8fad82b739c59da70dff2309602733f3dd9032239753
```

最终构建对应 `518c5858` 的已提交源码和生成的 Windows 内嵌源代码审计工具包（rg 15.2.0、ast-grep 0.45.3、osv-scanner 2.6.0）。其单文件测试耗时约 6.3 秒。生成的工具资源与可执行文件留在本地，不进入 Git。

本机证据保存在 `.tmp/audit-rounds/verification.json`、`credential-check.json` 和 `runs/{functional,resume-repair,functional-final}/`。每次运行保留 invocation、API 用量元数据、stdout、run.json、session.jsonl 和完整报告。早先四次真实复测证据仍在 `.tmp/audit-fixes/`，本表只计本次新增的三次运行。

本次没有验收 Linux/macOS、UPX 发布资产、逆向工具扩展、长仓库语义覆盖、并发子代理或业务漏洞准确率。没有推送或发布变更。
