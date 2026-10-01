# JEV 自动接管修复及真实验收（2026-10-01）

目标：普通 LLM 执行若干次后，自动发现可封闭的场景，动态生成 Observe 和原生调用绑定，交给 JEV loop；Playwright 不实现 JEV 专属接口、读取器或执行分支。

执行调度优先进入 JEV。已有场景由 JEV 持续选择并执行，只有未知场景、实际参数/策略缺口或服务不可用时才回退到普通 LLM；补齐后继续 JEV。JEV 完成执行之后，LLM 仅整理最终回答，这是用户确认的目标。

## 当前实现

链路为 `普通交互 → 后台发现/编译 → 动态 Observe → JEV 有限选择 → 普通 Executor → 实际结果 → Observe`。场景仍只有 When、Decide、Observe 三个字段。JEV 生产包不导入具体工具，也不按工具名分支。原生工具定义、普通命令文档和实际结果是输入，解析、地址提取及绑定逻辑由 LLM 在运行时生成；操作和读取都走原生准入与执行入口。

- 移除 Command/CommandRegistry、curl、Playwright 的 Choices/Contract 回调及特殊调用 ID 分支。Playwright 普通定位器实现未新增行为；仅补齐已有 CSS、XPath、语义定位语法及 evaluate 的普通接口文档。
- Observe 统一使用纯 Goja JavaScript。旧库在启动时完整备份并迁移；Expr 场景保留声明供重新编译，不继续执行。运行时没有文件、网络、Executor、浏览器对象或持久 VM 状态；普通外部读取另由 JEV 选择执行。
- 原生调用与结果按 call_id 关联。JSON 信封和字符串包裹在私有投影预算前统一解析，避免程序回显挤掉入口/句柄；完整原始证据及主模型前缀保留。program 仅序列化函数字面量、纯 JSON 辅助函数及显式参数。动态普通读取器可直接返回 `{state,candidates}`，Observe 复用实际协议结果，避免第二次解析及绑定。
- 新 JavaScript 绑定必须显式提供布尔 read。效果之后若有生成的检查候选，实际执行该检查后才开放 REPORT；不按工具或页面识别完成状态。
- 编译器只生成原始 Observe 程序；When/Decide 复用已分组 Claim 的有限语义。按实际轨迹试算，检查复制资源、无关措辞和明确不要求执行的引用示例。JEV 在同一个请求中复审整体程序及实际边界的下一步覆盖；具体缺口或有限错误分类进入最多三个草稿的纠正预算，接口故障直接回退。允许真正缺少依据的部分读取交接、再以实际证据扩展。后台不执行生成候选。
- 程序或绑定失败会撤下场景、保留声明供后续重编译；不重放失败效果。结构化原生结果优先进入回执，防止程序回显挤掉实际结果。REPORT 后额外工具调用触发缺陷核对，冗余核实不应重编译。
- 普通任务完成后才编译/修复；交互中仍可发现和声明。最终发现使用最近实际调用作为焦点，同任务未匹配的声明仍可进入编译。一个能力包含完整工作流，不能把会话策略当成用户能力。修复仅核对实际交接时存在的场景及支持声明。
- 编译前判别 whole/partial 所有权，纯程序试算后由 JEV 作有限准入；拒绝和格式错误共享三份草稿预算。任何已由轨迹和文档证实的操作都不能以 partial 为由省略。纯 Observe 及动态普通读取器共用数组/映射归一化及绑定验证；错误及效果之后不复用旧协议结果。
- Observe 支持数据表达式或纯函数入口，函数只在同一个受限 VM 中调用一次；生成器单份 JavaScript 代码块也可作为源码信封读取，仍执行相同验证。未知原生操作的输出格式不能靠名称猜测，可编程读取器自行定义 state 及完整调用绑定。审核边界共享重复调用，避免反复发送同一读取程序。
- 修复债务及最初的缺口快照在当前任务内保留。主 LLM 补齐动作之后，即使 JEV 继续读取并 REPORT，也不能覆盖缺口或使最终回答跳过修复。
- 已有实际交接和补充证据时，在原归并请求内由 JEV 判断具体修复是否必要；不能因为能力已有场景而忽略缺绑定。冗余核实或仅缺用户输入时可 defer，不调用生成器；需要修复时有界生成，null 保留原场景，纯试算及 JEV 准入仍不可省略。可编程读取的 Observe 收敛为句柄恢复、入口或一个观察生产器，全部操作绑定由动态生产器返回。
- 共享判别状态增加按候选 ID 的 reads 标记，使 generation 头可识别已经绑定、能取得当前外部事实的读取；不能要求未来效果在读取之前就有完整标识符。没有放宽真实参数、授权及策略缺口的交接。

