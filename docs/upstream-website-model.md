# 网站接入 FastClaw：已对齐模型与待办

记录日期：2026-09-05。  
对话来源：cloud agent 与产品方对齐（模板 Agent、账单、会话、记忆、同步、basic-memory）。  
API 合同：`docs/upstream-api.md`。集成短技能：`skills/fastclaw-api-integration/SKILL.md`。  
相关实现：owner 账单桶见 PR #44（`cursor/upstream-template-usage-e336`）。本文不代替该 PR 的代码说明。

本文是产品/集成决策，不是「一人一个 Agent」方案。后续改 API 或验收，先回到这里的开放问题。

---

## 1. 两个当场问题

### 1.1 `params` 是不是让应用自己附记忆？

分两层，不要混：

| 每轮 `params` 里放什么 | 谁负责 | 是不是「记忆特征」 |
|---|---|---|
| `user_id` / `display_name` / `locale` / 套餐 | 应用后端 | 身份，不是记忆。每轮都带，因为 `params` **不落库** |
| 从你们库或 basic-memory 检出的短事实 | 应用后端 | **是。** 跨会话记忆由应用检索后附上 |
| 这一通在聊什么 | FastClaw session | **不是。** 复用 `X-Fastclaw-Session-Key`，不要把聊天记录塞进 `params` |

所以：`params` 不是 FastClaw 的记忆引擎。它是当轮提示里的一块 JSON。  
身份必带。跨会话要点：应用查完再带。会话内上下文：交给 session。

`params` 在模型侧被渲染成 “client parameters, forward to tools”，不是 “这些是已核实的长期记忆”。字段请写成短陈述（如 `known_facts`），必要时在该 Agent 的 SOUL.md 写明「这些是事实」。不要假设一塞进去就稳定「记住了」。

### 1.2 选了 `params` 注入，还能在模板 Agent 上挂 basic-memory MCP 吗？

**同一条 Agent 上不要两套一起用。** 不是提示词能挡掉的。

原因：

- FastClaw 的 MCP 是 **每个 Agent 一份进程**。`CallTool` 不带 `chatter_user_id`。
- basic-memory 的隔离单位是 **project/目录**，进程启动时就锁死（`--project` / `BASIC_MEMORY_MCP_PROJECT`）。
- 模板 Agent 全站共用。挂一份 MCP = 全站一个库。模型再 `write_note`，会和你们按 `user_id` 写入的 HTTP/Cloud **双写**，并可能 **串到别人的笔记**。

「无法避免吗？」

- **要避免串库/双写：避免的办法是二选一，不是同时装。** 推荐：应用按 `user_id` 调 basic-memory HTTP/Cloud，短结果进 `params`；模板 Agent **不装** 这个 MCP。
- **想让模型自己 `write_note`：** 必须先有按用户的 project（每用户一个 MCP 进程，或每次调用带 project）。FastClaw 现在没有这条能力。只改 `api-user` → 真实 chatter **不够**。
- SOUL 里写「不许 write_note」不可靠，工具还在目录里。

basic-memory 故障时：带空记忆继续聊，不要挡住 `/v1/chat/completions`。写回放在回合后，自行去重。凭证只放应用服务端。

---

## 2. 已对齐的模型

### 2.1 账号与 Agent

- 一个网站 = 一个 FastClaw 账号 + 一把 **服务端** API key。
- Agent 是产品能力（客服、写作），**不是** 一个注册用户一个 Agent。
- UserSpace 会把该账号名下 Agent 都装进内存；一人一个 Agent 会拖垮内存。忙站的 30 分钟空闲回收帮不上。

### 2.2 身份（三层，不要用错）

| 层 | 是什么 | 怎么传 | 不要用 |
|---|---|---|---|
| Owner | 网站 FastClaw 账号 | API key | — |
| Session | 哪个人的哪一通 | `X-Fastclaw-Session-Key: <app>:<user-id>:<conversation-id>` | 跨人复用同一条 key |
| Chatter | 记忆/技能目录里的「这个人」 | 今天 API **写死** `api-user` | body `user` / `X-Fastclaw-End-User`（那是切 UserSpace） |
| 当轮说话人（给模型看） | 名字、语言、套餐、短记忆 | `params`，每轮带 | 指望 `params` 自己持久化 |

`user` / End-User 会铸 `app_user` 并切换 UserSpace。会话行、历史、曾经的用量桶都会跑偏。默认路径 **不要发**。

