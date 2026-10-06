# yangzec：修原项目清单（现象 → 解决思路）

**范围**：原 FastClaw 里由 yangzec 相关提交/PR 修或补上的项。  
**结构**：**先 Bug，后债**；域内仍按 Agent → Chat/API → Dashboard → 配置 → MCP → 集成 → 工程 排列。

| 分组 | 含义 |
|------|------|
| **Bug** | 行为错了、数据错了、回归、安全/隔离错误、接口返回与事实不符 |
| **债** | 能跑但缺能力、难用、引导不足、半成品集成（原项目「应该好用却不好用」） |

完整 feat/修 对照见 [`yangzec-changes.md`](./yangzec-changes.md)。文末 **不含** 纯新平台能力。

---

## 一、Bug（优先）

### Agent / 网关运行时

#### 工具输出过大 → 502 / 网关崩溃

| | |
|--|--|
| **现象** | 某次 tool/exec 返回超大 dump 时，整轮对话 502，严重时拖垮 gateway。 |
| **解决思路** | 在真实 SDK execute 路径裁剪输出；失败摘要 nil-safe；超限重试按 provider **实际拒绝尺寸** 收缩。 |
| **参考** | [#29](https://github.com/yangzec/fastclaw/pull/29)，`cd4d0ac` |

#### 单轮工具调用死循环

| | |
|--|--|
| **现象** | 同参反复调工具；回复中 XML 形态工具标记被误执行；空回复仍带 tools 继续请求。 |
| **解决思路** | 裁剪输出；wrap-up 拒绝已禁用工具；首包走 `HandleMessageStream`；同参关 tools 收尾；空回复一次 tools-off 重试；scrub 泄漏 XML 且不执行。 |
| **参考** | `9852a35` |

#### exec 失败 streak 误伤无关命令

| | |
|--|--|
| **现象** | `python3` 失败后 `pip3` 等也被 ban；包装命令（如 doctor）误判 streak。 |
| **解决思路** | 按 **命令 stem** 计失败；wrap-up 禁止假 sandbox 叙述。 |
| **参考** | `9af422a` |

#### Session compaction 行为不正确

| | |
|--|--|
| **现象** | compaction 边界/结果不对，长会话仍异常或丢关键上下文。 |
| **解决思路** | 修 compaction 实现与边界（PR #12）。 |
| **参考** | PR #12，`99686a7` |

---

### HTTP API / Chat 流（Dashboard `/api/*`）

#### 知识库上传：单文件 + 中文名被改坏

| | |
|--|--|
| **现象** | `POST /api/agents/{id}/knowledge-files`  practically 只接受一个文件；`产品说明.md` 被 sanitize 成 `----.md`。 |
| **解决思路** | 支持多文件；`sanitizeKnowledgeFilename` 保留可打印 Unicode，只剔路径非法字符。 |
| **参考** | [#41](https://github.com/yangzec/fastclaw/pull/41) |

#### Insert / steer 后 SSE 丢末尾 delta

| | |
|--|--|
| **现象** | `POST /api/chat/stream` 流式进行中打断（Insert/steer）后，UI 少最后几段 `content_delta`。 |
| **解决思路** | cancel provider 时事件改走 **turn ctx**；`POST /api/chat/steer` 与 **cancel 当前 provider 流** 对齐，同 SSE 继续。 |
| **参考** | [#54](https://github.com/yangzec/fastclaw/pull/54)，#33 |

#### 多租户 scoped API 串配置（MCP / plugins / skills）

| | |
|--|--|
| **现象** | `/api/config`、`/api/agents/...` 等路径下 inherit/可见性错误，管理员共享与用户私有 **串台**。 |
| **解决思路** | handler 层 **inherit scope + 租户隔离**；写回与列表按 uid/scope 过滤（`handlers.go`、`handlers_agents.go` 等）。 |
| **参考** | `6d27b80` |

#### Agent / Status API 模型信息不实

| | |
|--|--|
| **现象** | `GET /api/agents` 等列表 model 显示不对；新建 agent **无 model** 却像可用；status 与 env 不一致。 |
| **解决思路** | `effectiveAgentModel`、创建时 **inherit model**；`GET /api/status` 等补 **envProvider** 等如实字段。 |
| **参考** | [#50](https://github.com/yangzec/fastclaw/pull/50) |

---

### Chat 输入 / 展示（含前端，非独立 API）

#### 回复露出裸 Markdown 围栏

| | |
|--|--|
| **现象** | 助手消息里出现不应展示的 ` ``` `。 |
| **解决思路** | 修正 Markdown 渲染/后处理。 |
| **参考** | [#38](https://github.com/yangzec/fastclaw/pull/38)，PR #13 |

#### Open files 角标与面板数量不一致

| | |
|--|--|
| **现象** | Badge 与 Files 面板数量对不上。 |
| **解决思路** | 统一 open-files 计数来源。 |
| **参考** | [#49](https://github.com/yangzec/fastclaw/pull/49) |

#### Customize 多 Tab Save 串写、重叠

| | |
|--|--|
| **现象** | Save 写到错误 tab；close 区域重叠。 |
| **解决思路** | 只写 **当前可见 tab**；修 layout/overlap。 |
| **参考** | [#43](https://github.com/yangzec/fastclaw/pull/43) |

#### 刷新后 MCP/工具像丢失

| | |
|--|--|
| **现象** | 刷新后工具未注册或 UI 显示不可用。 |
| **解决思路** | Refresh 后与 registry 生命周期对齐，保持工具可用。 |
| **参考** | PR #14 |

---

### Agent 配置 / 工具协议

#### 模型把 MCP / 插件 / Provider 关空

| | |
|--|--|
| **现象** | `configure_agent` 等把 `mcpServers`、plugins、`provider.*` 删光（#31），后续能力不可用。 |
| **解决思路** | 工具拒绝时 **门路标**；schema **白名单 key**；prompt **关门纪律**（非 HTTP，但属对外工具 API）。 |
| **参考** | [#54](https://github.com/yangzec/fastclaw/pull/54)，#31 |

#### MCP 保存后卡片不列新工具

| | |
|--|--|
| **现象** | 保存 MCP 后 runtime 已有新工具，卡片/API 聚合仍不显示。 |
| **解决思路** | 注册 `source=mcp`；卡片读 **live registry**。 |
| **参考** | [#36](https://github.com/yangzec/fastclaw/pull/36) |

---

### 集成

#### 飞书待办列表恒空

| | |
|--|--|
| **现象** | 已连 bot 仍永远空列表（task v2 要 user token，bot 仅 tenant token）。 |
| **解决思路** | task v1 列 bot 创建任务 + 按发送者过滤。 |
| **参考** | [#51](https://github.com/yangzec/fastclaw/pull/51) |

---

### 工程 / CI

#### Dashboard build、时区 flaky

| | |
|--|--|
| **现象** | 前端构建失败；测试依赖本机时区。 |
| **解决思路** | 恢复 build；测试 timezone-independent。 |
| **参考** | `b4f1d21` |

#### pnpm 11 frozen 阻断 build

| | |
|--|--|
| **现象** | `make build` 因 frozen install 失败。 |
| **解决思路** | 调整 frozen/安装与 lockfile 策略。 |
| **参考** | `fe16860` |

---

## 二、债（产品债 / 体验缺口）

### Agent / 上下文

#### 长对话撑爆、上下文不可见

| | |
|--|--|
| **现象** | 不知剩余 context；模型 context/max-output 默认混乱。 |
| **解决思路** | Pi 式 compaction（`9c2a603`）+ Composer **上下文条/模型切换**（#39）+ 官方默认（`9676641`）。 |
| **参考** | `9c2a603`，[#39](https://github.com/yangzec/fastclaw/pull/39)，`9676641` |

---

### Chat 输入 / 发送 / 流式

#### 空内容 Send 仍显眼、易误点

| | |
|--|--|
| **现象** | 空 composer 发送 affordance 不清晰。 |
| **解决思路** | **ghost/muted** Send；空状态文案与 Copy 等自解释。 |
| **参考** | `979f133`，`593878e` |

#### 流式中无法「跟进一句」（Enter 被挡）

| | |
|--|--|
| **现象** | 生成中插话改方向，Enter/发送无效。 |
| **解决思路** | **Enter → `POST /api/chat/steer`**；mid-run steering + follow-up 队列（PR #25）；Shift+Enter 仍换行。 |
| **参考** | PR #25，`chat-screen.tsx` |

#### 切换 session 无焦点

| | |
|--|--|
| **现象** | 新建/切换会话后需手动点输入框。 |
| **解决思路** | 自动 **focus** composer。 |
| **参考** | `2a95c61` |

#### 本轮 token 不可见

| | |
|--|--|
| **现象** | 看不到本回合 token 消耗。 |
| **解决思路** | 输入框旁展示 **12.4k → 890**（与上下文条分离）；SSE `done`/usage 对齐。 |
| **参考** | [#54](https://github.com/yangzec/fastclaw/pull/54)，#42 |

---

### Dashboard 交互

#### Admin Chats / MCP Add / Save 无反馈

| | |
|--|--|
| **现象** | 操作是否成功不清楚。 |
| **解决思路** | Open / Add / Save 的 toast、状态与错误文案。 |
| **参考** | [#55](https://github.com/yangzec/fastclaw/pull/55) |

---

### Agent 配置

#### 自改配置无边界

| | |
|--|--|
| **现象** | Agent 可改不应自动变更的配置项。 |
| **解决思路** | **Agent config guard**（PR #37）。 |
| **参考** | PR #37 |

---

### MCP / 插件 / Skills

#### MCP 整页 JSON 难运维

| | |
|--|--|
| **现象** | 主界面以 JSON 为主；与 Cursor 习惯脱节。 |
| **解决思路** | **卡片 UI** + Add 对话框；**Cursor 风格 mcp.json**（`3d8eb08`）。 |
| **参考** | [#36](https://github.com/yangzec/fastclaw/pull/36) |

#### 插件能配不能装

| | |
|--|--|
| **现象** | 无真实 Install/Upload 流程。 |
| **解决思路** | CLI 与 Dashboard 共用 Install/Upload（含 **新增** plugin API，`handlers_plugins`）。 |
| **参考** | [#57](https://github.com/yangzec/fastclaw/pull/57) |

#### inherit 策略不可配（非串台时的体验债）

| | |
|--|--|
| **现象** | 管理员共享 vs 用户私有边界 operator 不可调。 |
| **解决思路** | 可配置 **inherit scope**（与 Bug 节「串台」同一提交，此处强调 **策略可配**）。 |
| **参考** | `6d27b80` |

---

### 上手与首跑

#### 首跑配不完、进不了对话

| | |
|--|--|
| **现象** | 不知先配什么；Overview 不实；agent 无可用 model。 |
| **解决思路** | 单屏 onboard → chat；Settings **Talk/Teach/Connect/Run**；**Adjust this agent**；featured Skills；`/cron` `/channels` 深链。 |
| **参考** | [#50](https://github.com/yangzec/fastclaw/pull/50) |

---

### SSH / 远程（含 HTTP API 体验债）

#### SSH 输出丢、无会话复用

| | |
|--|--|
| **现象** | 每次新连；stdout/stderr 不完整。 |
| **解决思路** | 连接复用 + keepalive + 2h idle；远程 tmux `fastclaw-<alias>`；输出串行化。 |
| **参考** | [#46](https://github.com/yangzec/fastclaw/pull/46) |

#### 错误 SSH 保存后才知连不上

| | |
|--|--|
| **现象** | 坏配置写入地址簿；列表无探测状态。 |
| **解决思路** | 保存前 **probe**（`echo ok`），失败 **400 不入库**；列表 Connected / Failed / Not tested。 |
| **参考** | [#53](https://github.com/yangzec/fastclaw/pull/53) |

#### 主机地址簿与权限缺失

| | |
|--|--|
| **现象** | 主机配置分散；host access 边界不清。 |
| **解决思路** | SSH 主机 CRUD API（PR #28）；SuperAdmin + host access（PR #35）。 |
| **参考** | PR #28，PR #35 |

---

### 集成（飞书 / 企微 / IM）

#### 飞书待办/文档读改与确认流

| | |
|--|--|
| **现象** | 待办/云文档不稳定可读可改；误改无闸门。 |
| **解决思路** | official list/get/update；文档 append/retitle；**preview + confirm_token**。 |
| **参考** | [#51](https://github.com/yangzec/fastclaw/pull/51) |

#### 企微 calendar/docs 等未接线

| | |
|--|--|
| **现象** | Intelligent Robot 能力缺块。 |
| **解决思路** | intelligent-robot CLI 接 calendar、docs 等。 |
| **参考** | `b476f13`，`a9e5cfa` |

#### IM 发文件 / 群 @ 不完整

| | |
|--|--|
| **现象** | 通道缺附件或 @ 行为。 |
| **解决思路** | R2 发文件、企微群 @（PR #22、#23）。 |
| **参考** | PR #22，PR #23 |

---

### HTTP API（债：补能力，非回归）

#### 上下文条无专用读接口

| | |
|--|--|
| **现象** | 前端无法轻量拉当前 session context 占用。 |
| **解决思路** | 新增 **`GET /api/chat/context`**（agentId + sessionId）。 |
| **参考** | [#39](https://github.com/yangzec/fastclaw/pull/39) `9417cc9` |

---

## 三、不含在本清单（偏真新能力）

| 项 | 说明 |
|----|------|
| WeCom 内置 Channel（#108） | 新通道 |
| Session R2（PR #15） | 新存储 |
| LAN bind（PR #26） | 部署策略 |
| Owner billing（#52） | 新计费 |
| OpenAI **`/v1/*`** 路由 | yangzec 提交几乎未专门改；回合问题多在 agent loop / `/api/chat/stream` |

---

## 四、速查索引

### Bug

| 参考 | 关键词 |
|------|--------|
| #29、`9852a35`、`9af422a` | 502、死循环、exec streak |
| PR #12 | compaction 修 |
| #41 | knowledge-files API、中文名 |
| #54 | steer/SSE delta、关门 |
| `6d27b80` | 租户 API 串台 |
| #50 | agents/status model 不实 |
| #38、#49、#43、PR #14 | UI 回归 |
| #36 | MCP registry 卡片 |
| #51 | 飞书空列表 |
| `b4f1d21`、`fe16860` | build |

### 债

| 参考 | 关键词 |
|------|--------|
| `9c2a603`、#39、`9676641` | 上下文、默认值 |
| `979f133`、`593878e`、PR #25 | 空 Send、Enter steer |
| `2a95c61`、#54 | focus、token |
| #55、PR #37 | Admin UX、config guard |
| #36、`3d8eb08`、#57 | MCP UX、插件安装 |
| #50 | 零培训 |
| #46、#53、PR #28/#35 | SSH |
| #51、`b476f13`、PR #22/#23 | 飞书读写、企微、IM |
| `9417cc9` | GET /api/chat/context |

---

*归纳自 `yangzec/fastclaw` git 历史。*
