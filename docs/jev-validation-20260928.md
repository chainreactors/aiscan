# JEV 实测验证

> 当前运行时生成 Observe 的复审与真实验收见 [2026-10-01 报告](jev-review-validation-20261001.md)，自动接管尚未通过。以下记录包含旧版工具适配、固定生成夹具和历史付费结果，不能作为当前无工具适配版本的验收。

## 编译入口修复后：非浏览器 20 对实现短路，总成本仍未通过

2026-09-29 修复了未编译 Claim 的后续匹配不进入编译评估、编译尝试提前标记完成、没有接管也向主模型系统提示加入控制器说明的问题。声明及编译判别改为结合当前交互和注册观察能力，沿用现有 Claim、Reflex、Observe 和原生执行器。说明仅随实际回执追加；旧库的编译完成标志从已发布 Reflex 重建。

### 为什么此前 LLM token 反而更多

前一轮失败对照的后续 20 个任务，主 LLM 调用仍是 90 次，没有被 JEV 替代。启用扩展后的 LLM 总量可以完整拆成：**121,046 基线 + 8,533 主流程差额 + 6,966 后台 Claim 生成 = 136,545**。

主流程差额由输入增加 10,590、输出减少 2,057 构成。旧实现通过 BeforeRun 无条件附加控制器说明，因此即使没有 Reflex 也增加每轮输入；轨迹和生成长度也有随机差异，不能将全部差额精确归因于这段说明。后台那次 Claim 生成另有 5,342 reasoning token。由于没有发布 Reflex，这些开销与 JEV 请求全部叠加，没有换掉操作中的 LLM 调用。

### 修复后的真实完整对照

先从空库完成 4 对冒烟测试，再从另一个空库重新跑 20 对及双方首次任务，共 42 条正式记录。真实主模型仍为 deepseek-v4.1-flash，JEV 为 jev-1.13.0；HTTP 夹具、任务、交替顺序和验收门槛未改。正式运行 497.31 秒，运行前后 173 个相关源文件哈希一致。最终从模型输出自动生成 **1 个 Claim、1 个 Reflex**，没有预装场景或编译提示。

| 正式后续 20 对指标 | off | auto |
| --- | ---: | ---: |
| 正确任务 | 19/20 | 20/20 |
| Reflex 接管任务 | 0 | **20/20** |
| 主 LLM 调用 | 90 | **20（−77.8%）** |
| 全部 LLM input+output | 121,378 | **40,866（−66.3%）** |
| 全部 LLM output | 13,575 | 7,648（−43.7%） |
| LLM reasoning（output 子集） | 2,148 | 2,413（+12.3%） |
| JEV input+output | 0 | 561,251 |
| LLM+JEV 总 token | 121,378 | **602,117（4.96 倍）** |
| 前台中位耗时 | 12.510 s | **5.854 s（−53.2%）** |
| 前台 p95 耗时 | 13.903 s | 7.273 s |
| 参考费用 | $0.013176378 | $0.030562512（2.32 倍） |

复用期 70 次原生 curl 读取全部由 JEV 驱动，LLM 每任务只调用一次作最终报告。没有新增 Claim/Compile、错误动作、重复读取、主请求前缀改写或协议问题。此次确认了操作短路与 LLM 总 token 下降，但没有达到该 HTTP 场景的 80% token 目标，也没有证明 reasoning 或全供应商成本下降。

### 首次准备成本和保留的失败

- 首次 auto 主流程为 13.257 秒，计入全部后台工作为 120.957 秒；off 为 13.545 秒。第一次声明生成消耗 8,192 个 reasoning/output token 而未产生完整声明，按截断响应拒绝。下一次普通交互的声明成功，再编译出 Reflex。两次 Claim 和一次 Compile 合计 17,725 个 LLM token，其中 reasoning 为 14,394；没有删去这次失败成本。
- 首次任务 LLM 总量为 off 6,684、auto 24,396。计入首次及全部后续任务，LLM token 为 **128,062 → 65,262（−49.0%）**，而全供应商 token 为 **128,062 → 643,546**。报告生成仍有 reasoning，JEV 每步传入当前上下文、策略和候选的输入成本也仍高。
- off 第 15 个任务正确访问全部节点并给出正确阴性结论，但报告漏掉终点的证据标识，因此未通过证据完整性校验。所有样本均保留。117 次 JEV 请求没有请求错误或缺失用量；上述一次失败是 LLM 声明截断，不是 JEV EOF。
- 正式测试退出码为 1，accepted=false：基线有一例报告失败，且参考总费用上升，没有满足原成本门槛。单价沿用基准参考输入，未核对网关账单。不能把主 LLM 节省表述成全链路节省。

新增回归覆盖已有 Claim 在拒绝后重新评估、生成失败/null 不锁死编译、旧库尝试标志恢复、未接管时主模型提示不变。实际成本证据见[正式 42 条记录](../.runlogs/jev-nonbrowser-fixed-20260929.json)、[汇总及后台归因](../.runlogs/jev-nonbrowser-fixed-20260929-summary.json)、[测试日志](../.runlogs/jev-nonbrowser-fixed-20260929.log)、[源文件哈希](../.runlogs/jev-nonbrowser-fixed-source-20260929.json)、[完整性验证](../.runlogs/jev-nonbrowser-fixed-verified-20260929.json)。前置 4 对冒烟结果单独保留在[冒烟记录](../.runlogs/jev-nonbrowser-fix-smoke-20260929.json)，未混入正式配对。

### 浏览器复核：复用接管成功，冷启动仍失败

编译修复后，又使用真实 JEV、deepseek-v4.1-flash 和 Chromium 从空库复核通用动态页面。第一次运行在首次页面失败后提前退出：普通 LLM 调用 7 次，完成资源被读取两次；当时尚未发生 Reflex 接管，随后后台发布了 Reflex。这次失败记录保留在[首次运行](../.runlogs/jev-browser-after-compile-fix-20260929.json)及[日志](../.runlogs/jev-browser-after-compile-fix-20260929.log)。

为避免一个独立页面失败遮蔽后续页面，将该集成测试的三处失败检查由 Fatal 改为 Error，仍然保留失败判定，再从另一个空库运行全部 5 页。生产实现、任务目标和正确性要求没有修改。完整运行 74.80 秒，最终库为 4 个 Claim、1 个 Reflex：

| 页面 | 正确 | 主 LLM 调用 | JEV 接管浏览器入口及页面操作 |
| --- | --- | ---: | --- |
| 首次页面 0 | 否：完成资源被读取 3 次 | 6 | 否 |
| 后续页面 1 | 是 | 2 | 是 |
| 后续页面 2 | 是 | 1 | 是 |
| 后续页面 3 | 是 | 1 | 是 |
| 后续页面 4 | 是 | 1 | 是 |

后续 4 页全部复用同一未修改的 Reflex，页面 ID、控件类型及顺序变化，末页没有元素 ID；主请求前缀改写为 0。首次页面失败仍发生于普通 LLM 操作阶段，不能归因于 JEV 回放固定动作；也不能把后续接管成功表述为冷启动已解决。此次完整测试退出码仍为 1，没有通过全项验收，也不是新的 off/auto 性能对照。证据见[全部页面记录](../.runlogs/jev-browser-after-compile-fix-allpages-20260929.json)和[保留失败的日志](../.runlogs/jev-browser-after-compile-fix-allpages-20260929.log)。

### 修复后的回归结果

`go test -tags full ./exts/jev ./tools/curl ./core/tool -count=1`、Agent BeforeModel/AfterModel 定向回归，以及 JEV 扩展、core/tool、agent/provider/jev 的 race 测试均通过；本轮改动范围内的 `git diff --check` 通过。新增用例覆盖编译再次评估、失败后的可重试状态、持久化恢复和未接管时提示不变。普通测试通过不覆盖概率判别的全部行为，不改变上述真实 HTTP A/B 和浏览器测试的失败状态。

## 修复前：非浏览器依赖闭环 20 对，自动编译未通过

2026-09-29 新增 `aiscan-http-loop`，使用真实 deepseek-v4.1-flash 和 JEV，从空 Claim/Reflex 库运行每种模式 1 次首次任务和 20 次后续任务，共 **42 条记录**。本场景只注册原生 curl，不注册 Playwright。任务没有 JEV、Claim 或编译提示，后台声明成本全部计入。生产提示和 JEV 机制在本轮未修改；运行前后 **172 个相关源文件哈希一致**。完整运行 597.01 秒，测试退出码为 1，`accepted=false`。

任务采用当前响应决定下一请求的依赖链。运行时生成 URL，用户提供许可集合，下一跳只有读取当前响应后才可确定；两种完整阳性分支、提前阴性终止和末尾阴性结果交错变化，各 5 对。服务端验证实际访问顺序和终点，提前请求、走错分支、重复读取、缺少证据标识或错误结论均不能通过。off/auto 顺序交替，所有样本保留。这是本机 HTTP 取证闭环测试，不等同于整个 aiscan 扫描引擎的生产验收。

| 后续 20 对指标 | off | auto |
| --- | ---: | ---: |
| 正确任务 | 19/20 | 20/20 |
| 发生 Reflex 接管的任务 | 0 | **0/20** |
| 主 LLM 调用 | 90 | **90，无减少** |
| 全部 LLM input+output | 121,046 | 136,545（+12.8%） |
| 全部 LLM output | 13,631 | 17,247（+26.5%） |
| 全部 LLM reasoning（output 子集） | 2,174 | 6,507（+199.3%） |
| JEV input+output | 0 | 287,491 |
| LLM+JEV 总 token | 121,046 | **424,036（3.50 倍）** |
| 前台中位耗时 | 13.220 s | 12.926 s（−2.2%） |
| 前台 p95 耗时 | 16.682 s | 15.315 s |
| 参考费用 | $0.013490466 | $0.027243132（2.02 倍） |