`(agent_id, chatter_user_id)` 是有意设计（对齐 IM：同一 Agent 上张三李四各有 USER.md/MEMORY.md）。  
API 写死 `"api-user"` 是 2026-03 给 ChatClaw 单调用方的 **占位**，不是「全站共用记忆」的产品决定。后来官方补的是重路径 End-User，**没有** 把轻量 chatter 接到 API。即使发了 End-User，`msg.UserID` 仍是 `api-user`。

### 2.3 聊天请求

`POST /v1/chat/completions` **长得像 OpenAI，语义是 IM**：只取 **最后一条** `role=user`，前面的 `messages` 丢掉。历史、系统提示、工具、压缩、配额由 FastClaw 拼。没有纯透传模式。

- 只发本轮用户文本。
- 同一通复用 session key；新对话换 `conversation-id`。
- 不要回放全量 OpenAI history（会和 session 叠两份，且叠不上，因为前面的消息被丢了）。

### 2.4 上下文会不会爆

同一会话会变长，但工作集会自动压：摘要 + 约 2 万 token 热尾；工具输出入库截 64KiB；溢出再强制压。阈值按模型 `contextWindow` 算。  
档案表 `session_messages`（含 `seq`）压缩不动；控制台 history 读档案。  
应用不必再做一套压缩。换新会话是产品（新话题/清空/控成本），不是防爆窗。

### 2.5 跨会话记忆（在改 chatter 之前）

- 模板 Agent：**Auto-remember chatter 保持关**（默认就是关）。
- FastClaw `USER.md` / `MEMORY.md` 不要当网站用户记忆（键在 `api-user`）。
- 跨会话：应用库或 basic-memory HTTP/Cloud，按 `user_id`（可选再加 `agent_id`）检索，短列表进 `params`。
- 模型仍可能 `write_file USER.md`（关 Auto-remember 只停 5 轮蒸馏）。SOUL 写明：跨会话只信 `params`，不要写这两份文件。

### 2.6 账单与配额

- `GET /v1/usage`、`/v1/quota` = API key **owner** 桶（#44）。
- 按 Agent：`daily[].agentId`。
- 按注册用户：应用按 session-key 前缀自己加。
- 完成响应里的 `usage` 仍是 0，不要用来计费。

### 2.7 聊天记录同步

FastClaw **不能** 当应用的多端同步云。

- 有：按 session 整包拉 `GET /api/chat/history`（控制台口，无消息 id、无 `since`、无分页）。
- 无：增量游标、发送幂等、只存不跑模型、按注册用户隔离的列表。
- completions **不能** 当同步写入：超时重试 = 再跑一轮 Agent。

应用自己管会话列表和用户可见气泡（`client_msg_id` + 游标）。FastClaw history 只做整段对账/备份。owner key 的 `GET /api/chats` 能看到该 Agent 下所有会话，不要暴露给终端用户。

文件列表必须带 `?sessionId=`。省略则 owner key 可列全 Agent 文件。

---

## 3. 推荐的应用流水线

```text
用户发消息
  → 应用库先落 user 气泡（client_msg_id）
  → 按 user_id 查 basic-memory HTTP/Cloud（可空、可超时降级）
  → POST /v1/chat/completions
       agent_id
       X-Fastclaw-Session-Key: app:user:conversation
       messages = 本轮一句
       params = 身份 + 短 known_facts
  → SSE 写入应用库的 assistant 气泡
  → 回合结束后按需把新要点写回 basic-memory（去重）
```

模板 Agent 不装 basic-memory MCP。Auto-remember 关。

---

## 4. FastClaw 打算怎么改（尚未开工）

在开放问题拍板之前 **不改代码**。倾向只做轻量 chatter，不碰 UserSpace。

### 4.1 拟议改动（待你选接线）

`POST /v1/chat/completions` 填 `InboundMessage.UserID`：

1. 若请求带来合法 chatter（`params.user_id` 和/或 `X-Fastclaw-Chatter`，以拍板为准）→ 用它。
2. 否则回落 `"api-user"`（旧客户端不变）。
3. **绝不** 用 body `user` / `X-Fastclaw-End-User` 当 chatter（那条仍是切 UserSpace）。
4. 不改 `ownerUserID`、账单、配额、session PK。
5. 必须由 **应用后端** 带。文档写明：不要让浏览器直填（可冒充别人写 USER.md）。

效果：FastClaw 自带 `USER.md` / `MEMORY.md` / Auto-remember / 每用户技能目录按注册用户分开。  
**仍然不会** 让共享 MCP 按用户分 project。basic-memory 继续走应用 HTTP，或等以后单独做「按 chatter 起 MCP」。

