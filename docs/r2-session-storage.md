# R2 三级配置与会话中间产物自动落盘

本文是 yangzec 上落地 Cloudflare R2 的实施方案，对照 `origin/tencent` 已有实现，只移植对象存储相关能力，不整支 cherry-pick。

目标：会话里产生的中间产物（`write_file`、sandbox 输出、上传附件、生图等）按 **agent → user → default** 解析到对应 R2，自动写入，不改现有 key 布局，不迁移旧文件。

---

## 1. 结论

yangzec 已经把会话产物写进 `workspace.Store`。缺的不是“再加一条保存链路”，而是 **按作用域选出正确的 Store**。

tencent 已经做对的部分：

- 每一级 objectstore 是 **整包配置**，不是字段级 merge
- 运行时路由：agent 行 → user 行 → 全局 Store
- 行存在但非法时 **fail-closed**，绝不 silently 掉回另一级
- 保存前做 R2 读写探活
- 密钥不回传明文，UI 用 mask
- 切换存储 **不迁移、不删除** 已有对象

yangzec 相对 tencent 要补的：

- 把 default 做成和 user / agent 同级的一等配置（tencent 只有 user / agent 专用页，default 靠 env + `/api/config`）
- 运行时路由 **禁止** 走 `scope.Setting` 的顶层字段 merge（现有 `assembleConfig` 会把不同级的 bucket / key 拼在一起）
- 保留 yangzec 已修好的多租户文件隔离（`9fa35ab`），不要把 tencent 的文件 GET / SSE 行为带回来

---

## 2. 现状

### 2.1 yangzec：写入已经按会话落盘

`workspace.Store` 的路径约定已经稳定：

| projectID | sessionID | 对象 key |
|---|---|---|
| `""` | `""` | `<prefix>/<agentID>/<path>`（agent 共享） |
| `""` | `sid` | `<prefix>/<agentID>/sessions/<sid>/<path>` |
| `pid` | `""` | `<prefix>/<agentID>/projects/<pid>/<path>` |
| `pid` | `sid` | `<prefix>/<agentID>/projects/<pid>/<sid>/<path>` |

已经走 `Store.Put(agentID, projectID, sessionID, …)` 的路径：

- `write_file` / `apply_patch` / `edit_file`
- 聊天附件、HTTP 上传
- sandbox 写后镜像（`mirrorSandboxWrite`）
- sandbox 执行后 / 空闲回收时的 `syncSnapshot`
- IM 本轮新媒体回扫（`appendNewWorkspaceMedia`）

这些路径 **不需要再复制一套“自动保存”**。只要 gateway 上的 `workspace.Store` 是带三级路由的 router，产物就会进对应 R2。

当前 gateway 只在启动时打开 **一个** 全局 Store：

```
system objectstore 行 + FASTCLAW_OBJECT_STORE_* → workspace.Factory.New
```

user / agent 即使在 `configs` 里写了 `objectstore`，运行时也不会用。

### 2.2 yangzec：配置层已经有三级，但 merge 方式错了

`scope.Setting` 的链是：

```
system (user='', agent='')
  → user (user=X, agent='')
    → agent (user='', agent=Y)
      → per-(user, agent)
```

它对 **顶层 key 做字段覆盖**。这对 `agents.defaults.model` 合适，对 objectstore **不合适**：

- agent 只填了 `bucket`，会吃到 user 的 `accessKey`
- user 改了 `prefix`，会和 default 的 endpoint 拼成一套从未探活过的配置
- 半套凭证写进错误 bucket，比“没用上 R2”更危险

`assembleConfig` 已经对 `NSObjectStore` 做了这种 merge。路由层不能复用它。

### 2.3 tencent 已经落地的形状

配置行：`kind=setting, name=objectstore`，整包 `ObjectStoreCfg`。

| 级别 | 存储坐标 | API | 谁能写 |
|---|---|---|---|
| agent | `user='', agent=<id>` | `/api/agents/{id}/objectstore` | agent owner |
| user | `user=<uid>, agent=''` | `/api/me/objectstore` | 非 `app_user` 的登录用户 |
| default | `user='', agent=''` + env | 无专用 API；super_admin 可经 `/api/config` 写 system 行 | 运维 / super_admin |