主 LLM 调用和原生工具调用均没有减少，Reflex 动作为零，不能把这轮前台耗时的微小差异解释为 JEV 接管带来的收益。全链路 token、费用和自动接管目标均未通过。参考单价沿用已有基准输入，未核对网关实际账单。计入首次任务后，LLM token 为 127,941 → 143,714，全链路为 127,941 → 443,609。

### 已确认的阻塞位置

1. 首次 auto 任务正确完成，但没有生成声明。后续第 2 个任务触发一次真实 LLM Claim 生成，得到 4 个声明：响应内容读取、依赖读取与批量读取、继续跟随资源、判定终止。该次生成耗时 44.283 秒，消耗 6,966 token，其中 reasoning 为 5,342；该成本保留在对应后续任务中。
2. 4 次 JEV 分组均认为这些声明属于同一场景，但 **compile 判别全部为 defer**。没有发起 LLM Compile 调用，最终库中为 **4 Claim / 0 Reflex**。审计包含 116 次 discover、1 次 claim、4 次 group、0 次 compile、0 次运行期 decision。
3. `declare.go` 当前只针对新加入的 Claim 进入 `compile`。后续输出匹配已有 Claim 时，`fresh` 为空就直接返回；本轮库因此始终停留在声明阶段。这确认自动形成 Reflex 的路径被阻塞，不能据此断言已有 Reflex 的通用执行器无法执行 HTTP 操作。
4. 共 120 次 JEV 请求，**0 请求错误、0 缺失用量**；没有 EOF。主模型请求前缀变化和协议问题均为 0。问题发生在语义判别及编译触发路径。声明/编译提示大量使用浏览器例子，这可能影响泛化，需要独立修订验证；审计没有返回拒绝原因，不能把这种影响当作已证明的唯一原因。

off 第 19 个任务完成了全部正确访问并给出正确阴性结论，但将 `evidence-812cd0fcbbb6669f` 抄成 `evidence-812cd0fcbbb69f`，缺少两个字符，按证据完整性要求判为失败；没有删除或重跑该样本。auto 对应任务通过，但该轮也由普通 LLM 执行，不能据此宣称 Reflex 提高了正确性。

**结论：通用 Observe/Reflex 接口已存在，但“任意非浏览器场景自动生成并接管闭环”尚未被验证成立，本场景明确未通过。** 后续修订应继续沿用 Claim → Compile → Reflex，明确能力级判别、编译充分性和已有未编译声明的评估条件。本轮只新增验收场景及记录，没有引入学习流程或修改生产控制机制。

### 复现与验证

已有 `CYBER_API_KEY`、`CYBER_MODEL`、`CYBER_BASE_URL`、`TYPESAFE_API_KEY` 和参考 `JEV_BENCH_PRICES` 配置时，设置 `JEV_BENCH_LIVE=1`、`JEV_BENCH_PAIRS=20` 及独立的 `JEV_BENCH_REPORT`，运行：

```text
go test -tags full ./exts/jev -run '^TestLiveAutomaticReflexAB/aiscan-http-loop$' -count=1 -v -timeout 25m
```

夹具正反例测试通过；运行后 `go test -tags full ./exts/jev ./tools/curl ./core/tool -count=1`、Agent 的 BeforeModel/AfterModel 定向回归及范围内 `git diff --check` 均通过。普通回归通过不覆盖真实模型语义判别，真实 A/B 失败状态仍保留。

证据：[42 条原始记录](../.runlogs/jev-nonbrowser-loop-20260929.json)、[完整汇总及后台归因](../.runlogs/jev-nonbrowser-loop-20260929-summary.json)、[运行日志](../.runlogs/jev-nonbrowser-loop-20260929.log)、[172 个源文件哈希](../.runlogs/jev-nonbrowser-loop-source-20260929.json)、[完整性校验及报告错误](../.runlogs/jev-nonbrowser-loop-verified-20260929.json)。测试实现：[HTTP 依赖闭环夹具](../exts/jev/benchmark_http_loop_test.go)。

## 泛化浏览器完整对照：五类任务，20 对

**主 LLM 短路和速度收益成立，全链路 token 降本仍未成立。** 本轮在上一轮生产实现不变的情况下扩展基准，使用真实 deepseek-v4.1-flash、JEV 和 Chromium，从空库自动生成 4 个 Claim、1 个 Reflex，完成每种模式 1 次冷启动及 20 个复用任务，共 42 条记录。100 个相关源文件运行前后哈希一致，复用阶段没有新增 Claim/Compile。

任务按五类交错运行，各 4 对：导航/反向目标、原生下拉筛选、表单填写与参数缺口、异步弹窗、同源 iframe 回退。页面元素 ID 在运行时生成，控件顺序及 button/link/ARIA 类型变化。用户任务没有编译提示，也没有给 JEV 预装页面路径。off/auto 顺序按任务交替；不同模式保持相同任务目标、页面逻辑及工具配置。

### 全部 20 对复用任务

以下包含失败和慢样本，不删除不利结果。正确性来自服务端动作/参数校验和实际返回的确认码，不接受只在回答里声称完成。p50/p95 沿用基准的 nearest-rank 定义。

| 指标 | off | auto | 变化 |
| --- | ---: | ---: | ---: |
| 正确任务 | 19/20 | 20/20 | off 有 1 次错误操作 |
| 主 LLM 调用 | 181 | 40 | −77.9% |
| LLM input+output | 899,725 | 166,703 | −81.5% |
| LLM output | 39,399 | 9,154 | −76.8% |
| LLM reasoning（output 子集） | 20,698 | 5,293 | −74.4% |
| JEV input+output | 0 | 782,710 | 额外开销 |
| LLM+JEV input+output | 899,725 | 949,413 | **+5.5%** |
| 前台 p50 | 32.811 s | 8.381 s | −74.5%，约 3.91 倍速度 |
| 前台 p95 | 59.149 s | 30.430 s | −48.6% |
| 参考费用 | $0.038852 | $0.042804 | +10.2% |

13/20 对同时满足双方正确、变快和 LLM input+output 减少至少 80%；计入 JEV 后，0/20 对达到总 token 减少 80%。复用阶段实际执行 69 个 Reflex 动作、149 次 JEV 请求。包含冷启动共 160 次 JEV 请求，没有请求错误或用量缺失；主历史前缀改写为 0。前缀稳定不等于供应商缓存命中保证。

### 分场景结果

下表仍包括全部样本，每类仅 4 对，只描述本地受控页面的实测，不代表任意互联网网站的分布。

| 场景 | LLM token 变化 | LLM+JEV token 变化 | p50 off → auto | 正确 off / auto |
| --- | ---: | ---: | ---: | --- |
| 导航、取消/返回等目标 | −88.2% | −11.1% | 26.135 → 7.362 s | 4/4、4/4 |
| 下拉筛选与结果选择 | −92.3% | −33.3% | 33.490 → 7.018 s | 4/4、4/4 |
| 表单填写与参数缺口 | −90.7% | −30.3% | 32.811 → 8.661 s | 4/4、4/4 |
| 异步弹窗与连续判断 | −82.8% | **+32.2%** | 26.643 → 7.934 s | 4/4、4/4 |
| iframe 回退 | −56.5% | **+74.4%** | 39.267 → 21.898 s | 3/4、4/4 |

原生 DOM 候选覆盖的导航、筛选大多只剩一次末尾报告；两个缺少字面量绑定的表单分别增加一次 LLM 补参。异步弹窗虽然更快，连续观察和交接仍消耗较多 JEV 输入。iframe 尚未进入原生 Observe 的完整候选覆盖，4 个任务分别需要 3、4、7、6 次主 LLM 调用，并付出 9、10、15、14 次 JEV 请求；现有普通模型回退没有消除旁路判别的成本。

### 失败配对与完整成本

第 19 对 iframe 任务：off 35.746 s，但出现 1 次错误操作，最终服务端完成状态不能抹去该错误；auto 正确，但耗时 42.312 s。这对保留在主表，不能拿失败任务的耗时当作等价成功执行。额外列出**双方均成功的 19 对**：LLM token 减少 84.4%，LLM+JEV token 仅减少 4.0%，p50 仍为 32.811 → 8.381 s。原始数据没有删除该失败配对。

原有验收要求两组全部正确，因此本轮 **accepted=false，测试返回失败**；不能据此宣称完整性能验收通过。主 LLM、reasoning 和速度的数值门槛达到，不改变基线失败及全链路 80% 目标未达到的事实。

首次任务前台 off/auto 为 23.367/19.643 s；auto 包含后台声明和编译共 65.919 s，必须等策略生成后才能测复用阶段。首次 LLM token 为 26,580/31,834，auto 另有 43,208 JEV token。**把冷启动与 20 次复用全部计入**：LLM token 减少 78.6%，LLM+JEV token 增加 10.6%。参考费用沿用 ab5 假设并按缓存用量计算，未核对网关实际账单。

当前瓶颈不是再次给每一页写规则：异步连续判断会重复传入 JEV 上下文，未覆盖的 iframe 则同时承担 JEV 判断和 LLM 补充操作。后续应针对通用观察覆盖和 JEV 私有输入的重复开销验证改进，继续保持原生执行边界、主历史 append-only 和可插拔扩展。

