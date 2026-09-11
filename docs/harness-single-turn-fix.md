# 单次 Turn Harness 修改方案

> 状态：第一批已按评审后的收窄范围落地（4.1–4.5，含 provider XML）。4.6 截断整批、4.7 Deferred 状态机、4.8 完整预算 compact、默认 schema 收缩仍待评审，不作为本批完成条件。
> 范围：只修「单个 Agent 一次 turn 怎么执行」，不扩展到工厂层。
> 授权：按 2026-09-11 对话「按你的建议来」实施第一批；不提交、推送、合并、部署。

## 1. 问题与推荐

**现象：** Agent 调很多工具后空转、很久才回、打到上限或空回复。

**推荐：** 第一批只修真实执行路径的输出裁剪、正常交卷、收尾执行约束、空回复一次恢复、两层 XML 不执行。截断整批、Deferred 记账、每次请求前 compact、默认 schema 收缩全部后置。不重写 Loop、不合 Handle。

**主刹车：** 正常模型响应没有工具调用时交卷；保留现有明确的 todo reconciliation、steering 例外，但终止收尾状态不能被它们重新打开工具。

**保险丝：** `MaxToolIterations` 默认 20；chatbot 在 entry 未显式设置且解析值超过 5 时压到 5。计数对象是 LLM 循环轮次，不是工具执行次数。本次不改默认数字。重试、压缩、最终合成单独记账，均受原 turn 的取消与截止时间约束。

**对照边界：** Pi 的默认编码工具集较小，模型无工具及无待处理输入时结束；模型输出因长度限制截断时，本批工具调用全部不执行。这里不是指工具返回内容被裁剪。Codex 的 turn 主循环以是否需要继续、待处理输入和上下文容量决定推进/compact/结束，不能将固定 N 圈当正常完成策略。

源码参考：

- Pi：`packages/agent/src/agent-loop.ts::runLoop`、`packages/coding-agent/src/core/tools/index.ts::createCodingTools`。
- Codex：`codex-rs/core/src/session/turn.rs::run_turn`。
- 以上为评审时读取的公开源码，不修改对照仓库，也不照搬其整体架构。

## 2. 已确认现状

| 问题 | 文件 / 函数 | 关键行为 |
|---|---|---|
| 入口分叉 | `loop.go::HandleWebChatStream`、`api/openai.go::streamResponseFromAgent` | Web stream 调用 `HandleMessage`；OpenAI `stream=true` 调用独立的 `HandleMessageStream` |
| 统一裁剪被绕过 | `sdkbridge.go::buildSDKRegistry`、`toolAdapter.Call`；`tools/registry.go::Execute` | SDK 通过 `GetFunc` 直接调用函数，未走 `Execute → clipToolResult` |
| Stream 二次采样 | `loop.go::HandleMessageStream` | 非流式 Chat 已返回正文，却重新用完整 toolDefs 调 ChatStream；新工具调用不再执行 |
| 重复检测误标上限 | 两个 Handle 的 `loopDetected` 分支 | 写入换方法提示后 break，进入统一 cap 合成；不是实际耗尽圈数也贴 cap 标记 |
| 工具关闭只到 schema | 两个 Handle；`sdkbridge.go::buildSDKRegistry` | `callTools=nil` 未变成执行约束；SDK registry 包含全部调用者可见工具 |
| XML 可变成真实执行 | `tool_recovery.go::maybeRecoverToolCalls` | 将正文解析结果写回 ToolCalls，随后进入工具执行 |
| 缺少截断批次判定 | `provider/provider.go::Response`、`StreamChunk` | 没有标准化停止原因；Loop 无法按模型输出截断拒绝整批调用 |
| Deferred 污染统计 | `HandleMessage` 的工具批次与结果分类 | 未执行结果被当作非失败，影响 allFailedRounds、同名失败 streak，并计入 totalToolCalls |
| 上下文检查不完整 | `compactSession`、`maybeCompactMidTurn`、最终合成分支 | 开头及工具轮末已有 compact，但估算 session 而非完整请求；部分退出分支绕过轮末检查 |