运行时 `agentStoreRouter.storeFor(agentID)`：

1. 有 agent 行 → 只用它（非法则报错，不回落）
2. 否则读 agent.owner → 有 user 行 → 只用它
3. 否则用启动时的全局 Store（system + env，或 local FS）

UI：`StorageSettingsForm`，R2 专用字段（Account ID / Bucket / Prefix / Endpoint / Public Base URL / keys），先 Test 再 Save。

tencent 另有一条 **展示链路**（本文第二期）：

- `PublicBaseURL` + `Store.PublicURL`
- 生图结果 archive 进 session workspace
- 聊天 / OpenAI 兼容 API 把 `/workspace/foo` 改写成 public URL 或 signed URL

yangzec 的 agent 被要求写 `[report](/workspace/report.md)`，前端再自己拼 `/api/agents/.../files/...`。R2 配好之后，如果没有改写，用户点开的仍是网关文件接口，不是 CDN。

---

## 3. 三级模型

三级都是 **完整 R2 配置**，不是 overlay。

```
agent 整包
  └ 没有 → user 整包
      └ 没有 → default（system 行 ⊕ env）
          └ 没有 / type=local → 本地 ~/.fastclaw/workspaces
```

`source` 回给 UI 的取值：`agent` | `user` | `default`。tencent 用 `global`，yangzec 对外改叫 `default`，内部可读两边。

### 3.1 Default（默认 / 系统）

给整站兜底，自托管和多租户托管都用它。

组成：

1. system 行：`kind=setting, user='', agent='', name=objectstore`
2. 启动时 `FASTCLAW_OBJECT_STORE_*` **盖在 system 行上**（现有 `readObjectStoreCfg`）
3. 都没有则 `type=local`

约束：

- 只有 **super_admin 且未 acting-as** 能写 default
- 普通用户保存 Settings 不得再经 `/api/config` 误写 objectstore（移植 tencent 的拦截）
- env 适合 K8s / compose 注入；UI 适合运行时改。两者同时存在时 **env 赢**，GET 要标明 `envOverride: true`，避免 UI 保存了却不生效

建议新增专用 API，不要继续挤在 `/api/config`：

```
GET    /api/system/objectstore
POST   /api/system/objectstore/test
PUT    /api/system/objectstore
DELETE /api/system/objectstore
```

DELETE 只删 system 行，不清 env。清掉之后若 env 仍在，default 仍是 R2。

Default 允许 `cloudflare-r2` 或其它 S3 兼容 preset（和现有 Factory 一致）。User / agent 第一期只暴露 R2，避免把 Aliyun / MinIO 的运维旋钮铺到每个用户。

### 3.2 User（用户）

该用户名下、没有 agent 覆盖的 agent 共用一套 R2。

- 行：`user=<uid>, agent=''`
- API：`/api/me/objectstore`（与 tencent 相同）
- `app_user` 只读继承，不能写
- 保存 / 删除后按 `ownerUserID` 失效所有 `source=user` 的 agent 缓存

推荐默认 prefix：`users/<userID>`。用户可改；空 prefix 表示 bucket 根。agentID 已经在 key 里，同 bucket 下不同 agent 不会撞。两个用户若手填同一个 bucket+prefix，才能互相看到对方的 agent 目录——文档里写清楚，UI 给一句提示。

### 3.3 Agent（智能体覆盖）

只影响这一个 agent。

- 行：`user='', agent=<id>`（与现有 agent-scope setting 一致）
- API：`/api/agents/{id}/objectstore`
- 写：owner；读：owner（GET 可展示当前生效 source，包括继承来的 user / default，但不回传继承来的密钥）
- 删除覆盖后回落到 user，再没有则 default

Agent 页必须显示 **Current source**，否则用户分不清“我在改覆盖”还是“只是看着继承值”。

tencent 的 `Configured` / `Enabled` 只在 source 为 agent/user 时为 true。Default 生效时应是 `configured=true, enabled=true, source=default`，避免 UI 把“站点已经在用 R2”画成未配置。

---

## 4. 配置字段

User / agent（R2 专用）：