证据：[42 条记录及生成的库](../.runlogs/jev-general-ab-20260929.json)、[完整汇总与 19 对成功配对](../.runlogs/jev-general-ab-20260929-summary.json)、[包含失败的完整日志](../.runlogs/jev-general-ab-20260929.log)、[源文件哈希](../.runlogs/jev-general-ab-source-20260929.json)、[结束校验](../.runlogs/jev-general-ab-verified-20260929.json)。本轮仅扩展测试与分析，未重跑完整外部 aiscan 引擎，也未验证跨域 iframe、Shadow DOM 或任意网站。

## 通用页面回归：动态候选而非固定元素

针对固定页面选项的检查，已去掉运行提示中的具体 Continue 标签，并明确 Claim/Compile 不能把主按钮、向导阶段、回执或固定收尾动作作为通用浏览器流程。生产候选来自实时 DOM；测试页里的元素 ID 和文案不是生产规则。

随后以真实 JEV、deepseek-v4.1-flash 和 Chromium 从空库运行 5 个任务，自动生成 4 个 Claim、1 个 Reflex。每次运行重新生成元素 ID，控件类型及位置变化，第 5 页没有元素 ID；Cancel 与 Continue 交替作为正确目标和干扰项。复用阶段严格比较 Reflex 内容保持不变，并检查随机 ID / 本机 URL 未进入持久化策略。

| 任务目标 / 控件 | 正确 | 主 LLM 调用 | 前台耗时 | JEV 接管打开与页面操作 |
| --- | --- | ---: | ---: | --- |
| Archive / button，首次任务 | 是 | 5 | 15.374 s | 否，首次发现尚未完成 |
| Invoices / link | 是 | 1 | 5.150 s | 是 |
| Cancel / ARIA button | 是 | 1 | 6.759 s | 是 |
| Continue / button | 是 | 1 | 5.827 s | 是 |
| Inventory / 无 ID link | 是 | 1 | 3.258 s | 是 |

5/5 正确，错误动作 0，主历史前缀改写 0，后 4 个任务均复用同一 Reflex，只调用主 LLM 生成最终报告。首次任务包含后台声明/编译共 58.146 s，完整测试 80.61 s、28 次 JEV 请求。首次成本保留，不能只报后续时间。

通过 `go test -tags full ./exts/jev ./tools/playwright -count=1`，以及同一跨页面测试的真实供应商模式。证据：[原始记录及自动生成的库](../.runlogs/jev-generic-browser-20260929.json)、[真实运行日志](../.runlogs/jev-generic-browser-20260929.log)。

该回归验证未见页面上的动态选择、策略复用和自动编译，不是新的 off/auto 性能对照，也不是完整浏览器覆盖证明。iframe、Shadow DOM、画布、任意编辑器等仍需按原生能力覆盖验证；ab5 的全链路费用和 reasoning 未通过项仍然有效。

## 向导与 HTTP 完整对照：ab5

**连续短路已生效，但完整性能验收仍未通过。** 本轮从空库自动 Claim → Compile → Reflex，浏览器与 HTTP 场景各收集 20 对复用任务，并保留各模式首次任务，共 **84 条记录，全部正确**。没有预装 Reflex 或给任务追加编译提示；使用真实 JEV、主 LLM、Chromium 和本机动态页面 / HTTP 取证服务器。运行期间冻结的 196 个源文件在结束后哈希一致，未拼接旧样本、删除慢样本或放宽门槛。

| 复用阶段指标 | 浏览器 off → auto | HTTP off → auto |
| --- | ---: | ---: |
| 正确任务 | 20/20 → 20/20 | 20/20 → 20/20 |
| 主 LLM 调用 | 194 → 27（−86.1%） | 45 → 20（−55.6%） |
| 全部 LLM input | 716,296 → 112,079 | 134,464 → 68,329 |
| 全部 LLM input+output | 738,606 → 119,418（−83.8%） | 145,853 → 73,951（−49.3%） |
| 全部 LLM output | 22,310 → 7,339（−67.1%） | 11,389 → 5,622（−50.6%） |
| LLM reasoning（output 子集） | 5,179 → 4,790（−7.5%） | 1,258 → 1,694（+34.7%） |
| 中位耗时 | 39.207 → 15.108 s（−61.5%） | 7.552 → 4.714 s（−37.6%） |
| p95 耗时 | 47.309 → 29.312 s | 12.356 → 5.948 s |
| 正确、变快且 LLM input+output 减少 ≥80% 的配对 | 13/20 | 0/20 |
| LLM+JEV 总 token | 738,606 → 1,928,145（2.61 倍） | 145,853 → 419,409（2.88 倍） |
| 参考费用 | $0.024549 → $0.085041（3.46 倍） | $0.010144 → $0.020767（2.05 倍） |

### 短路机制实际完成了什么

- 浏览器 14/20 个复用任务只有一次末尾 LLM 调用；其余五个任务两次、一个任务三次。四个填写任务全部正确、没有误提交。URL、控件顺序、异步更新和表单变化复用同一个自动编译出的通用场景，没有保存页面动作路径。
- HTTP 每个任务一次末尾 LLM 调用。Reflex 执行一次原生批量候选后，四个端点各读一次：20 个任务共 80 次读取、0 次重复读取；off 为 100 次读取，其中 20 次重复。扫描结论仍依据响应证据，由 LLM 报告 VERIFIED / UNCONFIRMED。
- 浏览器实际执行 269 次 Reflex 动作；4 次 stale 被拒绝并恢复，0 次错误动作。两场景主模型请求前缀改写均为 0；本轮共 409 次 JEV 请求，没有请求错误或缺失用量。前缀稳定只证明未改写历史，不等价于服务端缓存命中保证。
- 复用阶段两个场景均没有新增 Claim/Compile；全部 LLM 用量仍包含它们，首次声明与编译成本单独列在下表。当前结果不是通过遗漏后台生成成本得到的。

### 未通过项及剩余成本

浏览器 accepted=false 的直接原因是 reasoning 仅下降 7.5%，未达到原有 30% 门槛。HTTP accepted=false 的直接原因是参考总费用上升 104.7%，未达到下降 15% 的门槛。整个 A/B 测试因此返回失败；本次不能表述为“所有 token / 成本目标已经解决”。全部样本都有 reasoning 计量，不能将这项失败解释为缺少 usage。

两种 80% 口径必须区分：浏览器总体 LLM input+output 减少 83.8%，且 13 对同时满足正确、变快和减少至少 80%；**计入 JEV 后，两场景没有任何配对达到全链路 token 减少 80%**。若只算 LLM output，浏览器有 6 对达到 80% 且变快；若只算 reasoning，则浏览器 2 对、HTTP 1 对。这些子项不能相互替代。

剩余开销有两个具体来源：

1. **交接后的模型仍会思考，偶尔补查。** 浏览器第 4 个任务追加了一次 curl，第 12 个任务读取执行审计；两个任务的回执都已包含完整的最终 receipt，不能归因于该结果被截断。第 14 个任务没有再调用工具，单次报告仍产生 816 reasoning token，耗时 46.121 秒，高于对应 off 的 39.207 秒。这些样本全部保留，不能把一次 LLM 调用当成零 thinking。HTTP 已无重复取证，但末尾报告 reasoning 总量仍增加。
2. **JEV 每次判别仍传输上下文、当前观察和有限选项。** 复用阶段浏览器 327 次请求消耗 1,754,862 input / 53,865 output token，HTTP 60 次请求消耗 330,498 input / 14,960 output token。它替代了较慢的模型回合，却没有实现全链路输入复用；主 LLM 的 KV 前缀稳定不会自动消除独立 JEV 请求的输入费用。

现有架构保证有界执行、原生授权、动态绑定和明确回退，不保证概率判别与末尾 LLM 未来永不误判或补查。运行模式仍默认 off，性能证据不足时不能将可选加速变成必须启用的内核功能。后续优化应针对上述两项成本验证，不应通过继续叠加页面规则、关闭必要取证或只挑成功样本来满足数字。

### 首次任务与证据

| 场景 / 模式 | 前台 / 含后台耗时 | 全部 LLM input+output | 其中 Claim + Compile output | 参考费用 |
| --- | ---: | ---: | ---: | ---: |
| 浏览器 off | 56.758 / 56.758 s | 56,461 | 0 | $0.001907 |
| 浏览器 auto | 37.096 / 37.550 s | 37,439 | 3,645 | $0.006825 |
| HTTP off | 4.872 / 4.872 s | 6,225 | 0 | $0.000369 |
| HTTP auto | 8.858 / 36.012 s | 14,572 | 3,450 | $0.003874 |

主模型为 deepseek-v4.1-flash，JEV 为 jev-1.13.0。费用使用测试输入的参考单价，并按报告中的 cache-read 计量；未核对网关实际账单。当前复用费用也更高，因此本轮没有通过增加同类复用次数摊平费用的回本点。本机 HTTP 取证场景不是完整外部 aiscan 引擎的生产验收。

证据：[84 条原始记录](../.runlogs/jev-controller-ab5-20260929.json)、[汇总与逐任务 / 阶段归因](../.runlogs/jev-controller-ab5-20260929-summary.json)、[完整测试日志](../.runlogs/jev-controller-ab5-20260929.log)、[196 个源文件哈希](../.runlogs/jev-controller-ab5-source-20260929.json)、[结束后完整性校验](../.runlogs/jev-controller-ab5-verified-20260929.json)。相关机制、原生工具、集成与 race 测试已通过，完整 A/B 的失败状态仍保留；全仓库既有失败列在后文。

