# JEV：Claim 与 Reflex

每一次 JEV 语义判定都表达为 Claim。Claim 只描述判定内容；是否复用、何时调用、如何取证、如何编译和发布，由使用它的组件负责。

```json
{"type":"choice","context":"根据当前记录选择下一步。inspect 表示读取新证据，report 表示报告已确认结果，defer 表示需要补充推理。","options":["inspect","report","defer"]}
```

## 判定内容与结果

Claim 只有 `type`、自然语言 `context` 和有序字符串数组 `options`。事实、选项含义和判定标准直接写入 context；不再增加 Question、criteria 或业务请求对象。

| type | options | Evaluation 的值 |
| --- | --- | --- |
| choice | 2–64 个不同的候选结论 | 数组中的一个字符串 |
| score | 2–10 个从低到高排列的等级 | 加权等级索引，范围为 0 到 options.length−1，可以是小数 |
| noul | 不提供选项 | context 成立的概率，范围为 0 到 1 |

Context 最多 64 KiB，每个选项最多 1 KiB；空内容、重复选项、未知类型和旧字段会被拒绝。评分选项的顺序定义量表；所有 Claim 的内容身份都保留数组顺序。

[Provider](../agent/provider/jev/claim.go)提供封闭的三种类型，[决策协议](../proto/decision/claim.proto)用 oneof 表达结果。阈值和后续动作由调用者决定。批量 Evaluate 的 token 用量属于此次执行；厂商的 state/questions/criteria 请求仅存在于内部传输适配层。

## 职责与生命周期

临时 Claim 可以直接 Evaluate，无需进入库。适用性检查、运行时授权、完成检查以及 guardrail 都使用这一入口。当前证据附加在本次调用的 context 中，记录于决策事件，不改变持久化 Claim。

需要复用的 Claim 可以发布到库。其 ID 由 type、context 和 options 的内容计算；重复发布复用同一个 ID。库记录只额外保存来源任务，快照复制选项数组，发布通过原子持久化完成。生成流程和原生命令共用同一条发布路径。

```mermaid
flowchart LR
    C[Claim 内容] --> E[临时求值]
    C --> P[发布为可复用 Claim]
    P --> G[当前证据支持编译]
    G --> V[生成、回放、语义评审]
    V --> R[发布普通 Reflex]
    R --> G
```

证据一旦就绪，就在当前边界判断是否编译，包括第一个任务；来源任务不构成等待条件。Claim 定义判断的语义范围，Reflex 持有可执行函数、参数 schema、原生契约、效果次数及验证记录。一个 Reflex 可以覆盖多个 Claim，Claim 不保存“已消费”标记。

编译失败保留 Claim，证据或契约不足时可以保留待验证候选。修复只有通过验证并成功持久化后才替换原 Reflex；失败保留原源码。Reflex 退役也不会删除它覆盖的 Claim，后续证据可以再次触发编译。`learning=frozen` 禁止学习写入，仍允许临时判断和已验证 Reflex 的复用。

持久库使用 `format: "claim/2"`。缺少该版本、旧 Claim 字段或旧库版本直接拒绝加载，不迁移、不重写旧文件。

## 普通 Reflex 的自举

空库从实际交互归纳 Claim，并按同一证据门槛生成普通 Reflex。系统没有专用 bootstrap Reflex、特权 ID、额外源码类型或验证豁免。

安装 JEV 扩展后，现有原生命令提供：

```text
jev status
jev claim <Claim JSON>
jev compile <claim-id> [replace-reflex-id]
```

`status` 读取当前库；`claim` 发布内容并返回 `claim_id`；`compile` 以宿主记录的当前交互请求编译，返回目前覆盖该 Claim 的 `reflex_ids`。返回空数组表示尚无已发布覆盖，编译可能因证据不足或冷却而推迟。替换目标必须已经覆盖所选 Claim。

普通模型和任意普通 Reflex 都可以通过原生 Executor 调用这些操作。`jev-library` 契约将 status 分类为读取，将 claim/compile 分类为库写入；Reflex 中的写入仍需声明效果步骤和次数，通过运行时判定、效果日志与原生验证。命令不能接收调用者编造的轨迹或直接发布源码。编译器只回放已有结果，不执行用户工具。

