# FastClaw 知识库与外部 RAG 选型

2026-09-05 讨论纪要。记录 Knowledge 本身能做什么、和记忆/CRM 怎么分层，以及 SiliconFlow、火山、Dify、OpenViking 该不该接。价格会变，下单前以各控制台为准。

**结论先看这里：** 短文本继续用自带 Knowledge。语义检索先不要为 FastClaw 单独上 Dify / 方舟知识库 / Mem0。聊天模型可以走 SiliconFlow（Custom Provider）；向量化现成接不上。已经有外部知识库时，用 MCP 检索，不要再上传一份到 FastClaw。

## 1. 现有 Knowledge 是什么

主人策展的参考材料，不是企业知识管理系统，也不是向量库。

| 项 | 现状 |
|---|---|
| 入口 | Agent → Knowledge 页；`POST /api/agents/{id}/knowledge-files` |
| 格式 | `.md` / `.txt` / `.csv` / `.json` / `.yaml` / `.yml` / `.log` |
| 单文件上限 | 256KB；必须是合法 UTF-8，拒绝 PDF/Word |
| 存储 | `agent_files` 的 `knowledge/` 前缀；另切 `agent_knowledge_chunks` |
| 小库（≤24k rune） | 全文注入 prompt，引用 `[K1]` |
| 大库 | 注入目录 + `knowledge_search`（关键词 + CJK 二元组，**无向量**） |

代码：`internal/setup/handlers_knowledge.go`、`internal/agent/knowledge.go`、`internal/agent/tools/knowledge_search.go`。

2026-09 已修：

- 一次可选/拖多个文件，逐个上传，失败项留在列表。
- 文件名保留可打印 Unicode（中日韩），只去掉路径和控制字符。

## 2. 三层不要混

| 层 | 装什么 | FastClaw 落点 |
|---|---|---|
| 规范知识 | 制度、手册、FAQ | Knowledge，或外挂 RAG |
| 业务系统 | 客户/订单，按账号查 | MCP / 工具，禁止整库进 RAG |
| 情节记忆 | 聊过的偏好、事实 | `MEMORY.md`、会话；可选 `plugins/mem0` |

同一批文档不要同时进 FastClaw Knowledge、方舟知识库、OpenViking resources。

## 3. SiliconFlow

**聊天：能间接对接。** Models 选 Custom：

- API Base：`https://api.siliconflow.cn/v1`
- API Type：`openai-chat`，Bearer Token
- 模型必须是对话模型（如 `deepseek-ai/DeepSeek-V3`）
- 默认模型：`供应商名/模型ID`，只在第一个 `/` 切开

没有 SiliconFlow 预设。`BAAI/bge-m3` 不能当聊天模型。

**Embedding：接不上。** 代码不调用 `/v1/embeddings`。配了 Provider，Knowledge 不会变成语义检索。

自己 embed 的完整链是：切片 → SiliconFlow → 向量库 → 检索 → 再喂 Agent。embed 本身：

| 模型 ID | 用途 | 钱（2026-08 价格页快照） |
|---|---|---|
| `BAAI/bge-m3` | 向量，1024 维 | 免费（限额锁死） |
| `Pro/BAAI/bge-m3` | 同上 | 约 ¥0.07 / 百万 token |
| `BAAI/bge-reranker-v2-m3` | 重排 | 免费 / Pro 收费 |

`POST https://api.siliconflow.cn/v1/embeddings`，OpenAI 兼容。解析、建库、ACL 都要自己做。

## 4. 火山：别把三样当一个

### 方舟知识库（VikingDB 知识库，文档 84313）