接口通用性不等于模型能稳定生成正确程序。源码和有限复审均不能证明所有页面、分支及效果分类正确。规模限制、异步工作预算和 Goja 没有硬内存隔离的限制见[实现契约](jev-implementation-20260927.md)。

核心决策均由 JEV 作有限判断；它们共享现有请求，不引入一个由 LLM 判断是否使用 JEV 的上层路由。

| 控制决策 | JEV 判别入口 | LLM 的职责 |
| --- | --- | --- |
| 是否复用、选择哪个场景 | entry | 未覆盖时生成或补足 |
| 当前是否需要 LLM | generation 及场景 defer | 补足当前参数、绑定或策略缺口 |
| 是否创建有限问题 | discover | JEV 选择 new 后生成 Claim |
| 是否编译或修复 Reflex | group 中的 compile | 获准后生成 Observe |
| 生成程序是否准入 | validate 及边界 coverage | 被拒绝时在有界预算内纠正 |
| 下一调用、读取、完成或交接 | 当前 Reflex 的有限选项 | REPORT 后仅整理最终回答 |

## 验收条件

真实服务：用户提供的 JEV 凭据、`https://api.chainreactors.cn/v1` 的 `deepseek-v4.1-flash`，分别测过后台 `none` 和 `low`。凭据只注入进程环境，不写入代码、报告或模型请求。每个冷启动从空库开始；没有编译器固定响应、预置场景、固定 selector 或手动激活。

保留 `TestBrowserReflexRoutesAndOperatesUnseenPages` 的单任务发现窗口。独立的 `TestBrowserAutomaticTakeoverAfterBoundedDiscovery` 固定为 3 个发现任务加 5 个新验证页面：改变 URL、随机 ID、目标/干扰标签、元素类型，并覆盖无 ID 元素。每行要求真实 receipt、服务端效果恰好一次、错误效果零；修改页面属性也计为错误效果。五个验证页面还要求 JEV 实际打开和点击、完整读取结果、主 LLM 仅一次最终回答且不执行工具、场景定义及已提交请求前缀不变。2026-10-01 的协议读取实验曾在较宽条件下 PASS，但主 LLM 仍有补读；因此新增 closed_loop 字段及严格检查，旧 PASS 不作为当前闭环验收。

每行落盘，expected_tasks/test_finished 区分完整及中断报告。后台结算独立计量；没有完整费用来源，不宣称全链路节省。

最新验收从 execution 日志的实际调用统计打开/点击，不再搜索回执文本中的命令字符串。读取器源码可能包含尚未执行的 click，这不能计为接管。旧报告中的操作字段采用旧统计方式，不能单独证明实际执行，必须结合原始日志核验。

最新检查还要求 execution 日志中有成功原生结果包含所需 receipt（jev_result_evidence）；读取器源码或回执说明中的字符串不计。该字段加入之前的结果需独立核对原始日志。

## 保留的真实结果

首次复审的失败快照见[原记录](jev-review-validation-20261001.md)。下面列出继续修复期间的关键结果；不同源码版本的成功不能替代最新版本验收。