每次原生调用完成后，宿主立即保存真实结果，供同一执行中的下一次编译使用。在途调用没有结果，不能成为回放证据。原函数替换成功后，已开始的执行继续使用其源码快照，下一次选择使用新库。

## JavaScript 判定桥

`jev` 直接接收 Claim 并返回对应原始值：

```javascript
const next = jev({
  type: "choice",
  context: "根据当前证据选择 inspect、report 或 defer。" + JSON.stringify(currentFacts),
  options: ["inspect", "report", "defer"]
});
const severity = jev({type: "score", context: "评估当前风险。", options: ["低", "中", "高"]});
const complete = jev({type: "noul", context: "当前真实结果已经完成用户请求。" + JSON.stringify(currentResult)});
```

返回值分别是字符串、等级索引和概率。确定性的解析、转换和控制流直接用 JavaScript；只在语义分叉处调用 JEV。旧的 `jev({state,questions}).answers` 形式不再接受。

[生命周期测试](../exts/jev/claim_lifecycle_test.go)覆盖首个证据边界、普通 Reflex 自我替换、失败回滚和退役后的 Claim 保留；[类型测试](../agent/provider/jev/claim_test.go)覆盖序列化、旧字段拒绝、结果类型和数值边界。

## 完整机制验证

[连续链路测试](../exts/jev/claim_pipeline_test.go)从空库启动普通 Agent，经原生 Executor 取得随机回执，归纳 typed Claim，向普通编译 Agent 提交错误参数并接收回放诊断，修复后通过原生契约、回放和独立语义评审发布 Reflex。随后重新加载持久库，以不同目标和相同目标运行三个新任务，检查当前参数、新回执、一次读取、无重新规划或编译，以及冻结库不变。choice、score、noul 都运行这条链路。测试也检查带引号参数的 Bash 解码，以及语义评审收到已解析的真实报告值。

```text
go test ./agent/provider/jev ./exts/jev ./exts/guardrail ./pkg/web/service ./cmd/aiscan -count=1 -timeout 120s
go test -tags 'emptytemplates forceposix full netgo noembed osusergo sqlite' ./agent/provider/jev ./exts/jev ./exts/guardrail ./pkg/web/service ./cmd/aiscan -count=1 -timeout 120s
go test -race ./exts/jev -run 'TestClaim|TestOrdinaryReflex|TestReflexV2(Compiler|Frozen|.*Effect|.*Unknown|.*Steer)' -count=1 -timeout 120s
go test -tags 'full sqlite' ./cmd/aiscan -run '^TestJEVProfileStreamingCompilationRuntimeAndWebReplay$' -count=1 -timeout 120s
```

普通测试只替换模型推理，仍使用真实宿主和验证器。`TestLiveClaimToReflexPipeline` 提供两个明确区分的真实接口模式：设置 `JEV_PIPELINE_LIVE=1` 和 `TYPESAFE_API_KEY`，所有 JEV 判定调用真实接口，LLM 生成仍由固定测试响应提供；额外设置 `JEV_PIPELINE_LLM_LIVE=1`、`CYBER_API_KEY`、`CYBER_BASE_URL`、`CYBER_MODEL`、`CYBER_PROVIDER`，则 Claim 生成、编译、参数提取和最终整理也调用真实 LLM。

```text
go test ./exts/jev -run '^TestLiveClaimToReflexPipeline$' -count=1 -v -timeout 240s
```

可设置 `JEV_PIPELINE_REPORT_DIR` 保存 `pipeline-report.json`、`library.json`、决策日志、原生执行证据和完整 JEV 事件。每次使用新的报告目录，以保证真正从空库启动；日志不记录凭证。真实接口测试会检查完整验收条件，defer、未发布源码、模型调用失败或新任务重新规划都会导致失败，不能作为成功的端到端验证。

## Token 与成本的判定

Reflex 将已验证流程的重复规划变为普通代码执行和有限语义判断，因而可以减少重复任务的 LLM token。节省取决于原流程需要多少推理和历史上下文；一次读取再报告的短任务仍需要参数提取和答案组织，可能没有净节省。