小时租计算 + token 另算。[计费](https://www.volcengine.com/docs/84313/1414457)

| | 标准版 | 旗舰版 |
|---|---|---|
| 计算 | ¥0.0416 / 库 / 小时 ≈ ¥30 / 月 | ¥0.45 / CU / 小时（约 1 CU ≈ 100 QPS） |
| 存储 | 含在库租里 | ¥0.0015 / GB / 小时 |
| 何时扣费 | **创建即扣**，空库也收 | **上传文档后**预留算力；清空文档不停 |
| embed / rerank | 约 ¥0.0005 / 千 token | 同左 |

旗舰不是「更好的标准版」，是独占集群。停费必须退订/删库。比百炼标准版（约 ¥22/月）贵大约三成。

别和扣子套餐、Agent Plan ¥49.9 混。

### OpenViking（文档 84313/2374478 一带）

Agent 上下文数据库：`viking://` 文件系统、L0/L1/L2、目录递归检索、会话抽记忆。不是更便宜的知识库。

- 开源 AGPLv3，可自建；embedding 可指到 SiliconFlow（`provider: openai`）。
- 托管 OpenViking Context / Service：个人大约 50 文件试用；模型锁豆包。
- 官方插件是 OpenClaw 的 `contextEngine`。FastClaw **没有** `openclaw plugins install` 这条。可试 MCP `http://<host>:1933/mcp`，不会自动 `assemble` / `afterTurn`。
- 隐藏成本是 VLM（摘要、记忆抽取），不是 embed。

### 记忆库 Mem0（文档 86722）

对话抽取事实/偏好/摘要，**不是**文档 RAG。Knowledge 不该走 Mem0。

FastClaw 已有 `plugins/mem0` hook：默认 `http://127.0.0.1:8100` 的 `/search`、`/memories`（开源 Mem0）。火山托管 API（`filters` 等）不能只改 URL。短对话先用 `MEMORY.md`。

火山自己也拆开：知识库管文档，Mem0 管跨会话记忆。

## 5. Dify 做 RAG？

**能用，不是为「只做 RAG」长的。** 社区主流是「Dify 一条龙做带界面的问答应用」，不是「外面 Agent 只调 retrieve」。

| 社区玩法 | 多不多 |
|---|---|
| Dify = 知识库 + 工作流 + 对话 | 最多 |
| Dify = 给业务的应用门面 | 很多 |
| 只要 Knowledge API 给别的系统搜 | 少数，但官方支持 |

选型共识：要应用/编排 → Dify；脏 PDF → RAGFlow；只要轻量对话知识库 → FastGPT / MaxKB；英文工程圈 → LlamaIndex + eval。

「FastClaw 上传、Dify 检索」架构对，**现成没有**：

- MCP 只让 Agent 去搜，不能把 Knowledge 页变成 Dify 客户端。
- 要对齐上传，需要服务端转发 `create-by-file` / 删除 / 列表，并把 `knowledge_search` 改成 `POST /v1/datasets/{id}/retrieve`。
- 必须放开 256KB 和「仅文本」，否则 PDF 仍进不去。
- 检索常用 `dataset-` Key，上传常要有编辑权限的用户 Token；索引异步；删除要对 Dify `document_id`。

反方向（Dify 外部知识库 API 来调 FastClaw）不要走。也不要两个编排器同时聊。

已经在跑 Dify：MCP 挂检索，文档只放 Dify。从零只为 FastClaw 补语义检索：FastGPT/MaxKB + SiliconFlow 比上整套 Dify 更贴近社区「纯 RAG」用法。

## 6. 怎么选（给 FastClaw）

| 需求 | 做法 |
|---|---|
| 几份 md/txt，总字数不大 | 自带 Knowledge |
| 只要便宜中文对话模型 | Custom → SiliconFlow |
| 语义检索、尽量省、已有 8G VPS | FastGPT/MaxKB + SiliconFlow embed，MCP 挂上 |
| 已有 Dify，文档已在里面 | MCP 调 retrieve；不要再传到 FastClaw |
| Agent 长期记忆 + `viking://` | 自建 OpenViking + SiliconFlow embed + 便宜 VLM；FastClaw 无官方插件 |
| 「上传即用、不管切片」 | 百炼标准版通常比火山标准版便宜 |
| 飞书文档 + 已在方舟 | 火山知识库标准版（约 ¥30/月 + token） |
| 跨很多轮用户画像 | 先 `MEMORY.md` / 自带 mem0 插件；不够再托管 Mem0 |

钱从低到高（大致）：SiliconFlow 只 embed ≪ 自建小 RAG ≪ 百炼标准版 ≪ 火山知识库标准版 ≪ 旗舰/海外托管。重排开大时，token 往往超过库租。

## 7. 明确没做的

这次没有实现：Knowledge → Dify/百炼/方舟转发、SiliconFlow embed、OpenViking 官方插件、Mem0 托管适配。要做其中一条再单开任务。