## 控制权修订过程与历史对照

控制器已实现连续执行、独立的参数缺口判别、明确 report/defer 交接、无效果 stale 恢复、紧凑私有上下文及原生 HTTP 批量候选。核心类型和工具边界不变，详见 [当前架构](jev-controller-design-20260928.md)。

2026-09-28 本轮通过 `go test -tags full ./tools/playwright ./exts/browser ./exts/jev -count=1` 和 `go test -race ./exts/jev ./core/tool ./agent/provider/jev -count=1`。定向真实 JEV 测试发现：参数判断看不到明确绑定时，缺参数与可填写场景会同时误判；完整候选移入共享 state 后，缺绑定、控件改名、已有绑定及已填写四个用例均通过。该诊断使用人工定义的通用场景，只证明判断边界，不替代自动 Claim/Compile 或性能测试。首次失败 [日志](../.runlogs/jev-generation-boundary-20260928.log) 和修正后 [日志](../.runlogs/jev-generation-boundary2-20260928.log) 都保留。

第二轮 [ab2](../.runlogs/jev-controller-ab2-20260928.json) 在完成 8 条记录后中止：JEV 推理请求发生连续超时，审计记录 40 次请求错误，数据存在缺失用量和未结算的在途任务，不能用于性能验收。未删除这些失败，也不将其计为免费；[中止说明](../.runlogs/jev-controller-ab2-interruption-20260928.json) 和 [源文件哈希](../.runlogs/jev-controller-ab2-source-20260928.json) 均保留。

进一步排查表明服务可正常推理：同一 Go 请求使用默认 HTTP/2 路径超时，HTTP/1.1 为 0.68 秒。provider 独立 transport 现固定 HTTP/1.1、同步 ALPN 并保留连接复用。回归复现并修正了克隆默认 transport 后 ALPN 仍携带 h2 的问题，测试使用支持 HTTP/2 的 TLS 服务器验证实际 HTTP/1.1 及两请求复用一个连接。移除全部 GODEBUG 绕过后，真实参数判断连续 12/12 通过、缺失用量 0：[协议对照](../.runlogs/jev-transport-default-20260929.log)、[HTTP/1.1 对照](../.runlogs/jev-transport-http1-20260929.log)、[修复后连续验证](../.runlogs/jev-generation-http1-fixed-20260929.log)。

协议修复后的 [ab3](../.runlogs/jev-controller-ab3-20260929.json) 完成浏览器各模式冷启动及 8 对复用后中止，用于修正控制权问题；不是完整性能验收。已完成的 8 对全部正确、无请求错误，但 auto 仍有 30 次主 LLM 调用，普通任务通常需要 3 次，填写任务需要 6–7 次。原因是待完成效果连续两次尚未出现就被迫交回模型，以及参数判断把尚未出现的条件表单误当成当前缺口。所有样本及 [中止说明](../.runlogs/jev-controller-ab3-interruption-20260929.json) 均保留，不与后续运行拼接。

修订后只限制相同状态的动作重放，连续观察仍由 JEV 在 32 次 / 120 秒总预算内决定；参数判别仅检查当前下一步。连续未变化观察保持控制、预算耗尽回退及当前不存在条件表单的回归均通过。真实 JEV 的五个参数边界连续两轮 10/10 通过：[日志](../.runlogs/jev-generation-next-step-20260929.log)。

### 修订后的完整冒烟：3 对 / 场景

[smoke4](../.runlogs/jev-controller-smoke4-20260929.json) 从空库自动 Claim/Compile，包含两场景各模式的冷启动及各 3 对复用，共 16 条记录，全部正确。未预装 Reflex，未在用户请求中添加编译提示。它验证修订方向，不能代替 20 对验收。

| 复用阶段指标 | 浏览器 off → auto | HTTP off → auto |
| --- | ---: | ---: |
| 正确任务 | 3/3 → 3/3 | 3/3 → 3/3 |
| 主 LLM 调用 | 26 → 4 | 8 → 3 |
| 全部 LLM input+output | 95,725 → 16,480（−82.8%） | 27,095 → 11,074（−59.1%） |
| 中位耗时 | 35.313 → 14.237 s | 5.670 → 3.140 s |
| 参考费用 | $0.003239 → $0.012103 | $0.001747 → $0.003094 |

浏览器三个配对均正确、耗时下降且 LLM token 减少超过 80%；主模型调用分别为 1、1、2 次，填写缺口只增加一个必要回合。两个场景错误动作、重复 HTTP 读取、stale、前缀改写及缺失用量均为 0。复用阶段 JEV 另消耗浏览器 265,801 / HTTP 51,759 token；**全供应商 token 与参考费用仍增加**，不能将 LLM 节省表述为总成本节省。完整 [汇总](../.runlogs/jev-controller-smoke4-20260929-summary.json)、[日志](../.runlogs/jev-controller-smoke4-20260929.log)、[冻结源文件](../.runlogs/jev-controller-smoke4-source-20260929.json) 保留。

### 完整 ab4：短路成立，正确性与费用仍未验收

[ab4](../.runlogs/jev-controller-ab4-20260929.json) 已从空库收齐两场景各 20 对及各模式冷启动，共 84 条记录。运行代码在测量期间未改动，[194 个源文件归档](../.runlogs/jev-controller-ab4-source-20260929.json) 保留。两个场景仍为 accepted=false。

| 复用阶段指标 | 浏览器 off → auto | HTTP off → auto |
| --- | ---: | ---: |
| 正确任务 | 20/20 → 19/20 | 20/20 → 20/20 |
| 主 LLM 调用 | 196 → 33（−83.2%） | 45 → 20（−55.6%） |
| 全部 LLM input+output | 751,843 → 147,683（−80.4%） | 143,601 → 73,937（−48.5%） |
| 全部 LLM output | 22,026 → 7,298（−66.9%） | 10,584 → 5,626（−46.8%） |
| LLM reasoning（output 子集） | 5,203 → 4,101（−21.2%） | 1,467 → 1,696（+15.6%） |
| 中位 / p95 耗时 | 41.071 / 49.097 → 14.564 / 38.137 s | 7.544 / 15.137 → 5.035 / 6.591 s |
| 参考费用 | $0.023641 → $0.087711 | $0.009820 → 未知 |
| 正确、变快且 LLM token 减少 ≥80% 的配对 | 14/20 | 0/20 |

浏览器第 8 个复用任务存在一次误提交，故不能用总体节省覆盖正确性失败；reasoning 减少也未达到原 30% 门槛。HTTP 每任务仅一次末尾主模型调用，80 次必要读取、0 次重复读取（off 另有 13 次重复读取），但有一次 JEV 重试的用量未知，不能计算完整费用或通过费用验收。两个场景前缀改写均为 0；浏览器有一次被拒绝的 stale。浏览器全供应商 token 为 off 的 2.66 倍，费用为 3.71 倍；HTTP 已记录的全供应商 token 为 off 的 2.93 倍，且仍缺一次尝试的用量。

复用阶段没有额外 Claim/Compile，说明当前剩余 reasoning 来自主模型交接本身，而非后台生成。[汇总和阶段归因](../.runlogs/jev-controller-ab4-20260929-summary.json) 与 [完整日志](../.runlogs/jev-controller-ab4-20260929.log) 保留，阈值没有放宽。费用依旧采用公开参考价格，不是已核对的网关账单。

### 当前修订：明确缺口类别

第 8 个任务里 JEV 的 generation 选择 ready，同时选择了 Continue，但用户要求的 Reference 为空、没有 fill 绑定。仅强调“第一项未满足要求”会误拦尚未出现的条件表单；加入条件适用性后，真实浏览器回归仍复现误选。因此没有依赖未校准的置信度阈值或增加表单程序分支，而是在同一 generation 问题中明确 ready / parameter / strategy / defer：只有 ready 可接受场景动作，其他类别交给 LLM。它不增加网络往返、类型、执行器或持久化字段。

新增真实浏览器回归使用 ab4 自动编译出的通用 Reflex、完整工具说明、当前用户条件请求及原生观察/候选，检查四个任务各自的空字段和已填写状态；另保留五个语义边界用例。新版本连续三轮共 **39 次真实判断全部通过**，用量完整：[日志](../.runlogs/jev-prerequisite-categories-20260929.log)。确定性回归覆盖 parameter、strategy、defer 三种出口均只由 LLM 补一次缺口，再由 Reflex 连续完成；相关 race 与 full JEV 集成测试通过。该诊断不是端到端性能验收，也不证明未来页面零误判。

这次修订冻结后的 [196 个文件](../.runlogs/jev-controller-ab5-source-20260929.json) 已完成独立 [ab5](../.runlogs/jev-controller-ab5-20260929.json)，结果见本文首节；从空库开始、各 20 对，不混入任何旧样本。

### 已拒绝的首轮控制器 A/B

首轮控制器已收齐 84 条记录，仍不通过验收；本节结果只对应 [归档源代码](../.runlogs/jev-controller-ab-source-20260928.json)，不代表修正后的 ab2。它确实减少了中间 LLM 回合，但不能以更快掩盖错误：四个填写任务都误提交，另有一个完成后的 LLM 重放任务出错，共 11 次错误动作。