| 报告 | 结果及限制 |
| --- | --- |
| [早期三次发现验收](../.runlogs/jev-browser-bounded-discovery-1790804732839.json) | PASS；五个新页面均完整接管、每页主 LLM 一次。但这是较早的源码版本，独立重复出现结算失败。 |
| [内容反馈修复](../.runlogs/jev-browser-content-feedback-1790808033103.json) | 五个验证页面完整接管且场景稳定；整体 FAIL，首个普通 LLM 任务重复效果。 |
| [增量 low](../.runlogs/jev-browser-incremental-low-1790809961304.json) | 前四个验证页面完整接管，末页入口及场景变化，整体 FAIL。 |
| [二选一复审](../.runlogs/jev-browser-binary-review-1790810577731.json) | JEV 执行了验证页动作，但部分提前 REPORT、未报告实际 receipt，整体 FAIL。 |
| [效果后检查](../.runlogs/jev-browser-effect-read-report-1790811055942.json) | 业务任务完成，生成场景反复读取而未绑定效果，整体 FAIL。 |
| [显式 read](../.runlogs/jev-browser-explicit-read-1790812095758.json) | 无 ID 页面生成不支持的扩展定位语法；回退及场景变化，整体 FAIL。 |
| [补齐普通接口文档](../.runlogs/jev-browser-documented-native-1790812626512.json) | 八个业务任务正确，五个验证页均有 JEV 打开/点击；其中一次主模型补读引发场景变化，稳定性标准 FAIL。 |
| [协议读取器](../.runlogs/jev-browser-protocol-producer-1790813735553.json) | 较宽操作标准 PASS，八行业务正确，五个验证页 JEV 打开/点击且场景稳定；主 LLM 每页仍调用 2–3 次，严格闭环标准不通过。 |
| [协议读取器独立重复](../.runlogs/jev-browser-protocol-repeat-1790814103414.json) | 八行业务正确，但生成场景未覆盖用户入口，验证页依赖主 LLM 开会话且场景变化，整体 FAIL。 |
| [严格读取闭环](../.runlogs/jev-browser-strict-loop-1790814542994.json) | FAIL；发现任务中有一次缺实际 receipt，后续 JEV 入口/效果/读取不完整，还有错误效果。 |
| [已知循环绑定复审](../.runlogs/jev-browser-grounded-cycle-1790814785002.json) | 八行业务正确；仅两个热页面完整接管，其他页面漏绑定，严格标准 FAIL。 |
| [格式纠正 none](../.runlogs/jev-browser-format-repair-none-1790815497435.json) | 八行业务正确；三次发现后未形成完整循环，验证页仍依赖主 LLM，FAIL。 |
| [低温生成](../.runlogs/jev-browser-deterministic-generation-1790815926370.json) | FAIL；较低采样波动没有消除编译错误，验证页均需主 LLM 补充动作。 |
| [原协议契约对照](../.runlogs/jev-browser-original-protocol-contract-1790816195651.json) | 三个热页面完整接管，另外两个缺绑定，FAIL；对照发现目标解析绕过 JEV 的候选选择。 |
| [引用示例候选校验](../.runlogs/jev-browser-alternative-invariance-1790816555756.json) | 八行业务正确；生成程序仍不完整，严格标准 FAIL。 |
| [仅编译 Observe](../.runlogs/jev-browser-observe-only-1790817119811.json) | 八行业务正确；验证页仍需主 LLM 完成操作，FAIL；进一步收紧实际边界覆盖检查。 |
| [协议消费 none](../.runlogs/jev-browser-native-protocol-none-1790822501459.json) | 完整 FAIL；入口/读取不完整，验证页依赖主 LLM 操作。 |
| [协议消费默认推理](../.runlogs/jev-browser-native-protocol-default-1790822501459.json) | 20 分钟超时，仅六行，test_finished=false；不计完整验收或费用。 |
| [完整轨迹学习](../.runlogs/jev-browser-completed-learning-1790823287178.json) | FAIL；最后发现焦点落在纯总结，未及时编译完整能力。 |
| [完成焦点修复](../.runlogs/jev-browser-completed-focus-1790823839726.json) | FAIL；生成程序只有入口和读取，已知效果仍由主 LLM 补充。 |
| [完整循环 none](../.runlogs/jev-browser-whole-cycle-none-1790824623147.json) | FAIL；三次学习后无场景，后续部分场景仍不能完成循环。 |
| [完整循环 low](../.runlogs/jev-browser-whole-cycle-low-1790824623147.json) | FAIL；动态读取器返回完整调用数组，运行时未消费，反复读取后由主 LLM 操作。据此增加通用数组归一化。 |
| [数组归一化及独立审核 none](../.runlogs/jev-browser-generic-array-review-none-1790826588048.json) | 完整 FAIL；八行业务正确，但五个验证页只有 JEV 打开，没有实际 JEV 点击，主 LLM 补充。模型把会话策略当成能力，生成部分读取场景。 |
| [完整能力声明 none](../.runlogs/jev-browser-workflow-scope-none-1790826820605.json) | 完整 FAIL；仍生成缺少效果绑定的读取器，五个验证页主 LLM 补充。 |
| [修复生命周期 none](../.runlogs/jev-browser-repair-lifecycle-none-1790827185656.json) | 完整 FAIL；前期纯 Observe 数组被拒绝，后期草稿仍只有入口和读取；修复请求已能触发。 |
| [统一协议 none](../.runlogs/jev-browser-unified-protocol-none-1790827398255.json) | 完整 FAIL；一次验证页有实际 JEV 点击，但程序仍出错/变化，全部验证页未形成完整闭环。 |
| [统一协议 low](../.runlogs/jev-browser-unified-protocol-low-1790827541212.json) | 20 分钟超时，六行检查点，test_finished=false；额外源码审核及格式纠正使编译预算耗尽。 |
| [压缩审核材料 low](../.runlogs/jev-browser-compact-proof-low-1790828061737.json) | 四行后停止旧版本实验，test_finished=false；连续生成未调用函数，后续额外审核输出不完整。 |
| [纯函数入口 low](../.runlogs/jev-browser-pure-entry-low-1790828371338.json) | 四行后停止旧版本实验，test_finished=false；额外源码审核输出不完整，三次学习仍无场景。 |
| [轻量审核 low](../.runlogs/jev-browser-lightweight-generic-low-1790828901650.json) | 完整 FAIL；仅两个验证页闭环，其他页面效果绑定不完整。普通发现任务另有一次重复效果。 |
| [直接修复 low](../.runlogs/jev-browser-direct-repair-low-1790829569878.json) | 五个验证页全部闭环、场景稳定，原生日志确认实际 receipt；整体仍 FAIL，普通发现任务有错误点击及主 LLM 网络错误。不能将验证阶段成功写成整体通过。 |
| [直接修复独立重复 low](../.runlogs/jev-browser-direct-repair-repeat-low-1790830093082.json) | 完整 FAIL；三个发现任务业务正确，五个验证页均需主 LLM 补充。生成程序含多套猜测解析和线性进度。 |
| [单一观察生产器 low](../.runlogs/jev-browser-single-observation-low-1790832366963.json) | 完整 FAIL；八行业务正确、五个验证页实际 JEV 打开/点击且场景稳定，三个闭环。另两个在打开后被 generation 误判为参数缺口，主 LLM 准备读取后 JEV 继续点击和读结果；据此补齐共享 reads 标记。 |
| [共享读取分类 low](../.runlogs/jev-browser-shared-inspection-low-1790833058741.json) | 完整 FAIL；四个验证页闭环并有实际原生结果。无 ID 页面被生成器省略操作绑定，主 LLM 补充后场景替换。剩余页面没有再次发生打开后误判读取参数缺口。生成阶段另有格式失败、token 耗尽及一次网关超时。 |
| [结构地址 none](../.runlogs/jev-browser-structural-bindings-none-1790833685381.json) | 完整 FAIL；三次发现后无完整场景，五个验证页仍需主 LLM 补充，场景变化。none 没有 reasoning 用量、生成更快，但出现 helper 类型错误、入口资源和效果绑定不完整；速度不能代替正确性。 |
| [结构地址 low 旧版本](../.runlogs/jev-browser-structural-bindings-low-1790833968889.json) | 四行后停止旧版本，test_finished=false。生成输出为 js: 加 JavaScript 代码块，源码包装被误当作 JS 模板，反复报 TypeError；据此统一剥离包装，新增真正执行程序的回归。该未完成报告不能作为验收。 |
| [JEV 控制链 low](../.runlogs/jev-browser-native-controller-low-1790834536089.json) | 完整 PASS（103.15 秒）；第一项普通任务后已形成场景，后续七项均完整接管。五个验证页实际打开/点击/读到结果，主 LLM 各一次且无工具调用，场景稳定、效果一次、错误零、已提交前缀不变。该二版库运行尚未包含后续纯 JS/version 3 简化及修复必要性的 JEV 判断，仍需合并后独立重复。 |
| [纯 JS / JEV 控制链独立重复 low](../.runlogs/jev-browser-native-js-repeat-low-1790834952713.json) | 完整 PASS（143.19 秒），version 3，包含由 JEV 判断修复必要性。新空库第一项普通任务后自动形成不同的场景，后续七项完整接管；五个新验证页实际打开/点击/读取回执，主 LLM 各一次且无工具调用，场景稳定、效果一次、错误零、前缀不变。 |

