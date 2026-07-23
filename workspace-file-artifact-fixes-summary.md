# FastClaw 文件产出物链路修复总结

## 背景

这次主要解决 FastClaw 中普通文件产出物，尤其是 HTML / Markdown / 文本文件，在以下几个路径之间不一致的问题：

- AI / 工具看到的虚拟路径：`/workspace/<file>`
- sandbox / 本地真实写入路径：`~/.fastclaw/workspaces/...`
- workspace store 中的真实对象路径
- 前端可点击的文件链接
- OpenAI-compatible API 返回给调用端的链接
- 配置了 R2 / object store 后的公网访问 URL

原来的图片产出物链路已经相对完整：图片生成后会写入 workspace store，并返回可展示 URL。但普通文件原来没有完整复用这个思路，导致 HTML / md 等文件容易出现“文件写了，但链接打不开 / R2 404 / 调用端拿到 `/workspace/...`”的问题。

---

## 一、原来的问题

### 1. `/workspace/...` 被当成了对外 URL

原来普通文件写入后，AI 最终回复中经常出现：

```md
[打开文件](/workspace/report.html)
```

但 `/workspace/report.html` 只是 sandbox / 工具内部的虚拟路径，不是公网 URL。

问题是：

- AI 不应该自己生成真实公网 URL；
- 前端 / 调用端也不应该自己猜真实存储路径；
- 真实可访问 URL 应该由后端根据文件真实写入结果和存储配置生成。

---

### 2. 前端用 `session_key` 拼真实文件路径，导致 404

旧前端逻辑大致是：

```ts
fileUrl(agentId, sessionId ? `sessions/${sessionId}/${rel}` : rel)
```

也就是把：

```text
/workspace/foo.html
```

转换成：

```text
/api/agents/<agent>/files/sessions/<session_key>/foo.html
```

但在 www-agents 场景中：

```text
session_key != 后端真实 chat_id / workspace scope
```

所以前端拼出的路径和后端真实存储路径不一致，最终文件接口 404。

---

### 3. 配置了 R2 后，对外路径没有优先使用 R2 public URL

目标 agent / owner user 已配置 Cloudflare R2：

```text
type=cloudflare-r2
bucket=www-agents
prefix=fastclaw
publicBaseURL=https://r2.www-agents.com
```

因此文件对外 URL 应该优先是：

```text
https://r2.www-agents.com/fastclaw/<agent>/sessions/<scope>/<file>
```

而不是：

```text
/api/agents/...
https://www-agents.com/workspace/...
/workspace/...
```

---

### 4. OpenAI-compatible API 原样返回了 agent 回复

OpenAI-compatible API 原来直接把 agent 最终回复文本返回给调用端。

如果 agent 回复中有：

```text
/workspace/foo.html
```

或者调用端补全成：

```text
https://www-agents.com/workspace/foo.html
```

API 层不会把它转成 R2 URL，导致外部调用端拿到不可访问链接。

---

### 5. Docker sandbox 的 workspace path 含冒号时，容器创建失败

www-agents 的 session scope 形如：

```text
www-agents:<uuid>:<uuid>
```

本地 workspace 路径里会包含冒号：

```text
~/.fastclaw/workspaces/<agent>/sessions/www-agents:<uuid>:<uuid>
```

原 Docker mount 使用：

```text
-v <host-path>:/workspace:rw
```

Docker 的 `-v` 也是用冒号分隔字段，因此 host path 中的冒号会导致：

```text
docker: invalid spec ... too many colons
```

从而出现：

```text
write_file failed
sandbox create docker sandbox failed
```

---

### 6. Docker sandbox 写 `/workspace/...` 后没有即时上传 R2

后续又发现一种情况：

```text
Written to /workspace/us_market_analysis.html
```

文件确实写到了本地 Docker bind mount：

```text
~/.fastclaw/workspaces/<agent>/sessions/<scope>/us_market_analysis.html
```

但 R2 上没有对应对象，所以 R2 public URL 404。

根因是：

- Remote / E2B sandbox 写 `/workspace/...` 后会 mirror 到 workspace store；
- Docker backend 因为有本地 bind mount，原来没有每次 `WriteFile` 后立即 Put 到 workspace store；
- 在 local store 场景没问题；
- 但当前 workspace store 是 R2，因此前端/API 指向 R2 时就会 404。

---

## 二、这次修改的核心原则

### 核心原则一：真实 URL 由后端生成

AI 可以引用：

```text
/workspace/foo.html
```

但真实公网 URL 必须由后端基于：

- agent id
- project id
- session key
- 后端解析出的真实 session / chat scope
- workspace store 配置
- PublicURL 配置

统一生成。

---

### 核心原则二：普通文件复用图片产出物的管理思路

图片原来的链路是：

```text
image_gen
→ workspaceStore.Put
→ workspaceStore.PublicURL / display URL
→ 返回给 AI / 前端 / 调用端
```

这次普通文件也改成类似模式：

```text
write_file
→ workspaceStore.Put
→ workspaceStore.PublicURL 优先
→ fallback 到后端 resolver
→ 前端 / 调用端使用真实 URL
```