| 字段 | 必填 | 说明 |
|---|---|---|
| `accountId` | endpoint 为空时必填 | 用来拼 `<id>.r2.cloudflarestorage.com` |
| `bucket` | 是 | |
| `prefix` | 否 | 建议 `users/<uid>` |
| `endpoint` | 否 | 只允许 HTTPS hostname，无 path / query / userinfo |
| `publicBaseURL` | 否 | 第二期用来发 CDN 直链 |
| `accessKey` / `secretKey` | 是 | GET 只回 `hasAccessKey` / `hasSecretKey`；空字符串表示保留原值 |

服务端强制 `type=cloudflare-r2`, `useSSL=true`。

Default 额外保留现有 Factory 字段：`type`, `region`, `aliyunInternal`, `local.root`，方便自托管继续用 MinIO / OSS。

探活（tencent `testObjectStoreConnection`）：对目标 bucket 做 Put → Get → Delete `health-check/<rand>.txt`。Test 和 Save 都要过。失败返回 502，不写库。

---

## 5. 运行时路由

移植 `internal/gateway/agent_store_router.go`，挂在现有全局 Store 外面：

```go
wsInner, _ = workspace.Factory{...}.New(...)          // default
ws        = newAgentStoreRouter(wsInner, st, nil)     // agent / user 覆盖
```

`storeFor` **只读单行**（`GetConfigByName`），禁止 `scope.SettingInto`。

缓存：

- key = `agentID`
- value = `{store, source, ownerUserID}`
- `ClearAgentStoreCache(agentID)`
- `ClearUserInheritedStoreCache(userID)` 清所有 `source=user && ownerUserID==userID`
- default 变更：清整个 cache（或重启；第一期清 cache 即可）

Fail-closed：

- 行存在、`enabled=false`、data 空、type 不是 R2、Factory 失败 → 返回 error
- 调用方（`write_file`、sandbox sync、上传）把错误打到工具结果 / slog，**不要**改写到 default
- 半套覆盖把文件写进站点 bucket，比写入失败更难修

`assembleConfig` 里的 objectstore merge 只给 `/api/config` 展示用。长期应改成和 router 一样的整包解析，并带 `source`。第一期至少：非 super_admin 不能经 `/api/config` 写 objectstore。

---

## 6. 会话中间产物：自动保存怎么生效

```
工具 / sandbox / 上传
        │
        ▼
workspace.Store.Put(agentID, projectID, sessionID, relPath, bytes)
        │
        ▼
agentStoreRouter.storeFor(agentID)
        │
        ├─ agent R2
        ├─ user R2
        └─ default R2 或 local FS
```

yangzec 现已保证下列产物在 **同一套** `Put(agentID, projectID, sessionID, …)` 坐标落地（R2 router 接上后即自动上传）：

1. 相对路径 / `/workspace/...` 的 `write_file`、`apply_patch`
2. sandbox 内写到 `/workspace/` 的文件（写后镜像 + 快照）
3. 聊天拖拽 / HTTP 上传
4. IM 本轮新产生的可投递媒体
5. `image_gen`：provider URL / base64 会拉回并写入 `generated-images/…`，工具结果改成 `/workspace/generated-images/…`
6. `tts`：`/tmp` 音频会复制进 `generated-audio/…`，保留原来的 `MEDIA:` 行给 IM
7. Docker exec 写到 `/workspace/` 的文件：对象存储后端下 **每次 exec 后立刻 snapshot+Put**（不再等到 idle evict）。LocalFS + Docker bind-mount 仍跳过，避免无谓 churn

不自动进 R2（有意）：

- `SOUL.md` / `USER.md` / `IDENTITY.md` / `MEMORY.md`（走 system file store）
- skill 目录（现有 skill objectstore hydrate）
- `/tmp`、sandbox 家目录等非 `/workspace` 路径
- 会话 JSON 本身（Postgres / SQLite，不是 blob）

切换 source **不搬文件**。UI 文案必须写明。旧 session 的 Files 面板会空，直到有新写入；需要旧文件时，把覆盖改回去。

