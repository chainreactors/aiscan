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

后台 Claim 生成和编译的单次 LLM 请求使用 30 分钟兜底期限，独立于前台 Provider 的短请求超时。总编译期限 `compilation_timeout` 默认 `0`，允许普通编译 Agent 持续生成、验证和修复；显式配置正值才增加整体兜底。扩展关闭或宿主取消仍立即终止请求。超时用于回收异常挂起，不用于限制正常编译耗时。

编译器沿用宿主 Agent 的临时 Provider 错误重试策略；取消、非重试错误和已耗尽的上下文仍终止。`finish_reason: length` 表示不完整输出，即使包含看似完整的工具调用也不会执行；编译器将输出预算从 16,384 逐步扩大到 65,536 后重新生成，并保留每次请求的用量。上下文不足或最高预算仍耗尽时报告明确失败，保留 Claim 和已取得的修复候选。

JEV 判定使用 32 KiB 的私有证据投影，编译回放另保留完整的宿主调用/结果轨迹。投影省略早期证据不会删除编译器的实际记录；自动归纳和普通 `jev compile` 使用相同边界。独立语义评审超出 Claim 上下文限制时返回 `review_input_limit`，要求编译器简化重复分支后重新验证，不能通过截断证据或跳过审查发布。

编译产物的 `when` 描述用户任务入口适用性，`decide` 描述函数负责的工作和完成/交接条件。使用参数的产物必须声明 schema，独立评审检查所有必需值能在入口取得；函数自己创建的资源名与已经存在的句柄有不同来源，原生检查才能发现的地址由函数内部取得。Claim 的错误选项不会被拼入 Reflex 的适用描述。

运行时参数提取直接接收原始约束文本、当前证据和 schema，省去源码与样例。截断时将输出预算从 2,048 逐步增加到 8,192，全部尝试计入前台用量，部分值不进入执行。宿主提供一次提取专用的唯一名称，仅供 schema 明确描述的新建资源使用；已有句柄和业务字段仍须来自当前证据。运行时审查直接读取原始约束文本，按 schema 解释字段的编码形式；绑定判定只接收当前调用的工具协议，宿主继续验证完整原生契约及效果身份。结构化原生结果直接保存为 JSON，避免重复转义挤出实际轨迹，原始日志和 JavaScript 读取接口保留原值。接管及验证的整体期限使用 30 分钟兜底。参数交接失败且没有保留或派发任何效果时，释放普通执行；同一输入下不反复选择这个失败函数，新用户输入或不同函数可再尝试。存在已知或未知效果时继续保护日志，避免重复写入。

持久库使用 `format: "claim/2"`。升级时，缺少版本号或使用 `claim/1` 的旧库会按原始字节一次性归档到 `library-backup-*.json`，随后通过正常持久化流程建立当前版本的空库，前台启动和关闭模式均可继续。旧库中的 Claim 语义与 Reflex 验证记录保留在归档中，后续任务重新学习并按当前机制验证。未知的较新版本、损坏文件和当前版本中的非法字段仍报错，保留原文件。供应商的 questions/answers JSON 只在 Provider 内部转换，运行与扩展统一使用 Claim / Evaluation，不提供额外 wire 包或公开 Request / Response。

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
go test ./exts/jev -run '^TestLiveClaimToReflexPipeline$' -count=1 -v -timeout 3h
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

全程基线记录 691,488 个 LLM token；自动模式记录前台 780,860、Claim 生成 59,154、编译 103,154 个 LLM token，另有 2,437,864 个 JEV token，总工作量至少 3,381,032。自动模式有八次编译请求在 75 秒超时且未返回用量，完整总量保持未知，不能计为零。这轮源码记录于 `990782c6`：后台编译流程期限为 180 秒，单次 Provider 请求期限为 75 秒。不同 Provider 的 token 合计只代表工作量，网关价格未知，未推导货币费用或回本任务数。

报销自动模式的一次失败在提交前读取了不存在的状态，被独立 oracle 记录为错误操作。Shadow DOM 基线的两次失败均以 `finish_reason: length` 结束，单次 8,192 输出 token 全部属于 reasoning，没有最终正文或工具调用。自动模式的八次编译超时使普通 Reflex 未完成发布；前台还使用了任意 `evaluate` 与复合 Shell 命令，这些操作不属于当前可验证原生契约，不能通过放宽验证将普通工具执行记成接管。当前数据不支持 JEV 已降低总 token 或费用的结论。

后续版本已移除浏览器基准的 180 秒总编译、3 分钟任务和 6 分钟后台等待期限，Provider 使用 30 分钟兜底；每次测试由测试运行器的整体期限保护。上述历史失败数据保持原样，不能作为放宽期限后的新结果。

同日单独重放此前加购场景超时的编译第二轮请求，使用实际 Provider、保留其 75 秒默认值，并由本次请求覆盖为 30 分钟兜底。网关在 203,549 ms 后返回，记录输入 19,549、输出 16,384 token；全部输出属于 reasoning，`finish_reason: length`，没有最终文本或工具调用。这验证了短超时已解除，同时确认输出预算耗尽仍阻止产物生成。该检查没有执行验证工具或浏览器，不代表完整编译、发布或热任务接管通过，结果独立保存。