不是新增复杂 artifact 系统，而是做最小一致化。

---

### 核心原则三：前端和调用端不再猜真实 workspace 路径

前端只传：

```text
文件相对路径 + session_key
```

后端负责解析：

```text
session_key → 真实 chat_id / workspace scope
```

然后再读取 workspace store 或重定向到 R2。

---

### 核心原则四：配置了 R2 / object store 时，PublicURL 优先

如果 workspace store 支持 `PublicURL`，则对外优先返回：

```text
https://r2.www-agents.com/...
```

而不是停留在：

```text
/api/agents/...
```

`/api/agents/.../files/...` 只作为 resolver / fallback，不作为有 R2 配置时的最终对外地址。

---

## 三、具体修改内容

## 1. 修改 `write_file` 工具返回逻辑

涉及文件：

```text
internal/agent/tools/file.go
internal/agent/tools/file_test.go
```

原来：

```text
write_file → workspaceStore.Put → 返回 Written xxx bytes to file
```

现在：

```text
write_file
→ workspaceStore.Put
→ 返回 Workspace path
→ 返回 URL
```

并且 URL 生成规则是：

```text
优先 workspaceStore.PublicURL(...)
失败则 fallback 到 /api/agents/<agent>/files/<rel>?sessionId=<session_key>
```

这样新生成的普通文件会直接在工具结果里带可访问 URL。

---

## 2. 修改前端 `/workspace/...` 链接转换逻辑

涉及文件：

```text
web/src/components/chat-markdown.tsx
web/src/lib/api.ts
```

旧逻辑：

```ts
fileUrl(agentId, sessionId ? `sessions/${sessionId}/${rel}` : rel)
```

新逻辑：

```ts
workspaceFileUrl(agentId, rel, sessionId)
```

生成：

```text
/api/agents/<agent>/files/<rel>?sessionId=<session_key>
```

前端不再拼：

```text
sessions/<session_key>/<file>
```

因为这个路径在 www-agents 场景不等于真实 workspace scope。

---

## 3. 修改后端文件 resolver

涉及文件：

```text
internal/setup/handlers_agents.go
internal/setup/handlers_agent_files_signedurl_test.go
```

后端 `/api/agents/<agent>/files/<rel>?sessionId=<session_key>` 现在会：

```text
1. 接收 rel 和 sessionId
2. 用 sessionId 查真实 session / chat scope
3. 生成真实 workspace store path
4. 如果 workspaceStore.PublicURL 成功，302 到 R2/CDN URL
5. 否则 fallback 到 signed URL 或本地 stream
```

这样历史消息里的：

```md
[foo.html](/workspace/foo.html)
```

前端也能通过 resolver 打开真实文件。

---

## 4. 支持 R2 / object store PublicURL 优先

涉及文件：

```text
internal/workspace/s3.go
internal/setup/handlers_agents.go
internal/agent/tools/file.go
```

确认 R2 store 的 key 规则为：

```text
<prefix>/<agent>/sessions/<session_scope>/<file>
```

最终 public URL 为：

```text
<publicBaseURL>/<prefix>/<agent>/sessions/<session_scope>/<file>
```

例如：

```text
https://r2.www-agents.com/fastclaw/<agent>/sessions/<scope>/report.html
```

现在普通文件、HTML、Markdown 等只要进入 workspace store，就会优先使用这个 public URL。

---

## 5. 修改 OpenAI-compatible API 返回层

涉及文件：

```text
internal/api/openai.go
internal/api/server.go
cmd/fastclaw/main.go
internal/api/openai_workspace_rewrite_test.go
```

原来 API 层只返回 agent 原文。

现在 API 层拿到了当前 user / agent 对应的 workspace store，并在 response 返回前做兜底 rewrite：

```text
/workspace/foo.html
https://www-agents.com/workspace/foo.html
```

转换为：

```text
https://r2.www-agents.com/fastclaw/<agent>/sessions/<scope>/foo.html
```

覆盖：

- non-stream response
- stream response

为了避免 streaming chunk 把链接切碎导致无法匹配，当前 stream 方案是：

```text
先 buffer 最终文本
→ rewrite workspace URL
→ 再发送最终 content
```

这是可靠性优先的最小修复。

---

## 6. 重建并嵌入前端静态产物

涉及目录：

```text
web/out
internal/setup/web
```

FastClaw Go binary 使用：

```go
//go:embed all:web
```

所以只改 `web/src` 不够，还必须：

```text
cd web
pnpm build
rm -rf internal/setup/web
cp -a web/out internal/setup/web
go build
install binary
restart gateway
```

这次已完成前端构建，并确认旧的前端转换逻辑不再存在于新 bundle 中。

---

## 7. 修改 Docker sandbox mount 方式，兼容冒号路径

涉及文件：

```text
internal/sandbox/docker.go
internal/sandbox/docker_mount_test.go
```

旧逻辑：

```text
-v <host-path>:/workspace:rw
```

当 host path 含冒号时会报：

```text
too many colons
```

新逻辑：

```text
--mount type=bind,source=<host-path>,target=/workspace
```