| 复用阶段指标 | 浏览器 off → auto | HTTP off → auto |
| --- | ---: | ---: |
| 正确任务 | 20/20 → 15/20 | 20/20 → 20/20 |
| 主 LLM 调用 | 211 → 43 | 43 → 20 |
| 全部 LLM input+output | 846,661 → 209,184（−75.3%） | 137,834 → 75,702（−45.1%） |
| 全部 LLM output | 26,595 → 11,391 | 10,760 → 7,365 |
| 中位耗时 | 41.687 → 19.529 s | 7.087 → 5.680 s |
| p95 耗时 | 88.790 → 35.377 s | 13.983 → 7.889 s |
| LLM+JEV 总 token | auto 是 off 的 2.48 倍 | auto 是 off 的 2.80 倍 |
| 参考费用 | $0.028329 → $0.092568 | $0.010032 → $0.020337 |
| 正确、变快且 LLM token 减少 ≥80% 的配对 | 12/20 | 0/20 |

HTTP 从 auto 的重复 GET 112 次（Observe 基线）降到 0，每任务一次原生批量读取、一次末尾主 LLM 调用。尽管如此，在当前参考价格下总费用仍增加；不能把 LLM token 节省称为全链路 token 或费用节省。浏览器自动执行最终由 LLM 报告时仍有再执行风险，当前修正正验证去掉已恢复 stale 的噪音能否改善交接。

首轮 cold 参考费用：浏览器 off $0.001379 / auto $0.009932，HTTP off $0.000439 / auto $0.003940；未排除冷启动成本。两场景历史前缀改写 0、缺失用量 0。所有失败保留在 [原始数据](../.runlogs/jev-controller-ab-20260928.json)、[汇总](../.runlogs/jev-controller-ab-20260928-summary.json) 和 [日志](../.runlogs/jev-controller-ab-20260928.log)。费用仍为公开参考价格，代理实际账单未核对；本机动态网页/HTTP 取证夹具不代表外部扫描引擎的全面生产验证。

## 通用 Observe 重构验证

此处记录的是此前工具观察接口由 `Choices` 改为 `Observe` 的版本，当时 Reflex 为 When/Decide/Sources、持久化 version 1。2026-10-01 的通用修订改为运行时 Observe 表达式和 version 2，移除 Playwright/curl 的工具观察适配；当前机制见 [实现说明](jev-implementation-20260927.md)。本文件中的付费实测不能作为通用修订的性能验收。

本轮新增并通过的回归覆盖：无 Observe 的普通工具与宿主、已有 Reflex 接收页面动作而不再声明、观察源失败整轮回退、DOM 替换/重观察/消费/关闭并重建会话后的旧调用拒绝、普通调用仍可执行、表单原始事实与未知填写值、即时状态变化、fill/select/scroll/显式 wait。真实 Chromium 的三页自动声明/编译/复用机制测试通过（模型替身，仅证明机制）。完整 `full` Playwright/browser/JEV 测试、`full sqlite` aiscan 测试及相关 race 测试通过。

全仓库 `go test ./...` 未全绿：`cmd/harness` 请求数量为 11 而断言要求 10；`pkg/internal/architecture` 安装入口检查报告既有构造调用（包括 JEV 测试夹具）；`tools/katana` 的 leakless.exe 被 Windows 安全软件拦截。未为通过测试而放宽断言或关闭安全软件。本次相关路径的 `git diff --check` 通过。

真实 A/B 已完成：两种模式各一次发现任务，以及浏览器、HTTP 场景各 20 对复用任务，共 **84 条记录**，本轮没有续跑或丢弃样本。两组所有任务均正确，两个场景的 `accepted` 都为 **false**；功能通过不等于性能达标。

| 场景 / 复用指标 | off | auto | auto 相对变化 |
| --- | ---: | ---: | ---: |
| 浏览器正确任务 | 20/20 | 20/20 | 四个表单任务均无误提交 |
| 浏览器主 LLM 调用 | 195 | 168 | −13.8% |
| 浏览器全部 LLM input | 735,771 | 900,156 | +22.3% |
| 浏览器全部 LLM output | 24,162 | 25,185 | +4.2% |
| 浏览器 LLM reasoning（output 子集） | 6,776 | 10,733 | +58.4% |
| 浏览器中位 / p95 耗时 | 36.422 / 61.213 s | 42.116 / 85.205 s | +15.6% / +39.2% |
| 浏览器参考费用 | $0.025401 | $0.164880 | 6.49 倍 |
| HTTP 正确任务 | 20/20 | 20/20 | 无错误结论 |
| HTTP 主 LLM 调用 | 45 | 46 | +2.2% |
| HTTP 全部 LLM input | 134,119 | 169,641 | +26.5% |
| HTTP 全部 LLM output | 11,425 | 10,424 | −8.8% |
| HTTP LLM reasoning（output 子集） | 1,807 | 2,744 | +51.9% |
| HTTP 中位 / p95 耗时 | 8.005 / 11.094 s | 11.026 / 15.083 s | +37.7% / +36.0% |
| HTTP 参考费用 | $0.010641 | $0.050338 | 4.73 倍 |

auto 复用阶段实际执行浏览器 / HTTP Reflex 动作 **124 / 80 次**。浏览器出现 6 次 stale 拒绝、0 次误操作；HTTP 在必要的 80 次端点读取之外，off 又重复读取 12 次，auto 重复读取 **112 次**。工具调用总数分别为浏览器 413 → 367、HTTP 92 → 192。

复用阶段 JEV 请求分别为浏览器 **482 次 / 3,156,455 input token**、HTTP **192 次 / 935,198 input token**。包括首次发现，本轮共 **705 次 JEV 请求，0 次 EOF/请求错误，0 条缺失用量**。主模型请求前缀改写全部为 **0**。源文件哈希在结束时核对无变化；本轮性能数据仅对应 Observe 基线，不包含后续控制权修订方案。

### token 增加的归因

1. **浏览器输出增加主要来自额外声明。** 复用阶段主流程 LLM output 为 24,238，旁路一次 Claim 为 947，合计 25,185；相较 off 的 24,162，增加的 1,023 中有 947 来自 Claim。主流程 output 基本持平，但 reasoning 从 6,776 增至 9,990，说明动作接管没有对应消除主模型思考。HTTP 复用阶段没有 Claim/Compile 生成，output 略降，但模型调用和重复读取仍未减少。
2. **JEV 输入成本独立叠加。** 它不能解释“LLM output 增加”，却是总 token 和参考费用增长的重要来源。包括首次发现，浏览器运行判别 / 输出发现判别分别读取约 186 万 / 145 万 input token；HTTP 分别约 61 万 / 34 万。每次模型回合也会带来新的后台判别。
3. **即时观察导致连续执行碎片化。** 浏览器执行日志显示 Continue 已派发后，下一观察仍显示之前阶段。相同动作再次被选择时，当前 `seen` 保护会退出到 LLM；模型读取页面后下一轮 Reflex 再接管。wait 候选存在但未被稳定选用。因此接管多次点击不等于省掉相同数量的模型回合。
4. **交接没有消除重复取证。** HTTP 的 112 次重复读取直接证明，JEV 获取的证据未能稳定替代普通模型重新调用 curl。追加方式保留历史前缀，但中间回执与重复结果仍会增加后续输入；KV 前缀稳定不是总输入减少的保证。

首次发现成本单独列出，未混入上表：

| 场景 / 模式 | 前台 / 含后台耗时 | 全部 LLM output | 其中 Claim + Compile output | 参考费用 |
| --- | ---: | ---: | ---: | ---: |
| 浏览器 off | 34.992 / 34.992 s | 1,368 | 0 | $0.001394 |
| 浏览器 auto | 70.701 / 77.143 s | 9,265 | 7,252 | $0.014160 |
| HTTP off | 6.243 / 6.243 s | 412 | 0 | $0.000705 |
| HTTP auto | 9.569 / 62.267 s | 7,600 | 7,151 | $0.006136 |

浏览器首次生成中的 6,566 output token、HTTP 的 6,631 为 reasoning。auto 复用费用仍更高，因此本轮不存在可由更多同类复用任务摊平的回本点。费用使用公开参考价格，代理实际账单未核对；缓存 token 按报告中的 cache-read 价格核算。任务使用真实 JEV/LLM/Chromium 与本机证据服务器，不能视为外部扫描引擎的完整生产 aiscan 验证。

证据：[84 条完整记录](../.runlogs/jev-observe-ab-20260928.json)、[汇总与分阶段归因](../.runlogs/jev-observe-ab-summary-20260928.json)、[测试日志](../.runlogs/jev-observe-ab-20260928.log)、[源文件哈希](../.runlogs/jev-observe-source-20260928.json)。后续 [JEV 主导执行修订](jev-controller-design-20260928.md) 已实现；本节仍仅代表 Observe 基线，不能混作新版本的验收。

## 历史重试：Observe 重构前，20 对未通过验收

本轮已收齐浏览器、HTTP 取证两个场景各 20 对 off/auto 复用任务，加上各模式的首次任务，共 **84 条完整记录**。没有预装 Reflex，没有给用户任务追加 Claim/Compile 指令。浏览器在第 16 对 off 任务处发生一次超时中断，之后从原报告和持久化库续跑未记录的第 16–20 对；这次中断未取得完整用量，另行保留，不能计为零成本。

**当前版本不能认定为有效加速：浏览器正确性失败，两个场景的整体延迟和已记录用量均退化。** 下表统计 20 对已记录的复用任务，包含错误提交的任务；不包含那次缺失数据的 off 中断，所以浏览器完整尝试的总耗时、总费用不可精确比较。