运行当前版本时设置真实 `CYBER_API_KEY`、`TYPESAFE_API_KEY`、`CYBER_BASE_URL=https://api.chainreactors.cn/v1`、`CYBER_MODEL=deepseek-v4-flash`，并使用新的报告目录：

```text
JEV_TAKEOVER_LIVE=1
JEV_TAKEOVER_CASES=expense,shadow,repeat
JEV_TAKEOVER_WARM=2
JEV_TAKEOVER_REPORT_DIR=<新的外部目录>
go test -p 2 -tags 'emptytemplates full noembed' ./exts/jev -run '^TestLivePlaywrightTakeoverMatrix$' -parallel 1 -count=1 -v -timeout 3h
```

每个场景的 `report.json` 保存所有成功和失败行，`llm.jsonl`、`decisions.jsonl`、`events-*.jsonl` 与 `library.json` 保存原始证据，`source.json` 保存本轮源码身份。此次外部数据另提供逐任务 `results.csv`、分类汇总 `summary.json` 和 SHA-256 清单；凭证及运行产物不进入源码库。

## 编译及热接管修复验证（2026-10-07）

修复轮次发现了短超时以外的实际阻塞：reasoning 耗尽输出预算而不产出正文；未声明运行时参数 schema；错误选项进入入口描述；把原生检查才能发现的地址作为必需参数；重用旧会话名；转义字符串被误解；重复的语义分支撑爆审查输入；判定上下文预算挤出编译所需的早期真实调用。上述问题分别进入输出重试、schema/入口评审、参数提取、唯一分配名称、原文审查、代码修复诊断和完整编译轨迹机制。完整原生契约、回放、效果日志和业务 oracle 的要求保持有效。

此轮 Off/Auto 同时使用单次原生命令与 `snapshot --json`，禁止不支持分类的 `evaluate` 和复合 Shell；含引号的业务字段同时明确为 JSON 字符串、只解码一次。随机参数、实际字符、操作次数和私有 oracle 没有改变。这与前面的自由工具使用轮次范围不同，不能直接比较轮次之间的数字。

`matrix-current-7` 从两个空库分别为客户保存和重复加购生成并发布了普通 Reflex。此时客户保存的两个热任务业务正确，但参数审查误判后回退，完整接管为 0/2；重复加购在冷任务发布后结束该旧代码轮次。后续 `shadow-reload-11` 通过生产加载路径读取实际冷任务生成的库，冻结学习，未修改其源码、验证记录或参数 schema，并在最新运行时代码下执行三个新任务（一个额外复用预检及两个正式热任务）。所有任务改变 URL、字段和实际控件地址。

| 客户保存正式热任务 | 业务正确 | 完整接管 | 主模型工具调用 | 前台 LLM token | JEV token | 前台耗时 |
| --- | --- | --- | --- | --- | --- | --- |
| Off 合计 | 2/2 | — | 20 | 105,639 | 0 | 87,547 ms |
| Auto 合计 | 2/2 | 2/2 | 0 | 32,222 | 130,856 | 76,568 ms |

自动任务各执行 6 个真实原生操作，库源码不变，Claim 生成及编译 token 均为零。前台 LLM token 减少约 69.5%，合计耗时减少约 12.5%；计入 JEV 后总工作量由 105,639 增至 163,078 token，增加约 54.4%，仍未验证净 token 节省。第二个热任务实际触发了参数输出 2,048 → 4,096 → 8,192 的重试后成功，全部用量已计入。额外预检也完整接管成功，但不混入上述两个正式热任务的汇总。

重复加购的 `repeat-reload-12` 同样通过三个新任务，正式热任务完整接管 2/2，各执行 8 个真实原生操作；主模型工具调用、Claim 生成及编译均为零，源码不变。两个正式热任务 Off/Auto 前台 LLM token 为 91,858 / 19,320（减少约 79.0%），JEV 为 0 / 177,467，总工作量为 91,858 / 196,787。耗时合计为 50,264 / 34,217 ms（减少约 31.9%）。两个重载场景合计六次完整接管，四次正式热任务全部通过独立业务 oracle。

这是实际生成产物的跨进程复用验证，不是最新运行时从空库完成同轮冷/热任务的完整矩阵。此前加购和客户保存各有单次完整接管，但旧轮次仍有失败或回退，全部保留；多个旧源码探索轮次在最终重载验证通过后主动结束，标记为 interrupted，未完成请求的用量和结果保持未知。报销的新编译先耗尽 16,384 输出 token，随后反复遭遇上游 HTTP 500/503、auth_unavailable；该轮保留为中断、无已验证产物，不能把其他场景或未来任务宣称为全部稳定通过。

重载复现设置 `JEV_TAKEOVER_RELOAD_DIR=<真实空库冷任务报告目录>`，其他设置沿用上面的命令，选择对应场景并使用新的报告目录。该模式记录原库 SHA-256、来源路径和本轮源码哈希，普通加载/契约检查仍会拒绝无已验证 Reflex 的库；index 0 是额外复用预检，index 1/2 为正式热任务。