比较时分别记录前台 LLM、Claim 生成、Reflex 编译和 JEV 的输入与输出 token。参数提取计入前台 LLM，不能在前台汇总后再次相加；reasoning 已属于输出 token，不能重复计数。未知用量和失败重试不能当作零成本。不同模型的 token 单位、单价和缓存计费可能不同，所有 Provider token 的合计仅是工作量指标，费用必须按实际单价计算。

设基线每任务用量为 B，复用每任务用量为 W，学习及编译的额外用量为 C；重复 N 个任务后的净节省为 `N × (B − W) − C`。只有 B 大于 W 才能摊薄首次成本；价格不同则将公式中的用量换成实际费用。现有 [A/B 基准](../exts/jev/benchmark_live_test.go)分别报告前台 LLM、全部 LLM、全部 Provider、首次成本、复用费用和回本任务数；[记账测试](../exts/jev/token_accounting_test.go)验证分类汇总、负节省和缺失用量。

2026-10-06 的真实 JEV、固定 LLM 响应链路从空库发布 Reflex，并在三个新任务中各进行一次参数提取和一次答案组织，没有重新规划、生成 Claim 或编译；真实 JEV 判定共消耗 14,439 个复用 token。该运行的 LLM 响应及用量为测试固定值，不能据此报告真实 LLM token 节省率。最初的直连 DeepSeek 测试在首次调用返回 HTTP 402（Insufficient Balance）。

同日改用 `https://api.chainreactors.cn/v1` 验证真实模型。裸名称 `deepseek-v4.1-flash` 返回 HTTP 400（model_not_found）；模型目录中的完整 ID 是 `opencode-go/deepseek-v4.1-flash`。该 ID 通过文本、JSON、原生工具调用、工具结果续接及流式输出验证。

真实 JEV 与该模型共同通过完整链路：从空库生成三个 Claim、编译并发布一个 Reflex，重载后完成三个新任务，验证新目标、带引号和反斜杠的参数、当前随机回执及冻结库不变。冷启动调用两次普通推理、一次 Claim 生成、两次编译推理；三个复用任务共调用三次参数提取和三次答案组织，普通规划、Claim 生成和编译均为零。参数提取显式使用 `reasoning_effort: none`，避免默认推理耗尽 2,048-token 输出预算；测试任务明确目标为 JSON 字符串，要求只解码一次。

这次短任务复用共消耗 5,767 个前台 LLM token 和 15,393 个 JEV token，合计 21,160，未包含冷启动的学习和编译成本。完整机制通过不等于净节省。多步骤 A/B 的测试工具也必须提供原生读取、写入及效果回执契约；工具描述不能替代可信契约，缺少契约时保留候选，不能作为已接管的复用样本。

补齐原生契约后的两组多步骤 A/B（含启动任务，共六次执行）已完成，但未通过接管验收：三次前台请求在 90 秒后超时，另有 Claim 请求超时，其用量保持未知。自动模式直到最后一个任务后才发布 Reflex，没有执行任何复用操作。最后一组正确完成的任务双方均调用六次前台模型；基线消耗 6,155 token，自动模式消耗 6,186 个前台、33,357 个编译和 20,344 个 JEV token，合计 59,887。该任务包含延后的首次编译，不能视为稳定复用成本；当前实测尚不支持总 token 或费用降低的结论，也不能将失败样本造成的调用次数减少称为节省。

## 真实浏览器 A/B 基准（2026-10-06）

[浏览器基准](../exts/jev/playwright_takeover_live_test.go)在三个隔离的本地业务应用上运行真实 Chromium、LLM 和 JEV：报销表单在提交成功后丢失响应并经历状态查询故障；CRM 在开放 Shadow DOM 内填写客户引用并保存；购物流程要求同一个按钮加购两次后结算一次。服务端独立检查随机参数、效果次数、错误操作和随机回执，不向模型提供控制接口凭证或预期回执。这是实际浏览器交互的业务夹具测试，尚未覆盖生产 SaaS 网站。