| 场景 / 指标 | off | auto | auto 相对变化 |
| --- | ---: | ---: | ---: |
| 浏览器正确任务 | 20/20，另有一次超时后重试 | 16/20 | 四个填写场景均错误提交 |
| 浏览器前台中位耗时 | 34.538 s | 52.560 s | +52.2% |
| 浏览器前台 p95 | 42.634 s | 90.182 s | 2.12 倍 |
| 浏览器前台 LLM 调用 | 179 | 173 | −3.4%，未达 50% 目标 |
| 浏览器全部 LLM output | 20,861 | 39,969 | +91.6% |
| 浏览器 LLM reasoning | 5,179 | 24,066 | 4.65 倍 |
| 浏览器参考费用（已记录任务） | $0.023750 | $0.179785 | 7.57 倍 |
| HTTP 正确任务（人工复核后） | 20/20 | 20/20 | 两个旧字符串断言误报见下文 |
| HTTP 前台中位耗时 | 7.749 s | 10.271 s | +32.5% |
| HTTP 前台 p95 | 9.923 s | 18.863 s | 1.90 倍 |
| HTTP 前台 LLM 调用 | 43 | 43 | 无下降 |
| HTTP 全部 LLM output | 10,522 | 12,217 | +16.1% |
| HTTP LLM reasoning | 1,078 | 3,410 | 3.16 倍 |
| HTTP 参考费用 | $0.009815 | $0.051553 | 5.25 倍 |

费用包含 LLM 旁路声明/编译和全部 JEV 请求，使用原公开参考价格，代理实际账单未核对。不是生产账单或完整尝试总费用。浏览器与 HTTP 分别执行了 **132 / 88 次 Reflex 动作**，所以退化不是“完全未接管”。本轮 **715 次 JEV 请求均有用量、无 EOF、无超时**；全部主模型请求前缀改写为 **0**。

首次任务成本单独保留：浏览器 auto 前台 37.196 s、含后台 153.916 s，LLM output 16,848；HTTP auto 前台 4.768 s、含后台 63.632 s，LLM output 7,267。其中一条浏览器 Claim 请求的 8192 个 output 全用于 reasoning，没有产出 JSON，后续请求才生成声明。复用阶段的表格不能掩盖这些首次成本。

### 本次定位的机制问题

1. **填写前置条件没有可靠进入判断。** 浏览器 index 3/8/13/18 最终均到达第六步，但分别产生 1/2/2/2 次错误提交。日志能看到输入框为空时 JEV 选择 Continue；普通模型补填后完成任务不能撤销先前误操作。当前未知填写值会回退的设计边界没有稳定落实到实际判别。
2. **连续操作仍频繁回到主模型。** 异步页面改变后，观察/候选可能过期，或者立即观察仍为操作前状态，导致无进展退出和 LLM 再次读取。当前页面 `loading` 只判断 `document.readyState`，不能代表 fetch 驱动的业务流程已稳定。
3. **回执没有稳定消除重复取证。** HTTP 任务中 JEV 已读取四个端点，主模型仍再次发起 curl 循环；最终答复明确写有 “Re-checked all four”。因此多数任务仍需两次 LLM 调用，并叠加串行 JEV 请求。
4. **Claim/Compile 的 thinking 成本偏高。** 首次及部分后续声明有长 reasoning 或输出预算耗尽，需要计入总用量；当前模型配置下，旁路生成并不便宜或稳定。

这些是实测发现，不通过预置场景、删掉失败样本或调低性能门槛规避。当前结论仍为 `accepted=false`。

### EOF 的定位与重试

上轮报错来自 `http.Client.Do`，发生在拿到可用 HTTP 响应头之前；不是 JEV JSON 解析错误，也不是 Reflex 不匹配。多数 EOF 恰好约 5 秒返回，而客户端总预算为 10 秒；没有当时的代理或服务端链路追踪，不能据此断言具体是哪一端关闭了连接。

本次对“直连 / 本机代理”与“默认 TLS / 进程级 tlsmlkem=0”做四组各三次真实请求，**12/12 成功**，TLS 1.3 / HTTP/2 和连接复用均正常。之后第一轮诊断的 229 次 JEV 请求、完整记录中的 715 次请求也全部有用量，未重现 EOF。因此可以确认当前链路恢复，不能证明上次是 TLS 问题。

另发现原客户端仅重试 429/529，EOF 会立即返回。已补齐 EOF / unexpected EOF 的有限重试，共用原最多三次尝试及总超时，不增加配置、不重放工具。每个未收到用量的尝试仍计为未知。注入响应头前断连、截断响应体、持续断连和短 deadline 的测试，以及 provider race 检查全部通过。该改动在浏览器续跑前加入；续跑没有触发错误重试，原已完成数据保留原版本结果。

### 基准修正与数据边界

- 第一次重试仍使用“首个失败就停止”的旧基准，未跑完整。随后改为保留错误和没有接管的任务，继续收集配对，不降低验收门槛。
- 第二轮浏览器 off index 16 耗尽 4 分钟预算后，旧测试把同一个已到期 context 用于 `WaitIdle`，在写入任务行前终止。现在后台结算独立限时，先保存任务失败，结算不完整时明确标记费用未知；续跑只补未记录的任务。原中断报告和日志保留，合并报告中的 `interruptions` 记录这次缺失，汇总 `accounting_complete=false`。
- HTTP index 19 的两份回答都明确给出 UNCONFIRMED，并正确报告四条证据与失败检查。旧断言禁止答案任何位置出现 VERIFIED，误把对判定条件的解释当成肯定结论。**原始 `correct=false` 不改写**；上表人工复核为正确。后续测试要求明确的最终 `Verdict: ...` 行，并单独检查该行；已有回归防止解释性提及再次误报。本轮耗时和 token 来自修正前的原任务格式，没有伪称重跑了新格式。
- 两个场景均为真实 JEV/LLM/Chromium、使用本机证据服务器的代表性任务，不是外部扫描引擎的完整生产 aiscan 性能验证。

本次修改后的 JEV / Guardrail 回归、性能验收逻辑测试、最终结论断言测试均通过；JEV provider race 测试通过。付费性能测试按上述数据失败。

证据：[84 条完整记录及中断说明](../.runlogs/jev-claim-reflex-ab-complete-resumed-20260928.json)、[用量与耗时汇总](../.runlogs/jev-claim-reflex-ab-summary-20260928.json)、[续跑前原始报告](../.runlogs/jev-claim-reflex-ab-complete-20260928.json)、[首次诊断重试](../.runlogs/jev-claim-reflex-ab-retry1-20260928.json)。网络四组追踪分别为 `.runlogs/jev-network-retry-{0,1,2,3}-20260928.jsonl`。

## 上一轮：Claim → Compile → Reflex 功能验证

当前实现遵循 [实现契约](jev-implementation-20260927.md)，已移除 learn/training、样本标签、历史重放与激活流程。JEV 判别每次模型输出及新用户请求，旁路 LLM 声明 Claim、编译 Reflex；运行时 JEV 从实时工具候选中连续选择，通过原 Executor 执行，结果只追加到主历史。

**功能闭环已经实测通过，性能验收尚未通过。** 空库浏览器测试完成自动声明、编译及跨页面接管，真实 aiscan 默认流式 profile 正确工作。另一次计划执行 20 对任务的 A/B 测试遇到连续 JEV EOF / 超时，正确回退 LLM，但没有完成复用接管和性能对照，不能声称已达到 token 或延迟优化目标。

### 实际环境

- 主 LLM：`https://api.chainreactors.cn/v1`，`deepseek-v4.1-flash`。
- JEV：`https://api.typesafe.ai/v1/systemone`，`jev-1.13.0`。
- Windows、本地 Chromium；任务目标为本机测试服务器，实际 Agent / Executor / 扩展生命周期。
- 凭据仅通过进程环境变量提供。本机代理为 `http://127.0.0.1:1080`，测试进程设置 `GODEBUG=tlsmlkem=0`；未修改生产 TLS 配置。
- 费用采用公开参考价格，代理实际账单未知；失败请求缺少 usage 时完整费用未知。后台声明、编译和全部 JEV 请求均计入报告。

### 空库自动浏览器闭环

`TestBrowserReflexRoutesAndOperatesUnseenPages` 从空库开始，第一条普通任务自动产生 Claim 和 Reflex，之后复用到控件名称及页面结构不同的目标。没有预装场景，也没有在用户任务中要求声明或编译。

| 目标 | 前台耗时 | 含后台耗时 | 前台 / 全部 LLM 调用 | JEV 请求 | JEV 入口及页面动作 | 目标动作 / 误操作 |
| --- | ---: | ---: | ---: | ---: | --- | ---: |
| Archive（首次发现） | 16.546 s | 84.381 s | 7 / 10 | 12 | 否，普通模型执行 | 1 / 0 |
| Invoices（复用） | 8.338 s | 8.819 s | 2 / 2 | 5 | 是 | 1 / 0 |
| Inventory（复用） | 6.132 s | 6.635 s | 2 / 2 | 5 | 是 | 1 / 0 |

三页最终答案均包含真实页面收据。后两页的追加回执同时包含 JEV 打开浏览器和点击当前目标的原生调用；场景不保存 URL、selector 或操作序列。服务器核对目标动作次数，避免普通模型重复执行仍被算作成功。

实际主模型 JSON 请求中，已提交消息前缀累计改写 **0 次**。这证明该测试中的追加方式保留请求前缀，不保证供应商 KV cache 的命中率。

