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

1. **现在做 chatter 接入吗？**  
   A) 先只合并本文，应用用 `params` + basic-memory，模板关 Auto-remember。  
   B) 按 §4 做 API chatter，再验收。

2. **chatter 从哪读？**（若选 B）  
   A) 只认 `params.user_id`  
   B) 只认 header `X-Fastclaw-Chatter`  
   C) 两者都认，header 优先（或 params 优先，请写明）

3. **basic-memory**  
   默认：**HTTP/Cloud + `params`，模板 Agent 不装 MCP**。  
   若你坚持模板上要 MCP，需要另开「按用户 project」设计，不能当小补丁。

4. **记忆主键**  
   A) 每用户一份（客服/写作共享要点）  
   B) 每用户每 Agent 一份

5. **`params` 提示措辞**  
   A) 不动 FastClaw，靠 SOUL + 字段名  
   B) 改全局 `renderClientParams`（所有 API 调用方都会看到）

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
| 以后 API 接真实 chatter，不切 UserSpace | 是（待 §5） |