第一期不做：按扩展名过滤、TTL 生命周期、把 scratch（`todo.md`）和 deliverable 存进不同 prefix。IM 已经不自动投递 `.md/.txt`；R2 里仍按 session 存，Files 面板能看到过程文件，这是现有行为。

---

## 7. UI

### Settings → Storage

- 普通用户：一块 “My R2”，source 显示 User / Default
- super_admin：上面一块 Default（站点兜底），下面一块 My R2
- acting-as 时只显示被代理用户的 My R2，不能改 Default

复用 tencent `StorageSettingsForm`，加 `scope: "agent" | "user" | "default"`。

### Agent → Storage

挂进 agent settings dialog，和 Customize / Models 并列。

- 表单预填当前生效值，但继承来的密钥不回传、不预填
- 未保存覆盖时：Current source = User 或 Default；Save 之后变成 Agent
- Remove override 只删 agent 行

导航：

- `web/src/app/settings/layout.tsx` 增加 Storage
- `web/src/app/settings/storage/page.tsx`
- `web/src/app/agents/[id]/storage/page.tsx`
- `agent-settings-dialog.tsx` 增加 tab

---

## 8. 和 yangzec 隔离模型的衔接

`9fa35ab` 已经：

- 按调用者收窄文件 GET / project list
- 身份文件 HTTP 只给 owner
- sandbox hydrate 路径消毒

Router 只换 **后端 bucket**，不改 HTTP 鉴权，也不改 key 里的 `sessions/<sid>`。

不要做：

- 在 S3 key 里再塞一层 `users/<uid>`（user prefix 可选，不是强制改 Store 接口）
- 把 tencent 未隔离的文件 GET 逻辑合进来
- 让 `app_user` 写 user / default 配置

Signed URL / Public URL 仍然只在 **已通过现有文件 ACL** 之后签发。PublicBaseURL 若把 bucket 配成公开读，等于该 prefix 下对象对互联网可见——UI 要写明，默认仍走网关 + 短 TTL signed URL。

---

## 9. 实施切片

按这个顺序做，每一刀都可以单独 PR、单独测。

### 切片 A — 配置与路由（自动落盘的最小闭环）

移植并改到 yangzec：

- `internal/setup/handlers_agent_objectstore.go` + test
- `internal/gateway/agent_store_router.go` + test
- `PublicBaseURL` 字段进 `ObjectStoreS3Cfg` / `workspace.S3Config`（先存着，切片 C 再用）
- gateway：`ws = newAgentStoreRouter(defaultStore, st)`
- `/api/config`：非 super_admin 禁止写 objectstore
- `/api/system/objectstore*`（tencent 没有，yangzec 补上）
- 探活、密钥 mask、cache 失效

验收：

- 只配 default R2 → 任意 agent 的 `write_file` 出现在 default bucket 的 `sessions/<sid>/`
- 再配 user R2 → 该用户的 agent 改写到 user bucket，其它用户仍走 default
- 再配 agent R2 → 只有这个 agent 改 bucket
- 删 agent 覆盖 → 回到 user
- agent 行缺 secret → `write_file` 失败，default bucket **没有**新对象
- 多副本：A 保存 user R2 后，B 上同用户的下一次 Put 必须打到新 bucket（cache 失效或短 TTL）

### 切片 B — UI

- `StorageSettingsForm` + `agent-storage-api.ts`
- Settings / Agent Storage 页
- Current source 徽章
- Test → Save；Remove 确认框写明不迁移

### 切片 C — 对外 URL（保存之后能打开）

对照 tencent `e1f3e92`，只移植展示，不改存储布局。落地本身已在 yangzec 完成（`image_gen` / `tts` archive、Docker+对象存储 post-exec sync）。**会话回写约定见 §13。**

已落地：

- `Store.PublicURL` + `S3Config.PublicBaseURL` / `FASTCLAW_OBJECT_STORE_PUBLIC_BASE_URL`
- 工具结果：`Workspace path: /workspace/<rel>` + 稳定展示 URL（PublicURL，否则网关 `?sessionId=`；coding 省略 sessionId）
- 文件 GET：过 ACL 后非 HTML 302 → PublicURL → 10m SignedURL → 网关代读
- IM：`StoreSessionID`（coding 收成项目根）
- OpenAI：只改 HTTP 响应，不改已落库的 `/workspace/` markdown
- 前端继续用 `?sessionId=`，不用 `session_key` 猜 `sessions/<session_key>/...`