事实更正：`maxToolResultRunes` 是 65,536 个 rune，不是 64KiB；当前 chatbot allowlist 是 10 个名字，不是 11 个。现有 compact 有热尾裁剪等补救，但不能据此断言真实工具入口已有限长保护。

## 3. 范围与取舍

| 项目 | 决定 |
|---|---|
| 两份 Handle | 保留，分别做局部修复；补同一故障场景的两入口验收不等于合并实现 |
| Registry | 保留现有生命周期、权限检查、注册与 mode 过滤语义，不改共享单例 |
| 输出裁剪 | 接通现有裁剪策略，覆盖完整成功/错误返回；保留阈值 |
| Compaction | 优先修调用位置与完整请求预算核对，复用现有压缩算法；不重写 `compaction.go` |
| MaxToolIterations / 失败阈值 | 默认数字保持不变，修正计数描述与触发动作 |
| Deferred | 保留本轮 cap 行为，修状态、统计和提示；本轮不实现执行器排队，也不增加 Stream cap |
| 默认 schema | agent/chatbot/customize 保持现状；不新增 agent 核心 allowlist、channel overlay |
| 工厂层 | 不改建 Agent、渠道逻辑、SOUL、dashboard、manager 注册和 setup |
| 交付边界 | 代码实施另行确认；不提交、推送、合并、部署 |

### 为什么暂缓 schema 收缩

少工具可能减少探索，但尚未用失败 turn trace 证明其贡献。直接隐藏 update_goal、调度、办公等工具会改变现有能力；活动 goal 的完成路径也需要验证。默认工具暴露策略应另行评审，包含按需恢复入口和能力回归，不作为本次可靠性修复的前置条件。

Channel overlay 不采用“allow 非 nil 且非空就代表 agent”的判断：chatbot 也符合这个条件。消息来自哪个渠道也不能直接代表任务需要哪种办公能力。

## 4. 实施切片

第一批（已实施）可独立验收。4.6 / 4.7 / 4.8 和 schema 收缩各有自己的完成声明，不并入本批。

### 第一批：输出与交卷边界

#### 4.1 接通统一工具结果裁剪

- `buildSDKRegistry` 的调用闭包复用 `Registry.Execute`，不再直接执行 `GetFunc` 返回的函数；保留工具 schema、并发安全判断、串行包装和 `DenyIfHidden` 权限检查。
- 检查 `Registry.Execute → toolAdapter.Call → executeToolsConcurrently` 完整返回路径。现有 Execute 只裁剪 result，adapter 还会拼接 err.Error()；最终进入模型的成功正文、错误正文和错误附加文本必须全部受限。
- 复用 `clipToolResult` 策略，避免重复追加错误提示，保持 error 非空以及真实错误类别，不能用裁剪掩盖执行失败。
- 阈值维持 65,536 rune；文案与实际单位统一。测试使用经过真实 SDK 桥接的假工具，不能只测裁剪函数。

#### 4.2 正常 Stream 终稿直接交付

- `HandleMessageStream` 无工具、正文非空且完成现有 todo reconciliation 后，直接使用已得到的 Response：持久化 assistant，返回 `stringStream(resp.Content)`，调用一次 `runPostTurn`。
- 保留 Thinking、RawAssistant、metadata、用量和最终消息；不能只保存正文而破坏下一 turn 的 provider 重放。
- 删除正常终稿分支的第二次 ChatStream；不把整个圈内模型调用改为另一份 Handle 的实现。
- 这是合法 SSE 交付，但正文一次返回，不是逐 token 流式。首字延迟仍包含前面的 Chat 和工具执行时间，本轮不承诺消除这段等待。
- 保留的强制合成流必须检查 `sr.Err()`，不能将中途断流当成功。空合成的错误/兜底正文必须实际交付给消费者，不能只写 session。
- 检查 OpenAI API 对 StreamReader 错误的传递，必要时只调整 `api/openai.go::streamResponseFromAgent` 的错误交付；不修改渠道或网页逻辑。