可选、默认不做：改 `renderClientParams` 的措辞（影响所有 API 客户端）。更稳的是各 Agent 自己写 SOUL。

### 4.2 不在本轮做的

- 纯透传 LLM 代理。
- `/v1` 增量同步协议（消息 id / `since` / 幂等 key）。
- 按 chatter 动态起 basic-memory MCP。
- 一人一个 Agent 的懒加载（已否决该产品模型）。

### 4.3 拟议验收（chatter 落地后）

同一模板 Agent、同一 owner API key，两个 `user_id`（或 header）：

1. A、B 各说一句可写入记忆的话；开 Auto-remember 或让模型写 USER.md。
2. 新 session key 再问「我是谁」：A 只看到 A，B 只看到 B。
3. 不传 chatter：行为与现在相同（`api-user`）。
4. `GET /v1/usage` 的 `userId` 仍是 owner；配额仍打 owner。
5. 发 `X-Fastclaw-End-User` 仍走旧的 UserSpace 切换（默认集成测试 **不发**）。
6. 回归：只发本轮 messages、session 历史连续、文件仍按 session key 隔离。

单测：`UserID` 解析（params / header / 回落 / 不误用 `user` 字段）。  
现有 `go test ./internal/auth ./internal/api` 保持绿。

---

## 5. 需要你先拍板的

改代码前请定这几条（回复编号即可）：

1. **现在做 chatter 接入吗？** — **已做（B）**，已合入 `yangzec`（PR #48）。

2. **chatter 从哪读？** — **已做：C + 不一致 400**。`X-Fastclaw-Chatter` 与 `params.user_id`；只带一个用那个；两个不同或非字符串 → 400。

3. **basic-memory** — **已收口**，见 §8.3。应用 HTTP find-or-create + `params`；模板不装 MCP。

4. **记忆主键** — **已选 A**：每个产品注册用户一份（客服/写作共享要点）。区分方式见 §7.4。

5. **`params` 提示措辞** — **已选 A**：不动 FastClaw 全局文案，靠该 Agent 的 SOUL + 字段名。

---

## 6. 对照（以后不要翻案除非重开）

| 做法 | 结论 |
|---|---|
| 一人一个 FastClaw Agent | 否 |
| End-User 当默认隔离 | 否 |
| FastClaw 当多端聊天源 | 否 |
| FastClaw 当 LLM 纯中转 | 否 |
| 模板开 Auto-remember（未接 chatter 前） | 否 |
| 模板挂 basic-memory MCP + `params` 双写 | 否 |
| 跨会话记忆在应用 / BM HTTP，短结果进 `params` | 是 |
| 会话内记忆 = 同一条 session key | 是 |
| 账单在 owner，按人由应用汇总 | 是 |
| 以后 API 接真实 chatter，不切 UserSpace | **已合入** `X-Fastclaw-Chatter` / `params.user_id` |

---

## 7. 追问（2026-09-05）

### 7.1 改 API chatter 后，网页端会不会串用户？

这里有两个「网页」，不要混：

| 谁在聊 | 走哪条口 | 今天的 chatter | 改 API 之后 |
|---|---|---|---|
| 你们产品里的注册用户 | `/v1/chat/completions` | 全员 `"api-user"`（**会串** FastClaw 自带记忆） | 用后端传来的 id，A/B 分开 |
| FastClaw 控制台（你们自己登录试 Agent） | `/api/chat/*` | 登录者的 FastClaw 账号 id；没登录是 `"web-user"` | **不动** |
| 微信/飞书等 | 各通道 | 平台发送者 id | **不动** |

改的只是 API 这一条：`msg.UserID` 从占位 `"api-user"` 换成应用后端给的稳定 id。  
控制台登录用户之间本来就不会串（各用自己的 FastClaw 账号 id）。  
产品用户之间：**今天** 若开 Auto-remember / 写 USER.md 会串；接上 chatter 且 id 互不相同才不串。

会话（聊了什么）一直靠 session key，和 chatter 无关。两个人不要共用一条 session key。

控制台里运营者试模板、和产品用户走 API，记忆默认不共享（一边是 FastClaw 账号 id，一边是 `app:u_123`）。这是对的。  
为防撞号，API 传来的 chatter **加前缀**，例如 `app:<你们的user_id>`，不要直接拿裸数字去撞 FastClaw uuid / IM id。

### 7.2 `params.user_id` 和 header 有什么区别？

都是「告诉 FastClaw 这个人的 chatter id」，不是两套记忆。