当前原控制链及合并后的纯 JS/version 3 控制链各完成一次独立空库严格 PASS：第一项普通任务后自动形成场景，后续七项由 JEV 完成执行、LLM 仅整理最终回答。额外普通 LLM 源码审核已移除；保留纯试算及 JEV 有限复审。修复必要性同样由 JEV 判别，不再直接调用生成器。两轮成功覆盖这里定义的浏览器场景，不能证明任意工具或开放任务都能稳定生成。未完成实验保持 test_finished=false，不能计作完整费用或成功验收；所有失败及原始证据保留，不覆写为成功。

## 回归与运行

通用原生工具集成测试没有 CommandRegistry，工具名、ID、版本及 job 在运行时变化，覆盖 catalog/activate/receipt 组合、轮询及复用；其固定模型响应只验证机制。纯观察检查覆盖无 I/O、时间中断、候选预算、读取器序列化、显式 read、效果后实际检查、部分场景交接、场景撤下和实际补充后的修复。

```powershell
go test ./agent ./agent/provider ./agent/provider/jev ./core/tool ./pkg/exts/jev ./pkg/exts/guardrail ./tools/curl ./tools/toolargs ./pkg/harness -count=1
go test -tags full,sqlite ./pkg/exts/browser ./pkg/exts/jev ./cmd/aiscan -count=1
go test -race ./agent/provider/jev ./pkg/exts/jev ./core/tool -count=1
# 真实验收需要进程环境中的两组凭据及 CYBER_BASE_URL/CYBER_MODEL。
$env:JEV_BROWSER_LIVE='1'
$env:JEV_DECLARATION_EFFORT='low'
$env:JEV_BROWSER_REPORT='<新的报告路径>'
go test -tags full,sqlite ./pkg/exts/jev -run '^TestBrowserAutomaticTakeoverAfterBoundedDiscovery$' -count=1 -timeout=20m -v
```