三个不同任务的耗时不能代替 off/auto 配对实验。首次发现的后台开销明显，不能排除后再宣传整体收益。

证据：[空库浏览器报告](../.runlogs/jev-claim-reflex-browser-v3-20260928.json)、[执行日志目录](../.runlogs/browser-20260928-062428/)。前两次失败尝试亦保留于 `.runlogs/jev-claim-reflex-browser-20260928.json` 与 `...browser-v2-20260928.json`，不计入成功数据。

### aiscan 实际流式 profile

`TestLiveJEVProfileHTTP` 使用 aiscan 自身扩展组合、默认流式 Session 和空库 `auto`，独立 Guardrail 设置为 `none`。本机证据端点返回 HTTP 403、随机标记与 `check_pass=false`。

结果：正确输出 **UNCONFIRMED**，端点只读取一次；前台 **6.606 s**，2 次 LLM 调用，4,255 input / 379 output，其中 reasoning 142、cache read 1,920。该测试验证组合和流式协议，不加载外部扫描引擎，不能代表完整扫描或 HTTP Reflex 加速。

证据：[aiscan profile 报告](../.runlogs/jev-claim-reflex-profile-20260928.json)。

### 实际 A/B：失败，不能给出性能结论

`TestLiveAutomaticReflexAB` 比较 `off / auto`，每种模式先执行一次普通发现任务，再计划运行 20 对复用任务；含六步浏览器流程、重排控件、延迟渲染、需要新输入的页面，以及四个独立 HTTP 证据端点。

| 场景 / 模式 | 阶段 | 正确 | 前台 / 含后台耗时 | 前台 LLM 调用 | JEV 请求 / 缺失 usage | Reflex 动作 |
| --- | --- | --- | ---: | ---: | ---: | ---: |
| 浏览器 off | 首次任务 | 是 | 44.585 / 44.585 s | 9 | 0 / 0 | 0 |
| 浏览器 auto | 首次任务 | 是 | 54.251 / 131.646 s | 12 | 19 / 11 | 0 |
| 浏览器 auto | 第一次复用 | 是 | 92.553 / 97.577 s | 11 | 21 / 21 | 0 |
| HTTP off | 首次任务 | 是 | 5.016 / 5.016 s | 2 | 0 / 0 | 0 |
| HTTP auto | 首次任务 | 是 | 6.692 / 15.083 s | 2 | 3 / 3 | 0 |

浏览器已从四个 Claim 自动编译出通用 Reflex，但随后请求连续返回 `EOF`、`unexpected EOF` 或 `context deadline exceeded`；复用任务的 21 个 JEV 请求均未获得 usage，也没有实际 Reflex 动作，测试按预期失败。HTTP 的三个 JEV 请求全部失败，空库未产生场景，亦提前终止。日志只能确定 JEV 调用传输失败，不能进一步断言故障位于代理还是服务端。

这些任务经普通模型回退完成，所有主历史前缀检查为 0 次改写；等待失效旁路也增加了延迟。没有完成 20 对任务，完整费用未知，报告 `accepted=false`。没有继续反复运行长时间付费测试来挑选成功结果。

证据：[A/B 原始报告](../.runlogs/jev-claim-reflex-ab-20260928.json)、[旁路判别与执行日志](../.runlogs/jev-live-20260928-062724/)。原始报告的未知费用行以 `cost_known=false` 为准，旧汇总中的数值费用差无效；当前统计器已将未知费用差和收益改为 `null`，不改写原始证据。

### 回归与验收边界

已通过 Agent/provider、JEV、Command/Executor、Guardrail、curl、harness 定向回归；`full` 的真实 Chromium / browser / JEV 测试；`full sqlite` 的 aiscan 包测试；Agent、JEV provider / ext 与 Command 的定向 race 检查。

空库确定性测试验证：后台 Compile 未完成时前台能继续；同一任务后续边界用 Reflex 完成连续步骤；四次动作只需两次前台 LLM 调用，另外一次 Claim、一次 Compile 旁路调用全部计数。另覆盖一次性 Claim 消费、跨任务隔离、完整流式输出、多 toolcall、持久化、取消、非法生成字段、实时候选失效及无进展退出。统计测试拒绝缺失费用、没有接管、改写前缀和少于 20 对任务的性能通过。

最终的批量声明发布、所选能力无进展检查和 curl 去除重复历史已通过回归；尚无覆盖全部最终改动的成功完整 A/B 报告。性能目标仍为：浏览器主 LLM 调用下降 50%、全部 LLM output 下降 30%、中位耗时下降 20%；aiscan 中位耗时及总费用下降 15%；p95 退化不超过 10%。首次发现成本单独列出并参与成本摊销。

### 当前复现入口

先在测试进程中配置 `CYBER_API_KEY`、`CYBER_PROVIDER`、`CYBER_BASE_URL`、`CYBER_MODEL`、`TYPESAFE_API_KEY`。不将密钥写入脚本或报告。以下付费测试均须显式启用：

```powershell
# 空库普通请求 → 自动 Claim/Compile → 后续页面接管
$env:JEV_BROWSER_LIVE = '1'
$env:JEV_BROWSER_REPORT = 'D:\path\to\browser-report.json'
go test -tags full ./exts/jev -run '^TestBrowserReflexRoutesAndOperatesUnseenPages$' -count=1 -v -timeout 10m

# aiscan 默认流式 profile
$env:JEV_PROFILE_LIVE = '1'
$env:JEV_PROFILE_REPORT = 'D:\path\to\profile-report.json'
go test -tags 'full sqlite' ./cmd/aiscan -run '^TestLiveJEVProfileHTTP$' -count=1 -v -timeout 5m

# off/auto 完整对照，默认 20 对复用任务
# JEV_BENCH_PRICES 为两个实际模型的每百万 token input/output/cache_read 价格 JSON。
# JEV_BENCH_PRICE_SOURCE 注明价格来源；代理实际账单须另外核对。
$env:JEV_BENCH_LIVE = '1'
$env:JEV_BENCH_REPORT = 'D:\path\to\ab-report.json'
go test -tags full ./exts/jev -run '^TestLiveAutomaticReflexAB$' -count=1 -v -timeout 120m
```

`JEV_BENCH_PAIRS` 可缩小冒烟规模，但少于 20 对不能建立性能验收。当前没有 learn 模式、训练预算或强制激活命令。

## 历史证据：已替换的学习版本

以下内容原样保留用于追溯旧短路故障；其中学习、训练、预装规则、ABC 测试及旧复现命令均不适用于当前实现。

<details>
<summary>展开历史测试和故障诊断</summary>

历史结论：可选扩展安装、真实浏览器接管和 aiscan 普通工作流可以运行；“无提示自动学习并稳定减少 token / 延迟”尚未通过验收。不能把预装规则的成功，或者尚未接管时的模型耗时差异，算作自动加速收益。

## 测试条件

- 实际 L2：`https://api.chainreactors.cn/v1`，`deepseek-v4.1-flash`。
- 实际 JEV：`https://api.typesafe.ai/v1/systemone`，`jev-1.13.0`。
- Windows、本地 Chromium、真实 Agent / Executor / 扩展生命周期；网页和 HTTP 证据来自仅监听本机的测试服务器。
- 凭据通过测试进程环境变量提供，不写入报告或代码。
- 本机代理环境使用进程级 `GODEBUG=tlsmlkem=0`。没有改动生产 TLS 配置。
- 费用配置使用厂商公开参考价格，代理实际账单未确认。失败请求缺失 usage 时标为未知，不能按零费用计算。

## 真实浏览器与真实 L2

`TestBrowserReflexRoutesAndOperatesUnseenPages` 使用同一条预装的通用能力规则，在三个未见页面上执行不同目标。规则没有保存测试 URL、selector 或操作序列。每轮重新观察实际 DOM，包括按钮变链接、控件重排和目标名称变化。

| 目标 | 前台耗时 | 含后台验证耗时 | 前台 L2 调用 | JEV 请求 | 目标动作 / 误操作 |
| --- | ---: | ---: | ---: | ---: | ---: |
| Archive | 29.831 s | 33.644 s | 5 | 5 | 1 / 0 |
| Invoices | 11.895 s | 15.067 s | 4 | 4 | 1 / 0 |
| Inventory | 9.985 s | 13.854 s | 3 | 3 | 1 / 0 |

三页均通过。每页要求追加回执中同时存在 JEV 的浏览器入口与页面点击，且测试服务器只收到一次目标请求、没有 Cancel 请求，最终回答包含实际页面收据。普通 L2 完成全部工作或重复执行目标动作，都不能让这个测试通过。

全部 L2 用量（含后台验证）为 46,147 input / 1,761 output，其中 reasoning 278、cache read 43,008；JEV 已报告用量为 41,665 input / 2,462 output，共 12 次请求，其中一次缺少 usage。因此本轮完整费用未知。这不是与关闭 JEV 配对的性能测试。

在发送给模型的实际 JSON 请求上检查已提交消息前缀，三页累计前缀改写次数为 **0**。这验证追加方式没有改写已有消息；不代表可以保证供应商缓存命中率。

证据：[最终浏览器报告](../.runlogs/jev-real-browser-final-20260928.json)。

## 实际 aiscan profile

`TestLiveJEVProfileHTTP` 使用 aiscan 自身的扩展图、默认流式 Session 和 `auto` 模式，确认 `jev` 命令与 `BeforeModel` 钩子已经安装。从空库执行普通 HTTP 取证请求，用户任务没有 Reflex 提示。