| | `params.user_id` | `X-Fastclaw-Chatter` |
|---|---|---|
| 放哪 | JSON 身体，和 display_name 一起 | HTTP 头 |
| 模型看不看得到 | 看得到（整份 params 进提示） | 默认看不到 |
| 和 OpenAI `user` | **不是** 同一个字段。`user` 会切 UserSpace | 也不会切 UserSpace |
| 适用 | 应用本来就要带 params | 不想让模型看见内部 id，或和 params 里其它字段分开 |

「两个都认」只是解析顺序（例如 header 优先，没有再用 params）。  
**必须应用后端填，浏览器不能直连 FastClaw 自己填。**

### 7.3 basic-memory 支不支持按用户的 project？

支持 **多 project**（各有目录和索引；Cloud 还有 workspace/租户）。  
**没有** 「你们 SaaS 注册用户」这个一等公民。不会因为 FastClaw 换了 chatter 就自动一人一库。

按用户用它：应用用你们的 `user_id` 当 project 名（或先 `POST /v2/projects` 再检索）。这是应用侧 HTTP，不是挂在共享 Agent 上的那一份 MCP。

MCP 若不定死 `--project`，模型可以自己传 `project=`。理论上能填用户 id，但 `list_memory_projects` 会列出所有人的库，不可靠，也等于把全站项目名暴露给模型。所以模板 Agent 仍不建议装这份 MCP。

### 7.4 「每个用户一份」的用户是谁？

已选：**每个产品注册用户一份**（客服/写作共享要点）。

三拨人，id 空间不同：

| 人群 | chatter / 记忆键 | 今天 |
|---|---|---|
| 产品注册用户（API） | 应用传入的 id，建议 `app:<user_id>` | 实际全是 `api-user` |
| FastClaw 控制台用户 | FastClaw 账号 id | 已按账号分开 |
| IM 发送者 | 默认平台 `u_xxx`；开 Shared identity 后 = 通道主人（控制台账号） | 默认和控制台不是一份；要同一份见 §8.4 |

「非 API 的 user」不会自动和「API 传来的 user」合并。同一人既在控制台聊又在产品里聊，是两份记忆，除非你们故意用同一个字符串当 chatter。  
产品用户之间：只看你们是否传入 **互不相同、长期稳定** 的 id（不要用 session id、不要用可被客户端伪造的值）。

### 7.5 「params 提示措辞」是什么意思？

不是改记忆键。是 FastClaw 把 `params` JSON 塞进系统提示时那两句英文：

> The user's client app submitted these parameters… Forward them to whichever tool / skill you call.

模型可能把 `known_facts` 当成「要转给工具的设置」，而不是「已核实的事实」。

- A：FastClaw 这段字不动；在该 Agent 的 SOUL.md 写「`params.known_facts` 是事实」。只影响你们的 Agent。
- B：改 FastClaw 全局这两句。所有走 `/v1` 的调用方提示都变。

已选 A。

### 8. 再追问（接线建议 / BM API / IM 与控制台）

### 8.2 两个都带且不一致：什么时候发生？应否报错？

正常后端应只写一个来源，或两个写成同一个 `app:<user_id>`。会出现不一致的情况都是 **集成 bug**，不是合法业务：

- 网关/中间层改了 header，旧代码还在 `params` 里留着另一个 id
- 重试、复制请求、测试夹具只改了一边
- 两个服务拼同一请求，各填各的

静默「header 优先」会把这种 bug 吃掉，记忆写到错误的人身上还很难查。  
**应对：两个都缺 → 回落 `api-user`；只带一个 → 用那个；两个都带且不相等 → 400，不要挑一边。**

不要用 body `user` / `X-Fastclaw-End-User` 当 chatter。

### 8.3 basic-memory 最终方案（已收口）

**产品用户的跨会话记忆，不走 FastClaw MCP，不走 FastClaw USER.md。**

```text
应用后端（BM API key 只放服务端）
  → name = "app-" + 你们的 user_id
  → POST /v2/projects/resolve ；没有则 POST /v2/projects 创建
  → 只在这一个 project 里 search（回合前）
  → 短列表写入 params.known_facts
  → POST FastClaw /v1/chat/completions
  → 回合后按需把新要点写回同一个 project（自行去重）
```

模板 Agent：**不装** basic-memory MCP，Auto-remember **关**。  
不改 BM 上游，FastClaw 也不包一层 BM。find-or-create 就是应用那两步 HTTP。

### 8.3b basic-memory 有没有「按你们 user_id 找或建 project」？我们能改吗？

**BM 没有 SaaS 用户这一等公民。** 它有的是普通 project API：