#### 4.3 明确终止收尾状态与执行约束

- 用 turn 级持续状态记录是否进入终止收尾及原因：重复调用、整轮失败、同工具连败或实际圈数耗尽。不要将工具结果块内的局部 loopDetected 直接拿到下一圈使用。
- 命中停滞后只安排一次禁用工具的收尾请求，不再注入“换个方法继续调用”的矛盾提示；不依赖 for 循环还有剩余圈数。
- 最后一圈命中停滞时优先记录实际停滞原因。只有未命中更具体原因且圈数耗尽时，使用 iterationCapReached metadata 和 cap 文案。
- 保留现有阈值；补齐 Stream 的连续整轮失败判断，使同一 turn 故障在两入口有相同终止语义。这是局部行为修复，不要求合并 Handle。
- SDK 执行前校验本次请求的有效工具名集合；模型返回未开放的原生调用也不得执行。mode 过滤、本圈关闭与调用者权限都必须生效，不扩大到修改 Registry 全局注册。
- 收尾时返回工具调用属于协议异常，明确报告未执行，不再次启动工具轮。todo reconciliation 或 XML 修复不能重新开放工具；晚到 steering 沿用现有保存机制，不能丢失。
- 第一批同步关闭 `maybeRecoverToolCalls` 将正文改写成真实 ToolCalls 的行为；第二批完善提示与截断处理。

#### 4.4 一次空回复恢复

- 正常模型响应无 tool 且正文为空时，最多追加一次 tools=nil 的恢复请求；使用明确恢复分支，不用含糊的“i-- 或 continue”描述计数。
- 此请求不消耗工具循环圈数，但计入模型请求数与用量，受原 ctx 的取消和截止时间约束。第二次仍空即明确失败，没有第三次空回复恢复。
- 不插入空 assistant。thinking-only 仅在 provider 能合法重放完整块、签名与 RawAssistant 时保留；不能仅拼一个 thinking 字符串。诊断保留与模型消息重放分开处理。
- 已经处于终止收尾的请求若为空或失败，直接交付明确失败结果，不再叠加空回复或 XML 恢复请求。

### 第二批：模型输出协议边界

#### 4.5 正文 XML 不执行，不把前言误当终稿

- `recoverToolCallsFromContent` 可继续作为解析器；任何解析结果都不得自动进入工具执行列表。
- 区分模型泄漏的工具协议文本和普通文档/代码示例。普通文本中讲解 `<invoke>` 的内容必须保留，不能全局正则删除。
- 对明确的协议泄漏，告知模型“正文中的调用未执行”，最多恢复一次。不能仅凭 residual 非空就成功交卷，例如“我现在帮你检查”不是任务结果。
- 正常执行阶段恢复时允许模型使用当前有效 native tool schema；已关闭工具或终止收尾时不能重新开放。无可确认分类的正文保持普通文本，不执行。
- XML 恢复后的响应仍是 XML 或空正文时，明确失败，不再叠加一次空回复恢复。每 turn 的空回复/XML 格式恢复共用一次机会。
- 不制造 recovered_N tool_use/tool_result，也不伪造执行结果；保留原始诊断信息，用户明确知道哪些动作未执行。

#### 4.6 原生工具调用被截断时整批不执行

- 在 `Response`、`StreamChunk` 中透传标准化停止原因；覆盖 OpenAI Chat/ChatStream/parseSSE 和 Anthropic 对应解析。
- 将 OpenAI 的 length、Anthropic 的 max_tokens 映射为输出长度截断；停止原因未知时不伪装成正常 stop。
- `streamChatToResponseWithOptions`、`streamPartialResponse` 等转换过程不能丢失停止原因。
- 工具执行前检查整批响应：若模型输出长度截断，即使某些参数是合法 JSON，也不执行该批任一工具。
- 对已收到的原生调用 id 补明确“未执行：模型输出截断”的结果，保持历史配对；这与禁止制造 XML recovered_N 调用不同。
- 本轮选择停止工具执行并交付明确截断错误，保留可用的部分正文并标记未完成；不自动原样重试被截断批次，不自动调大 maxTokens。
- 非截断但参数 JSON 非法的调用直接返回参数错误，不通过 `_raw` 宽松转换后执行。合法的其他调用按既有批次策略处理。