服务器返回 403、每次运行随机生成的证据标记和 `check_pass=false`。模型真实读取一次响应，准确报告标记并给出 `UNCONFIRMED`。任务耗时 **5.002 s**，2 次 L2 调用，4,208 input / 189 output，reasoning 56，cache read 1,920。

这是扩展组合及流式工作流的冒烟验证。未加载外部扫描引擎，也未在单个冷启动任务中激活规则，不能据此宣称完整扫描加速。

证据：[aiscan profile 报告](../.runlogs/jev-aiscan-profile-20260928.json)。

## 空库自动学习与性能验收

`TestLiveAutomaticReflexABC` 对照 `off / learn / auto`，设置每组 12 个冷启动任务，激活后才允许进入至少 20 对留出任务。任务提示没有学习或 Reflex 指令；保持原有编译和激活门槛，不手动激活规则。

完整浏览器与 HTTP 对照两次被普通 L2 的工具续接 HTTP 400 中断：`reasoning_content` 必须回传。这在 `off` 路径也发生，且失败前 JEV 请求数为 0，不能归因于 JEV 接管，更不能计算加速率。

保留显式空 reasoning 后仍能复现这一上游错误。新增仅记录字段存在性的诊断显示，服务有时返回不含 reasoning 或 reasoning 为 null 的工具调用。部分这样的续接成功，部分完整对照失败。没有补造隐藏 reasoning，也没有自动修改 thinking 模式来让基准通过。

最后一轮 HTTP 诊断运行 699.71 秒，共执行 28 个任务，27 个完成。`off` 的第 10 个任务（index 9）再次收到 HTTP 400；该任务的字段诊断同时记录“上游工具调用响应缺失 / null reasoning”和“下一请求的对应字段缺失 / null”，确认这次没有可供客户端原样回传的 reasoning。该轮在这里停止，没有进入留出测试：

| 模式 | 正确 / 已运行任务 | 前台 L2 请求（含失败尝试） | 全部 L2 已报告 token（含编译） | JEV 请求 / 规则数 |
| --- | ---: | ---: | ---: | ---: |
| off | 9 / 10 | 27 | 79,500 | 0 / 0 |
| learn | 9 / 9 | 20 | 79,356 | 0 / 0 |
| auto | 9 / 9 | 20 | 89,102 | 0 / 0 |

三组请求前缀改写次数均为 0。`off` 有 3 次、`learn` 有 2 次请求未获得 usage，因此上表不能作为完整账单比较。样本数量也不相等，且没有任何 JEV 接管，不能从耗时或 token 差异计算加速比例。

另一个独立问题是训练覆盖率：实际 L2 经常批量请求多个 URL，或把页面操作和状态读取合在同一个 shell 调用中。当前学习只接受能精确绑定到一个候选的成功原生 ToolCall，这些普通轨迹往往只产生终止时的 defer 样本，无法为工具能力提供足够正样本。本轮 learn / auto 各收集了 9 个 curl defer、0 个 curl 正样本；各有 9 个上下文样本。

HTTP 诊断在第 8 个普通任务后自动触发了上下文规则编译。两组都失败，并非手动跳过学习：

| 模式 | 编译耗时 | Input | Output / reasoning | 结果 |
| --- | ---: | ---: | ---: | --- |
| learn | 57.031 s | 13,916 | 8,192 / 8,192 | 输出预算耗尽，无规则 JSON |
| auto | 61.108 s | 15,067 | 8,192 / 8,192 | 输出预算耗尽，无规则 JSON |

编译已经共享重复上下文，但仍未在当前模型和预算内产出规则。没有继续增加预算或降低门槛来改变验收结果。这些后台费用和耗时计入各自模式的总用量。

证据：[首次对照](../.runlogs/jev-acceptance-20260928.json)、[保留空 reasoning 后的对照](../.runlogs/jev-acceptance-fixed-20260928.json)、[HTTP 协议诊断与冷启动对照](../.runlogs/jev-http-protocol-20260928.json)。这些报告保留每任务的调用数、用量、正确性、历史前缀检查及失败信息。未完成留出验收时，`accepted=false`。

## 本轮修复

1. OpenAI 兼容 provider 保留明确返回的空 `reasoning_content`，避免下一次请求省略它；流式解析和消息累计也保留字段存在性。缺失 / null 字段仍保持缺失，不伪造模型推理。
2. JEV 追加回执包含实际已派发的工具及参数，扩展提示明确这些动作属于当前任务。失败或被拦截的调用标为尝试，不声称已经成功执行。浏览器执行结果同时记录观察到的控件名称与来源 URL，让 L2 能继续检查当前状态，减少返回原页再次操作。
3. 实测断言检查服务器实际动作次数、JEV 是否真正执行入口和点击、供应商请求前缀是否变化，并将后台验证和失败请求纳入用量。

修复前，真实 L2 曾在 JEV 点击成功后重新打开页面并重复操作，测试按动作次数失败。修复后的三页验证通过，但这不构成任意任务都不会重复操作的保证。

常规回归通过：Agent/provider、JEV native provider 与 Reflex、Guardrail、Command/Executor、curl、harness；`full` 的真实 Chromium 测试、`full sqlite` 的 aiscan 包测试；provider / JEV / Command 的定向 race 检查。付费自动加速验收仍按上述实际失败记录处理。

## 短路未生效的代码定位

后续离线诊断确认了两个独立的确定性阻塞，均不依赖上游 HTTP 400：

1. **环境标识混入动态时间。** `jev.environment` 对完整 `cfg.SystemPrompt` 做哈希；实际 Session 每次运行调用 `resolveSystemPrompt`，默认环境段包含精确到秒的 `Current Time`。仅将时间推进一秒、保持模型/工具/策略不变，环境标识就改变。训练按 `Source/Environment` 分组、接管要求 Environment 完全一致，因此实际 harness 中跨运行的训练积累和规则复用会被切断。固定 system prompt 的 A/B/C 测试没有覆盖此条件；单任务 profile 冒烟也无法验证跨任务复用。
2. **学习单元与真实工具调用不一致。** `collect` 仅接受一条 ToolCall 且完整 canonical 参数等于某个候选；重放验证使用同样条件。离线复现：`curl -i URL` 留下 1 个正样本；同一成功请求增加 `-s` 后留下 0 个；同一轮返回两条各自精确匹配的成功调用，也留下 0 个。实际学习记录中的零正样本与此一致。

离线探针位于 `.runlogs/jev-reflex-diagnosis_test.go`，通过 Go overlay 加入测试，不修改生产代码。运行 `go test -overlay .runlogs/jev-reflex-diagnosis-overlay.json ./exts/jev -run '^TestDiagnoseReflexEnvironmentAndSamples$' -count=1 -v` 可复现当前行为。这里的 PASS 表示重现诊断，不表示缺陷已修复。

另外，当前 `context` 采样把无工具调用的普通最终回答整个作为 Label。工具正样本被丢弃后，实际触发的是最终报告的上下文编译；编译器却要求有限判断，不能生成报告。这解释了当前学习目标的错位。两次 reasoning 预算耗尽是观测到的失败表现，不能仅靠增大预算证明这个目标有效。

执行侧也只有部分短路：`beforeModel` 内部可连续执行多个工具，确实绕过了中间的 L2 决策；但钩子返回类型只有追加消息，Agent 随后仍调用 L2。有限判断分支追加文字后立即退出，因此只是给 L2 增加判断材料。浏览器候选还没有结果读取动作，回执未携带循环末次观察到的完整当前状态，L2 仍要安排读取与核验。这是预装规则测试仍有每任务 3–5 次前台 L2 调用的结构性限制。

修复顺序应是：稳定的模型/工具/策略环境标识 → 能力所属的明确动作语义与批量观测 → 真正有限决策的编译输入 → 短路后的状态交接与继续执行。动态信息仍参与本次决策，历史消息继续只追加；环境标识的修复不应通过删除实际约束来实现。

## 复现

先在进程环境中配置 `CYBER_API_KEY`、`CYBER_BASE_URL`、`CYBER_MODEL`、`CYBER_PROVIDER`、`TYPESAFE_API_KEY`，不要把密钥写进命令文件或报告。

```powershell
# 真实模型 + 预装通用浏览器规则，仅验证执行链路
$env:JEV_BROWSER_LIVE = '1'
$env:JEV_BROWSER_REPORT = 'D:\path\to\browser-report.json'
go test -tags full ./exts/jev -run '^TestBrowserReflexRoutesAndOperatesUnseenPages$' -count=1 -v -timeout 10m

# 实际 aiscan profile，流式会话、空库 auto
$env:JEV_PROFILE_LIVE = '1'
$env:JEV_PROFILE_REPORT = 'D:\path\to\profile-report.json'
go test -tags 'full sqlite' ./cmd/aiscan -run '^TestLiveJEVProfileHTTP$' -count=1 -v -timeout 5m

# 完整自动学习验收；JEV_BENCH_PRICES 要包含两种模型的 input/output/cache_read 价格
$env:JEV_BENCH_LIVE = '1'
$env:JEV_BENCH_REPORT = 'D:\path\to\abc-report.json'
go test -tags full ./exts/jev -run '^TestLiveAutomaticReflexABC$' -count=1 -v -timeout 120m
```

默认训练预算为每组 40 个任务；本轮诊断使用 `JEV_BENCH_TRAINING=12`。测试不会在没有激活规则时继续跑留出任务并伪报节省比例。

</details>