- `POST /v2/projects/resolve` `{ "identifier": "名字或 uuid" }`
- `POST /v2/projects/` 用 **name** 创建（name 必须唯一）
- 按 uuid get / list

「按 user_id 找到或创建」是 **你们应用自己的两步**，不是 BM 内置 upsert：

```text
name = "app-" + user_id
resolve(name) → 有则用
没有 → create(name=...) → 再用
只在这个 project 里 search / write
```

| 谁改 | 要不要 |
|---|---|
| 去改 basic-memory 上游，加「按外部 user 自动建库」 | 不。那是别人的项目，你们也用不到一等公民 |
| FastClaw 里包一层 BM、按 chatter 建 project | 现在不。又绑死 BM，和「应用 HTTP + params」相反 |
| 应用后端 5 行 find-or-create | **要。** 密钥留服务端，模型不装 MCP |

挂 MCP 才能 list 全站 —— 这条继续关死。

### 8.3c 自己写个 MCP 封装，还是继续用 API？

分清 **谁调用** 这个封装：

| 封装挂在哪 | 行不行 | 说明 |
|---|---|---|
| 应用后端当客户端（stdio/HTTP 调你们的包装，或直接调 BM REST） | 行 | 和「用 API」是同一架构。模型仍只看见 `params`，list 不到全站 |
| 挂到 **共享模板 Agent** 上，让模型 `write_note` | 现在不行 | FastClaw 每个 Agent **一份** MCP 进程，`CallTool` **不传** chatter。包装进程里若能 list/切 project，模型仍可能串库 |

所以：要封装可以，封装给 **你们后端** 用（REST 或自建 MCP 当库都无所谓）。  
**不要** 把包装 MCP 挂上模板 Agent。那不是「用 MCP 就隔离了」，除非以后 FastClaw 每次 CallTool 注入 chatter，且包装 **强制** 只用这一个 project、对模型隐藏 list——那是另开的需求，不是现在的方案。

**继续用 API（应用 HTTP + `params`）。** 自己写 MCP 不增加隔离，只换了调用方式。

### 8.4 「现在不就是 IM 和控制台同一份吗？」

**默认不是。** 控制台 Context 页写得很清楚：Shared identity **默认关**，「each channel gets its own isolated session and memory」。

| | chatter | 会话 |
|---|---|---|
| 控制台网页 | 登录的 FastClaw 账号 id | `web` + 该聊窗 |
| IM（默认） | 新铸的 app_user，external = `wechat:<openid>` 这类 | 微信/飞书自己的 chat id |
| IM（Shared identity **开**） | 改写成通道主人 = 控制台账号 id | 虚拟 `shared` + ownerId，和控制台同一通 |

「IM 只允许控制台用户接」只限制 **谁能绑通道**，不会把微信 openid 自动换成 FastClaw 账号 id。同一人两边聊，默认仍是两份 USER.md、两通会话。控制台侧边栏能看到 IM 会话，容易误以为已经是一份。

要同一份：**不必改 FastClaw 代码**。在该（运营者自己用的）Agent 上打开 **Shared identity across channels**。会话也会并在一起。群聊不要开。  
产品 API 用户仍用 `app:<user_id>`，不要传控制台账号 id。

### 8.4b Hermes 有记忆，FastClaw 要不要改成那样？

记错可以理解：Hermes/OpenClaw 一类产品常把控制台和 IM 收成同一身份。FastClaw **默认故意分开**；同一能力已经做成 **每 Agent 开关**，不是缺失功能。

| | |
|---|---|
| 能改吗 | 能。运营者自己的 Agent 打开 Shared identity 即可。不要改全局默认（会让所有 Agent 的 IM 发送者并进主人） |
| 值得改吗 | **值得开开关，不值得改代码 / 改默认。** IM 只给控制台、且是私聊时，开了就能像 Hermes 一样两边接着聊、同一份 USER.md |
| 影响刚才的决策吗 | **不影响。** 产品 API、`app:<user_id>`、BM HTTP、`params`、owner 账单、不发 End-User、模板关 Auto-remember / 不装 MCP，全部不动。Shared identity 只作用于该 Agent 的 IM ↔ 控制台 |

不要在 **对外模板 Agent** 上开：以后若对公众开放 IM，所有发言者会被写成主人，记忆全串。模板继续关 Shared identity、关 Auto-remember。

**是的：你刚才要的「控制台和 IM 同一份记忆」，现在已经有，开开关即满足，不用改 FastClaw。** 默认关着，所以会误以为没有。运营者 Agent 打开即可；不是全局已开通。
