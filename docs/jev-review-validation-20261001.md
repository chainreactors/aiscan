# JEV 通用 Observe 复审与真实验收（2026-10-01）

本文件保留首次复审时的实现快照及失败结果。之后的代码修复和新验收记录见[自动接管验收](jev-autonomous-takeover-20261001.md)，当前实现契约见[实现说明](jev-implementation-20260927.md)。下文失败记录没有被后续运行覆盖。

结论：生产执行机制已移除具体工具适配，Observe 可以保留为运行时生成的纯数据表达式；目前还不能满足“LLM 执行几次后，自动发现封闭场景，稳定交给 JEV loop”的完整需求。真实编译器会生成语法错误、记住首次目标或错误解释进度，JEV 复审也存在误放行。接口通用性与模型自动生成能力必须分别验收。

## 实现复审

当前链路是 `普通交互 → 后台 Claim/Compile → 动态 Observe → JEV 判别 → 原生 Executor → 实际结果 → Observe`。场景只有 When、Decide、Observe 三个公开字段，Observe 输出状态和有限原生调用绑定；读取新事实也通过普通候选执行。

- `core/tool.Command` 和 CommandRegistry 不要求 Observe/Choices 回调。curl、Playwright 没有 JEV 专属候选或执行分支；JEV 生产代码没有具体工具包导入或工具名分支。
- 宿主只需现有 agent hooks、Provider 和原生 Executor。普通 CommandExecutor 可选提供文档/status；本次原生验收没有命令注册表，三个工具名在运行时随机生成，没有预装场景。
- 原生参数保持 JSON 含义；不再把名为 command 的字段解释为 shell。错误统一交回模型，移除 CommandCompleted 订阅和工具错误恢复逻辑。控制项仅 report/defer。
- JEV 的实际调用及结果按 call_id 关联，跨 LLM 补参交接保留在当前任务的私有轨迹。主历史只追加回执；不伪造模型 toolcall、不重写已提交前缀。缓存限 32 KiB，超限停止当前任务加速，RunEnd 清理。
- 生成草稿先编译和试运行，失败可携诊断纠正一次，再由 JEV 复审场景是否记住目标及进度是否真实。每项工作有界，但复审仍是模型判别，不能证明所有分支正确。

“没有工具耦合”指不需要为某个工具修改或编译适配代码。它仍依赖宿主的普通 hooks、Provider、Executor 和工具文档/真实结果；这些是执行所需的通用协议。

Observe 复用已有 Expr，纯数据计算、没有工具/文件/网络执行能力。单后台 worker、队列及场景库均有界，不能据此宣称运行开销轻量：本次浏览器生成了 38 份编译草稿，最终仍无有效接管。

## 真实测试条件

主 LLM 为网关 `https://api.chainreactors.cn/v1` 的 `deepseek-v4.1-flash`；JEV 为 `jev-1.13.0`。使用用户提供的两组凭据，未将凭据写入代码或报告。真实测试没有替换模型响应、预装规则、工具内置 Observe 或强制激活操作。