readonly 场景：

```text
--mount type=bind,source=<host-path>,target=<container-path>,readonly
```

同时统一修改了：

- workspace mount
- user skills mount
- template mount
- skill directories mount

这样不需要修改 session scope，也不需要 hash / 清洗路径，保留原有 workspace 语义。

---

## 8. 修改 Docker sandbox `WriteFile` 后即时同步 workspace store

涉及文件：

```text
internal/sandbox/lifecycle.go
internal/sandbox/lifecycle_test.go
```

原来：

```text
Docker WriteFile
→ 写入本地 bind mount
→ 不立即 Put 到 R2
```

现在：

```text
Docker / E2B / Remote WriteFile
→ 只要写入 /workspace/<file> 成功
→ 立即 mirror 到 workspaceStore
→ workspaceStore 是 R2 时立即上传 R2
```

这样：

```text
Written to /workspace/report.md
Written to /workspace/report.html
```

这类 sandbox 写入也会立即进入 R2。

---

## 四、现在的完整链路

### 新生成普通文件

```text
AI / tool 生成文件
→ write_file 或 sandbox WriteFile
→ 写入 /workspace/<file>
→ workspaceStore.Put
→ 如果配置 R2，则上传 R2
→ 生成 PublicURL
→ 前端 / API / 调用端拿到 R2 URL
```

---

### 历史消息里的 `/workspace/...`

```text
历史消息: /workspace/foo.html
→ 前端转换成 resolver
→ /api/agents/<agent>/files/foo.html?sessionId=<session_key>
→ 后端解析真实 scope
→ workspaceStore.PublicURL
→ 302 到 R2 URL
```

---

### OpenAI-compatible API 返回

```text
agent 最终回复中仍有 /workspace/foo.md
→ API response 层兜底 rewrite
→ 返回 https://r2.www-agents.com/.../foo.md
```

---

### Docker sandbox 写入

```text
Docker sandbox 写 /workspace/foo.md
→ 本地 bind mount 有文件
→ 立即 mirror 到 workspaceStore
→ R2 有对象
→ PublicURL 可访问
```

---

## 五、对不同文件类型的影响

这次修改不只针对 HTML。

只要是真实写入 workspace 的文件，都适用：

```text
.html
.md
.txt
.csv
.json
.pdf
.png
.webp
zip
其他普通文件
```

判断标准不是扩展名，而是：

```text
文件是否真实写入 /workspace/<file>
或是否通过 write_file 成功写入 workspace store
```

如果写到这些位置，则会进入 R2/public URL 链路。

不自动处理的情况：

```text
写到 /tmp/xxx
写到容器非 /workspace 路径
AI 只在回复里编了一个链接，但没有真实写文件
写入失败但模型仍然说保存成功
```

---

## 六、验证过的结果

### 后端 / sandbox / API 测试

已通过：

```text
go test ./internal/sandbox ./internal/api ./internal/agent/tools ./internal/setup -count=1
```

覆盖点包括：

- `write_file` 返回 workspace URL
- `write_file` 优先使用 PublicURL
- resolver 使用真实 session scope
- resolver 配置 PublicURL 时 302 到 R2
- OpenAI API response rewrite `/workspace/...`
- Docker mount host path 含冒号时使用 `--mount`
- Docker sandbox `WriteFile` 后同步 workspace store

---

### 服务状态

已重新编译并重启：

```text
fastclaw-gateway active
root page 200
```

---

### 已补上传历史文件

历史文件：

```text
us_market_analysis.html
```

已从本地 workspace 手动同步到 R2，验证 R2 public URL 返回：

```text
200
Content-Type: text/html
```

---

## 七、相比原来，最终变化总结

| 项目 | 原来 | 现在 |
|---|---|---|
| 普通 `write_file` 返回 | 只返回 `Written ...` | 返回 `Workspace path` 和真实 URL |
| `/workspace/...` | 容易被前端 / 调用端错误拼接 | 由后端 resolver 解析 |
| 前端文件链接 | 用 `session_key` 拼 `sessions/...` | 传 `sessionId` 给后端解析真实 scope |
| R2 配置 | 普通文件不一定遵守 PublicURL | PublicURL 优先 |
| OpenAI API | 原样返回 agent 文本 | 返回前兜底 rewrite 为 R2 URL |
| stream API | 可能无法处理跨 chunk URL | buffer 后统一 rewrite |
| Docker mount | `-v` 遇冒号路径失败 | 改为 `--mount` |
| Docker 写 `/workspace` | 本地有，R2 可能没有 | 写成功后立即 mirror 到 R2 |
| HTML / md 文件 | 容易本地有但公网 404 | 进入统一 workspace store / R2 链路 |

---

## 八、核心结论

这次修改的本质是：

```text
把普通文件产出物，从“AI/前端/调用端各自猜路径”，改成“后端基于真实写入结果和 workspace store 配置统一生成 URL”。
```

最终目标：

```text
普通文件 / HTML / Markdown 与图片一样，走统一的 workspace store → PublicURL → 前端/API/调用端链路。
```
