# yangzec：修原项目清单（现象 → 解决思路）

**范围**：原 FastClaw 里 **bug、回归、产品债、集成半成品**——用起来像「坏了 / 缺了 / 不对 / 很难用」，由 yangzec 相关提交/PR 修或补上的项。  
每条只写 **现象** 与 **解决思路**；文末 **不含** 纯新平台能力（新通道、新存储、新计费模型等）。

完整 feat/修 对照见 [`yangzec-changes.md`](./yangzec-changes.md)。

**类型标签**：`bug` 行为错误 · `债` 能跑但缺能力或体验 · `混合` 补能力同时修实现

---

## Agent / 网关运行时

### 工具输出过大 → 502 / 网关崩溃 `bug`

| | |
|--|--|
| **现象** | 某次 tool/exec 返回超大 dump 时，整轮对话 502，严重时拖垮 gateway。 |
| **解决思路** | 在真实 SDK execute 路径裁剪输出；失败摘要 nil-safe；超限重试按 provider **实际拒绝尺寸** 收缩。 |
| **参考** | [#29](https://github.com/yangzec/fastclaw/pull/29)，`cd4d0ac` |

### 单轮工具调用死循环 `bug`

| | |
|--|--|
| **现象** | 同参反复调工具；回复中 XML 形态工具标记被误执行；空回复仍带 tools 继续请求。 |
| **解决思路** | 裁剪输出；wrap-up 拒绝已禁用工具；首包走 `HandleMessageStream`；同参关 tools 收尾；空回复一次 tools-off 重试；scrub 泄漏 XML 且不执行。 |
| **参考** | `9852a35` |

### exec 失败 streak 误伤无关命令 `bug`

| | |
|--|--|
| **现象** | `python3` 失败后 `pip3` 等也被 ban；包装命令（如 doctor）误判 streak。 |
| **解决思路** | 按 **命令 stem** 计失败；wrap-up 禁止假 sandbox 叙述。 |
| **参考** | `9af422a` |

### Session compaction 不正确 `bug`

| | |
|--|--|
| **现象** | 长会话 compaction 边界/结果不对，与预期（Pi 式）不一致。 |
| **解决思路** | 修 compaction 实现与边界（PR #12）；必要时引入/对齐 Pi 式机制（`9c2a603`）解决 **撑爆上下文** 的根因。 |
| **参考** | PR #12，`99686a7`，`9c2a603` |

### 长对话上下文撑爆、无可视化 `债`

| | |
|--|--|
| **现象** | operator 不知道还剩多少 context；模型 context/max-output 默认值混乱，易配错。 |
| **解决思路** | Composer **上下文条 + 模型切换**；Models/Onboard 套用 **官方推荐 context/输出上限**（#39、`9676641`）。 |
| **参考** | [#39](https://github.com/yangzec/fastclaw/pull/39)，`9676641` |

---

## Chat 输入 / 发送 / 流式交互

### 空内容仍可点发送、反馈弱 `债`

| | |
|--|--|
| **现象** | 输入为空时 Send 仍显眼、易误点；空状态与发送 affordance 不清晰。 |
| **解决思路** | 不可发送时用 **ghost/muted** 样式；空 composer **mute send**；相关空状态文案与 Copy 等操作自解释（`979f133`、`593878e`）。 |
| **参考** | `979f133`，`593878e`（yangzec Co-authored） |

### 流式生成中无法「跟进一句」`债`

| | |
|--|--|
| **现象** | 模型还在流式回复时，用户想插话改方向，Enter/发送 **无效或被挡**，与 Cursor/Codex 式体验不一致。 |
| **解决思路** | **Enter（无 Shift）在 sending 时走 steer**：`handleSteer` 注入当前回合；配合 mid-run steering 与 follow-up 队列（PR #25）。规则仍为 **Shift+Enter 换行**。 |
| **参考** | PR #25 `4fff4ea`，`chat-screen.tsx` `handleKeyDown` |

### Insert / 打断流后丢已输出文字 `bug`

| | |
|--|--|
| **现象** | 打断 in-flight 流后，UI 少最后几段 streaming 内容。 |
| **解决思路** | `content_delta` 在 cancel 时改走 **turn ctx**，保留半截回复再同回合掉头。 |
| **参考** | [#54](https://github.com/yangzec/fastclaw/pull/54)，#33 |

### 切换 session 还要手动点输入框 `债`

| | |
|--|--|
| **现象** | 新建或切换会话后 composer 无焦点，多一步操作。 |
| **解决思路** | 新 session / 切换时 **自动 focus** textarea。 |
| **参考** | `2a95c61` |

### 回合 token 不可见 `债`

| | |
|--|--|
| **现象** | 用户看不到 **本轮** 消耗多少 token（与长期上下文占用混淆）。 |
| **解决思路** | 回合结束在输入框旁展示 **12.4k → 890** 等，与上下文条分离。 |
| **参考** | [#54](https://github.com/yangzec/fastclaw/pull/54)，#42 |

---

## Chat / Dashboard 展示

### 回复露出裸 Markdown 围栏 `bug`

| | |
|--|--|
| **现象** | 助手消息里出现不应展示的 ` ``` `。 |
| **解决思路** | 修正 Markdown 渲染/后处理。 |
| **参考** | [#38](https://github.com/yangzec/fastclaw/pull/38)，PR #13 |

### Open files 角标与面板数量不一致 `bug`

| | |
|--|--|
| **现象** | Badge 与 Files 面板列表数量对不上。 |
| **解决思路** | 统一 open-files 计数来源。 |
| **参考** | [#49](https://github.com/yangzec/fastclaw/pull/49) |

### Customize 多 Tab Save 串写、重叠 `bug`

| | |
|--|--|
| **现象** | Save 写到错误 tab；close 区域重叠。 |
| **解决思路** | 只写 **当前可见 tab**；修 layout/overlap。 |
| **参考** | [#43](https://github.com/yangzec/fastclaw/pull/43) |

### 刷新后 MCP/工具像丢失 `bug`

| | |
|--|--|
| **现象** | 刷新页面后工具未注册或 UI 显示不可用。 |
| **解决思路** | Refresh 后与 registry 生命周期对齐，**保持工具可用**。 |
| **参考** | PR #14 |

### Admin Chats / MCP Add / Save 无反馈 `债`

| | |
|--|--|
| **现象** | Open、添加 MCP、保存后是否成功不清楚。 |
| **解决思路** | 补 toast/状态/错误等 **Save/Open/Add 反馈**。 |
| **参考** | [#55](https://github.com/yangzec/fastclaw/pull/55) |

---

## Agent 配置 / 模型行为

### 模型把 MCP / 插件 / Provider 关空 `bug`

| | |
|--|--|
| **现象** | `configure_agent` 等把 `mcpServers`、plugins、`provider.*` 删光（#31），后续能力不可用；仅靠 prompt 拦不住。 |
| **解决思路** | 拒绝时 **门路标** 指出被关项；禁 retry/`--help`/读源码；工具 schema **白名单 key**；prompt **一条关门纪律**。 |
| **参考** | [#54](https://github.com/yangzec/fastclaw/pull/54)，#31 |

### Agent 自改配置无边界 `债`

| | |
|--|--|
| **现象** | 可改到不应自动变更的配置项。 |
| **解决思路** | **Agent config guard** 限制可写字段与校验。 |
| **参考** | PR #37 |

---

## MCP / 插件 / Skills（原项目难用或半残）

### MCP 保存后卡片不列新工具 `bug`

| | |
|--|--|
| **现象** | 保存 MCP 后消息已有新工具，卡片仍不显示。 |
| **解决思路** | 工具 `source=mcp`；卡片读 **live registry**。 |
| **参考** | [#36](https://github.com/yangzec/fastclaw/pull/36) |

### MCP 只能整页啃 JSON、难运维 `债`

| | |
|--|--|
| **现象** | 主界面以 JSON 为主，Add/Save 流程不直观；与 Cursor 用户习惯脱节。 |
| **解决思路** | **卡片列表** + Add 对话框粘贴 JSON；支持 **Cursor 风格 mcp.json** 导入。 |
| **参考** | [#36](https://github.com/yangzec/fastclaw/pull/36)，`3d8eb08` |

### 插件「能配不能装」`债`

| | |
|--|--|
| **现象** | Dashboard/CLI 缺少真实 Install、Upload 流程。 |
| **解决思路** | **统一 Install/Upload UX**（CLI 与 Dashboard 共用）。 |
| **参考** | [#57](https://github.com/yangzec/fastclaw/pull/57) |

### 多用户下 MCP/插件/Skills 继承混乱 `债`

| | |
|--|--|
| **现象** | 管理员共享与用户私有边界不清，易串配置或看不到 inherited。 |
| **解决思路** | **可配置 inherit scope** + **租户隔离**。 |
| **参考** | `6d27b80` |

---

## 上手与首跑（原项目「不会用」）

### 首跑配不完、进不了对话 `债`

| | |
|--|--|
| **现象** | 新装后不知先配什么；Overview 与真实状态不符；agent 没有可用 model。 |
| **解决思路** | **单屏 onboard**（粘贴 key → 进最老 agent chat）；Overview 如实；新建/已有 agent **继承可用模型**；Settings 分 **Talk/Teach/Connect/Run**；空 chat **Adjust this agent**；featured Skills；`/cron` `/channels` 指向当前 agent。 |
| **参考** | [#50](https://github.com/yangzec/fastclaw/pull/50) |

---

## SSH / 远程执行

### 每次新连、stdout/stderr 丢输出 `债`

| | |
|--|--|
| **现象** | SSH exec 输出不完整；无长会话复用。 |
| **解决思路** | 连接复用 + keepalive + **2h idle**；远程 **tmux** `fastclaw-<alias>`；stdout/stderr **串行化**。 |
| **参考** | [#46](https://github.com/yangzec/fastclaw/pull/46) |

### 错误 SSH 配进地址簿才发现 `债`

| | |
|--|--|
| **现象** | 保存后才连不上；列表无探测状态。 |
| **解决思路** | 创建/变更时 **probe**（`echo ok`），失败 **400 不入库**；列表 **Connected / Failed / Not tested**。 |
| **参考** | [#53](https://github.com/yangzec/fastclaw/pull/53) |

### 主机记录与权限 `债`

| | |
|--|--|
| **现象** | 主机配置分散；平台/用户主机访问边界不清。 |
| **解决思路** | **SSH 主机地址簿**（PR #28）；**SuperAdmin + host access**（PR #35）。 |
| **参考** | PR #28，PR #35 |

---

## 集成（飞书 / 企微 / IM）

### 飞书待办列表恒空 `bug`

| | |
|--|--|
| **现象** | 已连 bot 仍 **永远空列表**（task v2 要 user token，bot 只有 tenant token）。 |
| **解决思路** | task v1 列 bot 创建任务 + 按发送者过滤。 |
| **参考** | [#51](https://github.com/yangzec/fastclaw/pull/51) |

### 飞书待办/文档「接了但不能用」`债`

| | |
|--|--|
| **现象** | 待办/云文档无法稳定读改；变更无确认易误操作。 |
| **解决思路** | official list/get/update；文档 append/retitle；变更 **preview + confirm_token**。 |
| **参考** | [#51](https://github.com/yangzec/fastclaw/pull/51) |

### 企微日历/文档等能力缺失 `债`

| | |
|--|--|
| **现象** | Intelligent Robot 侧 calendar/docs 等未接线。 |
| **解决思路** | 用 **intelligent-robot CLI** 接 calendar、docs 并补齐剩余 CLI 能力。 |
| **参考** | `b476f13`，`a9e5cfa` |

### IM 发文件 / 群 @ 不可用 `债`

| | |
|--|--|
| **现象** | 通道侧缺文件发送或群 @ 行为不完整。 |
| **解决思路** | **R2 文件发送**、**企微群 @** 等通道补齐（PR #22、#23）。 |
| **参考** | PR #22，PR #23 |

---

## 知识库 / 数据

### 多文件与中文文件名 `bug`

| | |
|--|--|
| **现象** | 上传受限；中文文件名乱码或丢失。 |
| **解决思路** | 多文件上传；路径 **保留中文名**。 |
| **参考** | [#41](https://github.com/yangzec/fastclaw/pull/41) |

---

## 工程 / CI

### Dashboard build、时区 flaky `bug`

| | |
|--|--|
| **现象** | 前端构建失败；测试依赖本机时区。 |
| **解决思路** | 恢复 build；测试 **timezone-independent**。 |
| **参考** | `b4f1d21` |

### pnpm 11 frozen 阻断 build `bug`

| | |
|--|--|
| **现象** | `make build` 因 frozen install 失败。 |
| **解决思路** | 调整 frozen/安装与 lockfile 策略。 |
| **参考** | `fe16860` |

---

## 不含在本清单（偏真新能力）

以下多为 **扩展产品线/架构**，不是「原项目坏了」同一类问题，故不列入上文：

| 项 | 说明 |
|----|------|
| WeCom **内置 Channel** 长连（#108） | 全新通道形态 |
| Session **R2** 存储（PR #15） | 新持久化方案 |
| **LAN bind** 默认策略（PR #26） | 部署策略选择 |
| **Owner billing**（#52） | 计费归属新产品逻辑 |
| **Codex follow-up 队列**（PR #25） | 与 steer 配套的新队列语义（steer/Enter 修债仍见上文） |

---

## 速查索引

| 参考 | 关键词 |
|------|--------|
| #29、`9852a35`、`9af422a` | 502、死循环、exec streak |
| PR #12、`9c2a603`、#39、`9676641` | compaction、上下文条、模型默认 |
| `979f133`、`593878e`、PR #25、#54 | 空 Send、Enter steer、Insert 丢字、token |
| `2a95c61` | focus composer |
| #38、#49、#43、PR #14、#55 | Markdown、Open files、Customize、刷新、Admin UX |
| #54、PR #37 | 关门、config guard |
| #36、`3d8eb08`、#57、`6d27b80` | MCP、插件、租户 |
| #50 | 零培训首跑 |
| #46、#53、PR #28、#35 | SSH |
| #51、`b476f13`、PR #22/#23 | 飞书、企微、IM |
| #41 | 知识库 |
| `b4f1d21`、`fe16860` | build |

---

*归纳自 `yangzec/fastclaw` git 历史；类型标签便于检索，边界见「不含在本清单」。*
