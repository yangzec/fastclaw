# yangzec：修原有问题（现象 → 解决思路）

仅收录 **已有 bug、回归、或产品本应具备却失效** 的修复（不含纯新功能）。  
总览与「新功能」见 [`yangzec-changes.md`](./yangzec-changes.md)。

---

## Agent / 网关运行时

### 工具输出过大导致 502 / 网关崩溃

| | |
|--|--|
| **现象** | 某次 tool/exec 返回超大 dump 时，整轮对话失败（502），严重时拖垮 gateway。 |
| **解决思路** | 在 **真实 SDK execute 路径** 上裁剪 tool 输出；失败摘要做 nil-safe；上下文超限重试时按 **provider 实际拒绝的尺寸** 收缩，而不是假设 1M 窗口硬塞。 |
| **参考** | [#29](https://github.com/yangzec/fastclaw/pull/29)，`cd4d0ac` |

### 单轮在工具调用上空转（死循环）

| | |
|--|--|
| **现象** | 同一轮内反复调工具：同参数循环、模型回复里泄漏 XML 形态的工具标记并被误执行、或空回复仍继续带 tools 请求。 |
| **解决思路** | 执行路径裁剪输出；wrap-up 阶段 **拒绝已禁用的工具**；从 **第一次 Chat** 走 `HandleMessageStream`，避免第二条仍挂 tools 的请求；同参循环改为 **关 tools 收尾**（不伪造 iteration cap）；空回复 **一次 tools-off 重试**；在 provider 与 agent 层 ** scrub 泄漏 XML**，且 **不执行**。 |
| **参考** | `9852a35` |

### exec 失败 streak 误伤整条命令族

| | |
|--|--|
| **现象** | 例如 `python3` 连续失败两次后，后续无关的 `pip3` 等也被一律禁止 exec；或 `browser-use --doctor` 等包装命令导致 streak 误判。 |
| **解决思路** | 失败 streak 按 **命令 stem**（而非整行/整类 exec）累计；wrap-up 阶段禁止模型 **编造 sandbox 执行叙述** 糊弄用户。 |
| **参考** | `9af422a` |

### Session compaction 行为不正确

| | |
|--|--|
| **现象** | 引入 compaction 后，边界条件或压缩结果与预期（Pi 式行为）不一致，长会话仍异常或丢关键上下文。 |
| **解决思路** | 在已有 compaction 机制上 **修实现与边界**（follow-up PR），与后续新机制 `9c2a603` 区分开：本条是 **修**，不是首引入。 |
| **参考** | PR #12，`99686a7` |

---

## Chat / Dashboard 展示与交互

### 助手回复露出裸 Markdown 围栏

| | |
|--|--|
| **现象** | 用户可见回复里出现不应展示的 ` ``` ` 围栏字符，破坏阅读。 |
| **解决思路** | 修正 chat Markdown 渲染/后处理，避免 fence 泄漏到最终展示层。 |
| **参考** | [#38](https://github.com/yangzec/fastclaw/pull/38)，PR #13 |

### Open files 角标与面板数量不一致

| | |
|--|--|
| **现象** | 顶栏/角标显示的打开文件数与文件面板列表对不上。 |
| **解决思路** | **统一计数来源**（同一套 open-files 状态），badge 与 panel 共用。 |
| **参考** | [#49](https://github.com/yangzec/fastclaw/pull/49) |

### Customize 多 Tab Save 串写与 UI 重叠

| | |
|--|--|
| **现象** | 多个 Customize 标签页同时存在时，Save 可能写到错误 tab；面板 close 区域重叠，误触或状态错乱。 |
| **解决思路** | 持久化时 **只写当前可见 tab**；修复 close/overlap 布局与事件。 |
| **参考** | [#43](https://github.com/yangzec/fastclaw/pull/43) |

### 刷新页面后工具 / MCP「像丢了」

| | |
|--|--|
| **现象** | 浏览器刷新后，已配置的 MCP/工具在 UI 或 runtime 上表现为不可用或未注册。 |
| **解决思路** | Refresh 后 **保持工具注册与可用状态**（与后端/registry 生命周期对齐）。 |
| **参考** | PR #14 |

### Admin Chats / MCP Add / Save 反馈缺失

| | |
|--|--|
| **现象** | 管理端打开会话、添加 MCP、保存配置时缺少明确成功/失败反馈，操作是否生效不清晰。 |
| **解决思路** | 补 **Open、Add、Save** 的可见反馈与流程 UX（toast/状态/错误文案）。 |
| **参考** | [#55](https://github.com/yangzec/fastclaw/pull/55) |

### MCP 保存后卡片不显示新挂载工具

| | |
|--|--|
| **现象** | 保存 MCP 配置后，下一条消息其实已有新工具，但 agent 卡片 UI 仍列不出刚附着的 MCP tools。 |
| **解决思路** | 注册工具时标记 **`source=mcp`**；卡片从 **live registry** 读取，避免仍用 builtin 默认导致新工具被隐藏。 |
| **参考** | [#36](https://github.com/yangzec/fastclaw/pull/36)（修复部分） |

---

## Agent 行为 / 配置被模型改坏

### 模型通过 configure_agent 关掉 MCP / 插件 / Provider

| | |
|--|--|
| **现象** | Agent 在失败或绕路时尝试修改配置，把 `mcpServers`、plugins、`provider.*` 等 **关空或删光**（#31 类），后续能力永久不可用；仅靠 prompt 说明拦不住。 |
| **解决思路** | 拒绝修改时在错误信息里 **点明具体被关的「门」**；禁止用 retry、`--help`、读源码等方式绕过；工具描述 **只允许合法 config key**；系统 prompt 用 **一条纪律覆盖整类关门行为**（模型在失败路径也能看到结构化反馈，而非 markdown 小字）。 |
| **参考** | [#54](https://github.com/yangzec/fastclaw/pull/54)（关门部分），关联 #31 |

### Agent 可修改危险或越权配置

| | |
|--|--|
| **现象** | 缺少对 agent 自改配置范围的约束，可能改到不应由 runtime 自动变更的项。 |
| **解决思路** | **Agent config guard**：服务端/工具层限制可写字段与校验。 |
| **参考** | PR #37 |

### Insert 取消流时丢失末尾 streaming 内容

| | |
|--|--|
| **现象** | 用户 Insert 打断进行中的 provider 流时，已生成的最后几段 `content_delta` 到不了 UI，半截回复被截断。 |
| **解决思路** | 取消 LLM ctx 时，delta **改绑定 turn ctx** 继续 emit，保证 steer 后 **同一回合** 仍保留已流式输出的文字。 |
| **参考** | [#54](https://github.com/yangzec/fastclaw/pull/54)（Insert 修复部分），关联 #33 |

---

## 集成 / 数据

### 飞书待办列表始终为空

| | |
|--|--|
| **现象** | `feishu_list_tasks` / 待办列表在已连接 bot 下 **永远空**；官方 task v2 list 需要 user token，而 bot 仅有 tenant token。 |
| **解决思路** | 改用 **task v1** 列出 **由 bot 创建的任务**，再按当前飞书发送者过滤，使列表对对话用户可见。 |
| **参考** | [#51](https://github.com/yangzec/fastclaw/pull/51)（列表修复部分） |

### 知识库多文件与中文文件名

| | |
|--|--|
| **现象** | 知识库上传不支持多文件或中文文件名被改乱码/丢失。 |
| **解决思路** | 支持 **多文件上传**，存储与展示路径 **保留原始中文文件名**。 |
| **参考** | [#41](https://github.com/yangzec/fastclaw/pull/41) |

---

## 工程 / CI

### Dashboard build 失败、测试依赖本机时区

| | |
|--|--|
| **现象** | 前端/dashboard 构建挂；部分测试在非 UTC 环境 flaky 或失败。 |
| **解决思路** | 恢复可构建状态；测试断言与 **时区解耦**（timezone-independent）。 |
| **参考** | `b4f1d21` |

### pnpm 11 frozen install 阻断 make build

| | |
|--|--|
| **现象** | pnpm 11 的 frozen lockfile 策略导致依赖安装失败，`make build` 无法完成。 |
| **解决思路** | 调整 **frozen / 安装策略** 与 lockfile 流程，使 CI 与本地 build 一致通过。 |
| **参考** | `fe16860` |

---

## 索引（仅 fix 类 commit / PR）

| 参考 | 关键词 |
|------|--------|
| `cd4d0ac` / #29 | 大 tool dump、502 |
| `9852a35` | 工具空转、XML 泄漏 |
| `9af422a` | exec streak、command stem |
| PR #12 | compaction 修复 |
| #38、PR #13 | Markdown 围栏 |
| #49 | Open files 计数 |
| #43 | Customize Save |
| PR #14 | 刷新丢工具 |
| #55 | Admin/MCP UX |
| #36（部分） | MCP 卡片列工具 |
| #54（关门、Insert delta） | 关门、流式截断 |
| PR #37 | config guard |
| #51（列表） | 飞书空列表 |
| #41 | 知识库文件名 |
| `b4f1d21`、`fe16860` | build / pnpm |

---

*归纳自 `yangzec/fastclaw` git 历史。*