### 明确不做

- 整支 merge tencent
- 切换 source 时搬迁 / 双写
- 用 `scope.Setting` 字段 merge 做出运行时 Store
- 第一期给每个用户开放 MinIO / OSS 全量旋钮
- 改 session 聊天记录的存储后端

---

## 10. 测试要点

已有 tencent 测试应一起搬，并补 default：

- `agent_store_router_test.go`：agent / user / default 优先级；fail-closed；user 缓存失效
- `handlers_agent_objectstore_test.go`：owner ACL、`app_user` 403、空 key 保留旧 secret、探活失败不落库
- 新增 system objectstore：非 super_admin 403；acting-as 不能写 default
- 现有 `handlers_agents_workspacefile_test.go` 隔离用例必须继续绿
- sandbox `lifecycle_test.go`：镜像 / snapshot 仍按 session 写入 **router 后面的** Store
- 项目聊天 vs coding：hydrate 目标、Docker 挂载 = store 前缀、HTTP list/GET/upload 的 session 段必须和 `storeSessionID` 一致

---

## 11. 建议的落地顺序

先做 **切片 A**。做完之后，会话中间产物就会按三级配置自动进 R2，现有 Files 面板和 IM 投递不用改。

切片 B 让用户自己配，而不是只靠 env。

切片 C（§13）解决“文件在 R2 里，如何正确回到会话”：历史只存 `/workspace/`，展示 URL 在读时解析。

---

## 12. 项目 + sandbox 坐标复盘

`workspace.Store` 的四段 key 和 sandbox 物理路径不是同一件事。对齐规则：

**整包替换**：某次 `Get(agent, project, session)` 的 `session` 必须等于 file tools 的 `scopeSessionID()` / `Agent.storeSessionID()`。Coding agent（`projectRuntime != nil` 且当前在项目里）把 session 收成 `""`，和 preview 共用 `projects/<pid>/`。

| 模式 | Store `(project, session)` | Docker / E2B 挂载 | 容器 `/workspace` | snapshot |
|---|---|---|---|---|
| 散聊 | `("", sid)` → `sessions/<sid>/` | 该 session 目录 | store 前缀 | 整棵挂载（跳过 `node_modules` 等） |
| 项目聊天 | `(pid, sid)` → `projects/<pid>/<sid>/` | **同一套 store 前缀**（不再挂项目根） | `img.save('/workspace/x')` = 本 chat 的 `x` | 整棵挂载 → Put 回 `(pid, sid)` |
| Coding | `(pid, "")` → `projects/<pid>/` | 项目根 | 项目树 | 整棵项目（跳过构建产物） |

各产物是否打到同一套坐标：

| 产物 | 散聊 | 项目聊天 | Coding |
|---|---|---|---|
| `write_file` / `apply_patch` | session 前缀 | `projects/<pid>/<sid>/` | 项目根 |
| 镜像进 sandbox | `/workspace/<rel>` | `/workspace/<rel>` | `/workspace/<rel>` |
| 附件 `WriteSessionAttachments` | 同上 | 同上 | 项目根 + 共用 sandbox |
| HTTP list / GET / upload / reveal | `?sessionId=` → chat 前缀 | 同左 | `runtimeMgr` 在时收成项目根，和 write_file 一致 |
| `image_gen` / `tts` archive | `scopeSessionID()` | 同左 | 项目根 |
| Docker / E2B / Boxlite exec 后 snapshot | 挂载根 | 挂载根（= chat 前缀） | 项目根 |
| sandbox `WriteFile` 镜像 | `/workspace/x` → store `x` | 同左 | 同左 |
| LocalFS + Docker bind-mount | 无需 post-exec sync | 同左；对象存储后端仍 post-exec sync | 同左 |

实现约束（不是缺口）：