每个场景分别从空库运行 `off` 与 `auto`，各包含一次冷启动和两次热任务，交替执行顺序，共 18 次。新任务改变 URL、字段值和控件地址，没有预置 Reflex、固定模型答案或验证豁免。自动热任务的接管验收要求真实 Reflex 效果执行及报告、业务正确、主模型工具调用为零、源码保持不变。测试记录源码哈希及前台、后台和服务端证据。

本次模型为网关 `deepseek-v4-flash`，JEV 为 `jev-1.13.0`。此前 `opencode-go/deepseek-v4.1-flash` 的同一矩阵因上游代理连接失败全部返回 HTTP 500，没有执行浏览器任务，保留为独立失败轮次。`deepseek-v4-flash` 通过了文本、JSON、工具调用、工具结果续接和流式协议检查。项目的 `playwright` 命令实际由 Go Rod 驱动；官方 Python Playwright 与项目原生命令分别完成三个夹具预检，合计 6/6 通过，正式 Agent 基准使用项目命令。

| 场景 | 基线业务正确 | 自动业务正确 | 自动热任务完整接管 | 两次热任务前台 LLM token：基线 / 自动 | 全程已记录 Provider token：基线 / 自动 |
| --- | --- | --- | --- | --- | --- |
| 报销及提交后恢复 | 3/3 | 2/3 | 0/2 | 184,740 / 193,520 | 248,941 / ≥1,302,856 |
| 两次加购及结算 | 3/3 | 3/3 | 0/2 | 150,741 / 166,007 | 220,890 / ≥813,851 |
| Shadow DOM 客户保存 | 1/3 | 3/3 | 0/2 | 131,457 / 140,810 | 221,657 / ≥1,264,325 |

自动模式最终发布 Reflex 为零，六次热任务接管为零，实时验收测试失败。基线业务正确 7/9，自动模式 8/9；双方成功率不同，不能把失败造成的低消耗解释为节省。即便两边全部正确的加购场景，自动模式热任务也没有减少前台 token。每种模式只有两次热任务，这些数据用于定位当前机制，不能推断整体成功率或长期性能。

全程基线记录 691,488 个 LLM token；自动模式记录前台 780,860、Claim 生成 59,154、编译 103,154 个 LLM token，另有 2,437,864 个 JEV token，总工作量至少 3,381,032。自动模式有八次编译请求在 75 秒超时且未返回用量，完整总量保持未知，不能计为零。后台编译流程期限为 180 秒，单次 Provider 请求期限为 75 秒。不同 Provider 的 token 合计只代表工作量，网关价格未知，未推导货币费用或回本任务数。

报销自动模式的一次失败在提交前读取了不存在的状态，被独立 oracle 记录为错误操作。Shadow DOM 基线的两次失败均以 `finish_reason: length` 结束，单次 8,192 输出 token 全部属于 reasoning，没有最终正文或工具调用。自动模式的八次编译超时使普通 Reflex 未完成发布；前台还使用了任意 `evaluate` 与复合 Shell 命令，这些操作不属于当前可验证原生契约，不能通过放宽验证将普通工具执行记成接管。当前数据不支持 JEV 已降低总 token 或费用的结论。

复现时设置真实 `CYBER_API_KEY`、`TYPESAFE_API_KEY`、`CYBER_BASE_URL=https://api.chainreactors.cn/v1`、`CYBER_MODEL=deepseek-v4-flash`，并使用新的报告目录：

```text
JEV_TAKEOVER_LIVE=1
JEV_TAKEOVER_CASES=expense,shadow,repeat
JEV_TAKEOVER_WARM=2
JEV_TAKEOVER_REPORT_DIR=<新的外部目录>
go test -p 2 -tags 'emptytemplates full noembed' ./exts/jev -run '^TestLivePlaywrightTakeoverMatrix$' -parallel 1 -count=1 -v -timeout 45m
```

每个场景的 `report.json` 保存所有成功和失败行，`llm.jsonl`、`decisions.jsonl`、`events-*.jsonl` 与 `library.json` 保存原始证据，`source.json` 保存本轮源码身份。此次外部数据另提供逐任务 `results.csv`、分类汇总 `summary.json` 和 SHA-256 清单；凭证及运行产物不进入源码库。