### 第三批：Deferred 与完整上下文预算

#### 4.7 Deferred 是未执行，不是成功

- 保留当前 per-round cap；MaxParallelToolCalls=0 仍不限。不增加排队执行器，不在 Stream 新增 cap。
- 结果内部增加可明确识别的未执行状态，不通过英文提示字符串猜测。
- totalToolCalls 只统计实际执行的调用；Deferred 不触发工具执行成功/失败统计或 AfterToolCall 成功语义。协议配对结果及必要的展示事件仍保留。
- allFailedRounds 只依据实际执行结果更新；一轮全是未执行项时既不递增也不清零。Deferred 不重置同名工具失败 streak，不送入重复执行结果检测。
- 提示写清楚：本次调用未执行，应依据已执行结果决定是否仍需要它。删除“下一轮原样重发”，也不禁止合理地用相同参数重试未执行调用。
- 重点测试状态与执行次数，不用字符串匹配代替行为验收。

#### 4.8 每次模型请求前核对完整预算

- 覆盖两个 Handle 的正常请求、todo/steering 后继续、空回复/XML 恢复、停滞收尾和 cap 合成；不能只依赖工具轮末的 maybeCompactMidTurn。
- 估算对象包含最终 system、session、额外提示、tool schema，并预留模型输出预算。复用现有 token 估算和 compact 阈值逻辑，明确这是估算而非 provider 精确计数。
- 调用现有压缩流程前，把不可压缩的 system/schema 开销纳入可用于 session 的预算；压缩后重新组装并再次核对。优先局部调整 compactSession 的参数和调用点，不重写摘要/裁剪算法。
- 压缩后仍超预算，或 system/schema 本身已超预算时，明确返回上下文过大，不反复 compact，也不继续发出已知超限请求。
- 保留现有 context-overflow 单次恢复边界；正常预检不通过不等于可以无限增加强制压缩次数。中途压缩失败应保留错误信息。

## 5. 文件与函数地图

| 文件 | 拟修改位置 | 用途 |
|---|---|---|
| `internal/agent/sdkbridge.go` | buildSDKRegistry、toolAdapter.Call、executeToolsConcurrently、toolCallResult | 统一裁剪、执行集合校验、错误保真、未执行状态、参数拒绝 |
| `internal/agent/tools/registry.go` | Execute | 复用真实执行入口，保持权限与失败语义 |
| `internal/agent/tools/result_cap.go` | clipToolResult | 复用策略、统一单位，覆盖最终错误包装 |
| `internal/agent/loop.go` | 两个 Handle 的响应处理、工具结果处理、终止分支 | 正常交卷、收尾状态、失败分类、Deferred 统计 |
| 同上 | streamChatToResponseWithOptions、streamPartialResponse、streamFinalDeliveryAfterCap、stringStream | 停止原因透传、流错误与空合成交付 |
| 同上 | compactSession、maybeCompactMidTurn、retryAfterOverflow 的参数/调用点 | 完整请求预算与最终合成检查 |
| `internal/agent/tool_recovery.go` | maybeRecoverToolCalls、scrubLeakedToolCallContent 及调用点 | 禁止自动执行、保护普通 XML 示例、有限恢复 |
| `internal/provider/provider.go` | Response、StreamChunk | 标准化停止原因 |
| `internal/provider/openai.go`、`anthropic.go` | Chat、ChatStream、parseSSE | 非流式/流式停止原因映射 |
| `internal/api/openai.go` | streamResponseFromAgent，若现有传递不足 | 只补错误交付，不改路由和渠道能力 |