默认后台推理在先前原生冒烟中多次用尽 8192 output token，仅返回 reasoning 或不完整响应，后台结算超时。随后显式配置 `declaration_effort: none`，仅作用于 Claim/Compile。它解决了本次网关的长推理响应问题，未解决自动编译可靠性。供应商的推理开关见 [DeepSeek 官方说明](https://api-docs.deepseek.com/guides/thinking_mode/)；生产默认仍为空，不暗中修改主模型设置。

最新完整两轮报告如下，先前失败文件也保留在 `.runlogs`，没有用后续运行覆盖：

- [原生工具 A/B 冒烟 JSON](../.runlogs/jev-review-20261001-1790792689780-native-reviewed-smoke.json)、[完整日志](../.runlogs/jev-review-20261001-1790792689780-native-reviewed-smoke.log)、[原生自动模式审计](../.runlogs/jev-review-20261001-1790792689780-native-reviewed-smoke-evidence/auto/decisions.jsonl)。原生目录还有 `llm.jsonl`，包含透明计量的实际请求及解析响应，不含认证头。
- [五页 Chromium 验收 JSON](../.runlogs/jev-review-20261001-1790792689780-browser-reviewed.json)、[完整日志](../.runlogs/jev-review-20261001-1790792689780-browser-reviewed.log)、[浏览器自动模式审计](../.runlogs/browser-20260930-193709/decisions.jsonl)。

## 原生工具：任务完成，自动闭环未成立

流程跨 catalog/activate/receipt 三个动态命名的工具：读取当前随机 ID 和版本、仅激活请求资源一次、轮询同一实际 job 直到 complete、报告真实 receipt。三个任务依次使用 Archive、Invoices、Cancel，新任务重新产生 ID/版本/job。off/auto 分别从空库开始，顺序轮换；后台结算独立计量。

| 模式与任务 | 业务正确 | 主 LLM 调用 | JEV 动作 | 前台耗时 |
| --- | --- | ---: | ---: | ---: |
| off 首次 Archive | 否，参数被主模型误抄 | 4 | 0 | 17.940 s |
| auto 首次 Archive | 是 | 6 | 0 | 14.814 s |
| off Invoices | 是 | 6 | 0 | 16.047 s |
| auto Invoices | 是 | 6 | 0 | 22.567 s |
| off Cancel | 是 | 6 | 0 | 17.783 s |
| auto Cancel | 是 | 6 | 0 | 28.283 s |

auto 的 3 个任务均由普通 LLM 完成。两个复用任务都没有满足“至少 4 个 JEV 动作、只需 1 次主 LLM 汇报”的闭环标准，报告 `functional_accepted=false`。off 首次任务本身失败，也完整保留，不能归因于 JEV。

最终生成的场景包含 `#.label == "Archive"`，When/Decide 还是 schema 示例占位文本。它还依据 activation 的 pending 状态继续 poll，没有正确使用 receipt 的终态。实际 JEV 复审给出 compile，证明复审没有可靠拦截任务目标固化。换成 Invoices/Cancel 后 Observe 输出 no_target，普通模型以真实工具结果纠正了这份矛盾观察；没有实现跨目标接管。

auto 全部 LLM token 为 38,382，JEV token 为 107,384，JEV 请求 38 次；off LLM token 为 16,779。后续两个正确配对任务中，主 LLM 调用没有减少，全部供应商 token 为 off 的约 7.05 倍。这里只是功能冒烟，不构成性能统计或生产收益证明。

## 浏览器：五种未见页面，复用未通过

真实 Chromium 页面采用随机 selector、不同控件结构与目标标签，其中包含无 ID 控件。验收要求正确结果、服务端效果恰好一次，复用任务还必须观察到 JEV 的普通 open/click 执行。

| 页面目标 | 服务端完成次数 | 错误选择次数 | 主 LLM 调用 | JEV open/click | 完整验收 |
| --- | ---: | ---: | ---: | --- | --- |
| Archive | 1 | 0 | 5 | 无 | 首次业务任务通过，但未形成 Reflex |
| Invoices | 1 | 0 | 8 | 无 | 未通过接管 |
| Cancel | 2 | 0 | 7 | 无 | 未通过，效果重复 |
| Continue | 2 | 0 | 5 | 无 | 未通过，效果重复 |
| Inventory | 1 | 0 | 4 | 无 | 未通过接管 |

后台生成 38 份编译草稿，记录 34 条 compile_invalid，JEV 复审 10 次，9 次拒绝、1 次放行。最终只有一个不完整场景，库在后续页面发生变化；所有复用页面均未通过完整验收。记录的 LLM token 为 311,245，JEV token 为 342,846，JEV 请求 77 次。

重复效果发生在未观察到 JEV open/click 的任务，不能据此断言由 JEV 重放了动作；验收仍按真实服务端次数拒绝通过。完整调用记录及输出见报告。

## 通过项与尚未解决的问题

普通回归、确定性编译/执行集成、Chromium 集成和 race 检查通过。它们验证纯表达式运行、动态工具绑定、循环、原生证据交接、错误回退、跨任务隔离、持久化和只追加主历史。固定编译响应的成功不能替代真实模型自动生成的成功。本次真实运行中，计量器发现的主请求前缀变更均为 0；上游 absent/null reasoning_content 在 off/auto 都出现，单独记录为协议问题，没有当作 JEV 改写历史。

已执行并通过：

```powershell
go test -buildvcs=false ./agent/... ./core/... ./exts/jev ./exts/guardrail ./exts ./pkg/harness ./tools/curl ./tools/toolargs -count=1 -timeout=6m
go test -buildvcs=false -tags 'full sqlite' ./tools/playwright ./exts/browser ./exts/jev ./cmd/aiscan -count=1 -timeout=6m
go test -buildvcs=false -race ./agent/provider/jev ./exts/jev ./core/tool -count=1 -timeout=6m
```

完整自动接管仍有以下阻碍：

1. 真实编译器没有稳定遵守 Expr 和场景协议；诊断纠正一次仍经常失败。
2. JEV 场景复审会误放行写死目标及错误进度逻辑；语法校验、一次试运行和模型判断不足以保证可复用。
3. 后台在多次普通输出边界重复尝试生成，实际请求与 token 开销较高；没有接管时这些都是额外开销。
4. Observe 输入及 Expr 的 fromJSON 使用通用 JSON 数字解码；大整数尚未做端到端精度保证。canonical 的 UseNumber 只修复去重规范化，不代表 Observe 的数值绑定已无损。本次随机 ID 是字符串，未覆盖此边界。

因此保留 `mode: off` 默认。本次没有代理的实际价格输入，费用明确为未知；没有将功能失败解释成节省，也没有扩大为 20 对性能验收。继续改进应针对通用编译协议、换目标验证和真实终态一致性，不能把这些失败转为 curl、Playwright 或其他工具内的适配规则。