- Coding 下多个 chat **共用**一个 turn sandbox（`Get(..., session="")`）。`MoveWebChatSession` 的 `Release` 仍用 chat id，对共享槽是 no-op，idle evict 会收。
- 项目聊天的 sandbox **不再**看到同项目其它 chat 的目录；Files 面板仍按本 chat 前缀列文件。
- `SOUL.md` 等身份文件、skill 树、`/tmp`、会话 JSON 仍不进 session object store。

---

## 13. R2 路径如何正确回到会话

对象进了 R2 之后，**不要把 R2 key、`r2.dev` 直链或 signed URL 写进会话 JSON**。会话里只存 sandbox 稳定路径；展示 URL 在读的时候再算。

### 13.1 三层路径，各管各的

| 层 | 例子 | 谁看见 | 能不能进聊天记录 |
|---|---|---|---|
| 会话路径 | `/workspace/report.md` | 模型、历史、IM 解析、Web 改写 | **必须**。跨存储源、跨 TTL 都稳定 |
| Store 坐标 | `Put(agent, project, storeSessionID, "report.md")` → `sessions/<chat_id>/report.md` 或 `projects/<pid>/[sid/]report.md` | 网关 / Store | 不写进 markdown。读时由 `(agent, project, storeSessionID)` 推出来 |
| 展示 URL | `/api/agents/{id}/files/report.md?sessionId=<session_key>`，或 ACL 之后的 PublicURL / 短 TTL SignedURL | 浏览器、OpenAI 响应、CDN | **不持久化** signed URL（会过期）。PublicURL / 网关 URL 可以出现在**工具结果**里给模型看，最终回复仍应引用 `/workspace/` |

tencent 404 的根因是把 `session_key` 猜成 `sessions/<session_key>/…`。yangzec 的约定：

- 模型写 `[report](/workspace/report.md)`
- Web `chat-markdown.tsx` 改写成 `/api/agents/{id}/files/report.md?sessionId=<session_key 或 chat_id>`
- `GET` 用 `workspaceSessionScope` 把 token 收成 `chat_id`，再用 `httpStoreSession` 收 coding（`session=""`）
- **禁止**在 URL path 里手拼 `sessions/<session_key>/`

### 13.2 各通道怎么回写

**工具结果（`write_file` / `tts`）**

```
Written N bytes to report.md
Workspace path: /workspace/report.md
URL: /api/agents/{id}/files/report.md?sessionId=<goalSessionKey>
```

`URL` 优先 `Store.PublicURL`（配了 `publicBaseURL`）。没有则走网关文件接口；`sessionId` 用持久 `session_key`，**coding 根省略 sessionId**（文件在 `projects/<pid>/`）。工具结果里**从不**放 signed URL。

`image_gen` 的 markdown 继续用 `![x](/workspace/generated-images/…)`，让模型原样抄进最终回复。

**Web 聊天**

历史里仍是 `/workspace/…`。前端 `fileUrl()` 拼 cookie 鉴权的 files API。GET 过 ACL 之后：非 HTML 先 302 到 PublicURL，再没有则 10 分钟 SignedURL，再没有则网关代读。HTML 不跳转，好让 CSP `sandbox` 生效。

**IM**

`splitMediaFromReply` / `splitFilesFromReply` / `appendNewWorkspaceMedia` 用 `Agent.StoreSessionID(project, chatID)` 去 `Store.Get` 字节，再当 MediaItem 发出去，并剥掉 markdown。Coding 下 session 收成 `""`，否则会去 `projects/<pid>/<chatID>/` 找项目根上的文件。

**OpenAI 兼容 API**

只改 **HTTP 响应**（含流式 chunk），不改已经落库的 assistant markdown。有 PublicURL 就把 `/workspace/foo` 换成 CDN；没有就保持 `/workspace/`，由调用方自己解析。

### 13.3 不要做的事

- 把 `s3://…`、R2 key、`*.r2.cloudflarestorage.com` 预签名链接写进 session
- 用 `session_key` 当 Store 的 session 段
- 切换 objectstore source 时期望旧 URL 还指向同一对象
- 在签发 PublicURL / SignedURL **之前**跳过文件 ACL

`publicBaseURL` 把 bucket 配成公开读，等于该 prefix 对互联网可见。默认仍走网关 + 短 TTL signed URL。