测试优先扩展现有 `loop_stall_test.go`、`loop_retry_test.go`、`tool_recovery_test.go`、`compaction_test.go` 与 provider 测试。SDK 桥接及 Stream 终稿场景若无合适位置，再新增任务专用测试文件；复用现有 fake Provider，不预先建立新测试框架。

## 6. 验收矩阵

确定性测试用假 Provider 记录请求、工具计数器记录真实副作用；Web/API 核对只做补充。每项都记录通过/失败及证据，未执行不得标通过。

| 场景 | 必须断言 |
|---|---|
| 超长工具成功输出 | 经过真实 SDK 桥接后，进入 session/下一模型请求的输出受限 |
| 超长工具失败输出 | result 和 err.Error() 都超长时仍受限；失败状态与错误来源不丢失 |
| Stream 第一枪纯文本 | ChatStream 次数为 0；客户端得到同一正文；assistant 持久化一次；PostTurn 一次 |
| 下一 turn 重放 | Thinking/RawAssistant/签名保留，provider 消息合法 |
| 同参同结果连续 3 次 | 进入禁工具收尾，不贴 cap metadata；真实工具执行停止 |
| 最后一圈才命中停滞 | 按停滞原因收尾，不误标 max iterations |
| 真正耗尽圈数 | 只此路径标 iterationCapReached；最终合成无工具且先检查预算 |
| 两入口整轮失败/同工具连败 | 达到既有阈值后收尾；Deferred 不干扰计数 |
| 工具关闭后返回 native/XML 调用 | 工具真实执行计数为 0，无写文件等副作用；明确未执行 |
| 首次空回复 | 最多一次禁工具恢复，无非法空 assistant；第二次空即失败 |
| XML 带非空前言 | 不能只交付“我现在检查”；提示未执行，最多恢复一次 |
| XML/空正文混合出现 | 共用一次格式恢复机会，不串联成多次额外采样 |
| 普通 XML 示例 | 原文保留，工具执行次数为 0 |
| length/max_tokens 含多个 native calls | 整批真实执行次数为 0，包括 JSON 完整的调用；已有原生 id 配对完整 |
| 非法 JSON 参数 | 不通过 `_raw` 执行，返回明确参数错误 |
| cap 后部分 Deferred | 每项配对完整；只计实际执行；未执行项不重置失败 streak，不进入重复检测 |
| 最终请求超预算 | system/schema 与输出预留计入预算；先压缩再核对；仍超限则明确失败 |
| 收尾流空/中断 | 错误或兜底正文实际到达消费者，不只落 session，不冒充正常成功 |
| 取消/截止时间 | 不开启新的恢复或合成请求；历史无孤立原生工具调用，已有结果不伪报完成 |
| mode 回归 | agent 仍返回 nil，chatbot 仍为当前 10 个名字，customize 仍为空切片；Plugin/MCP 与权限语义不变 |

人工核对：使用实施时明确记录的本地 Web URL 和 OpenAI API 地址，各验证一个普通回答及一个受控失败场景。记录可见正文、真实调用次数和退出原因。当前尚未指定或运行这些地址，不能声明端到端验收完成。无需打开 dashboard 或验收工厂能力。

## 7. 风险与交付条件

- 去掉 Stream 二次采样改变的是输出分块方式，不应改变回答内容；SDK/API 消费者必须正确处理单块 SSE。
- 关闭 XML 自动执行可能影响依赖该兼容行为的模型；本轮明确暴露协议问题，不以后台执行正文作为兜底。
- 工具结果裁剪可能丢失长输出尾部，保留清晰截断标识及缩小查询的提示，不能将截断结果描述成完整证据。
- 完整请求预算仍是估算，provider 的溢出错误恢复继续保留；不保证预检永不低估。
- 每批完成相关行为测试后再推进；全部矩阵通过并记录实际 Web/API 地址后，才交付“单 turn 修复完成”。未运行线上失败样本时仍不能宣称线上故障已消除。
- 本文档修订不授权提交或部署。代码实施、验收结果与未关闭问题另行汇报，默认工具集收缩继续保持待评审。
