# XAuth 第二阶段：管理后台补全

四阶段总计划在 `PLAN.md`。本文只负责第二阶段。

交接文档。第一阶段已完成并可运行。第二阶段的存储层已经写了一半，**服务端目前编译不过**。从“修复编译”开始，不要重做存储层。

不要做 xmail，不要加邮箱共享表，不要做多组织、第三方登录、Passkey、邮件验证码。那些是第三、第四阶段。

界面继续用简体中文。JSON API 继续走现有的同源校验、错误码和 `writeError`。管理操作只给管理员。

## 目标

Casdoor 的简化版管理能力，只保留单实例自托管需要的部分：

1. 操作记录：登录成功和失败、注册、退出、改密、应用登录、拒绝访问、撤销授权、管理操作、令牌重放。
2. 设备与授权：用户能看到自己的登录设备并退出其中一台或其余全部；能看到仍持有令牌的应用并撤销。
3. 应用访问控制：每个应用是“所有用户”“仅管理员”或“指定用户”。收紧权限时，已经发给无权用户的令牌立即作废。
4. 签名密钥轮换：生成新的 RSA 密钥并立刻用来签名。旧密钥留在 JWKS 里继续验签，直到管理员停用。停用后，用那把密钥签的令牌立刻无法通过 userinfo 和 introspection。
5. 用户列表：显示最近登录时间，可按邮箱或名称搜索，可强制某用户退出所有设备并作废令牌。

## 当前断点

`go test ./internal/store` 可以通过。`go build ./internal/server` 失败：

```text
internal/server/account.go: startSession 调用 CreateSession 少了 store.SessionMeta
```

`CreateSession` 现在的签名是：

```go
CreateSession(ctx, userID string, ttl time.Duration, meta store.SessionMeta) (token string, sess *Session, error)
```

`SessionMeta` 只有 `UserAgent` 和 `IP`。`startSession` 要带上 `r.UserAgent()` 和 `s.clientIP(r)`。用户自己的登录、注册都走这个函数，所以一处改完两边都有设备信息。

另外这几个文件还没 `gofmt`：`internal/store/audit.go`、`authrequests.go`、`clients.go`、`tokens.go`。先格式化再继续。

## 已经落地的存储

迁移在 `internal/store/migrations.go`，是第二段 SQL，用 `PRAGMA user_version` 自动执行。不要改第一段。

新增或变更：

| 位置 | 内容 |
| --- | --- |
| `sessions` | 增加 `id`、`user_agent`、`ip`。`id` 是给前端用的公开标识，cookie 里仍然只放随机 token |
| `users.last_login_at` | 可空。`TouchLogin` 在登录成功时写入 |
| `clients.access` | `all`、`admins`、`selected`，默认 `all` |
| `client_users` | `selected` 模式的允许名单，级联删除 |
| `audit_events` | 追加日志，保留 180 天，`Purge` 会删过期行 |
| `signing_keys.retired_at` | 已有列。停用前为 NULL |

可用方法：

- 会话：`ListSessions`、`DeleteUserSession(userID, id)`、`DeleteOtherSessions(userID, keepHash)`。
- 用户：`TouchLogin`。`User` 增加了 `LastLoginAt`，零值表示从未登录。
- 访问：`UserMayUseClient`、`ClientUserIDs`、`SetClientUsers`、`RevokeDisallowed`、`RevokeAdminOnlyGrants`、`ListGrants`、`RevokeGrant`。
- 审计：`Record`、`ListEvents`、`WithClientIP`、`EventGroups`。事件类型常量都在 `audit.go`。
- 密钥：`SigningKeys` 只返回未停用的，最新在前；`AllSigningKeys` 含已停用；`RetireSigningKey`。

`store.SetClock` 现在是并发安全的，测试可以在服务运行中拨动时间。

授权码被第二次使用时，`RedeemAuthCode` 已经会撤销整组令牌并写 `token.reuse` 事件。刷新令牌重用还没有写事件，见下面的 OIDC 适配。

## 还要做的后端

### 1. 让现有服务重新编译

- `startSession` 传入 `SessionMeta`。
- 全项目 `gofmt`，然后 `go test ./...` 必须恢复到第一阶段那 17 个测试全绿。改存储签名时不要悄悄改掉现有安全行为。

### 2. 把请求 IP 放进 OIDC 上下文

`server.serveProvider` 在交给库之前调用 `store.WithClientIP(ctx, s.clientIP(r))`。`Record` 在事件自己没填 IP 时会从 context 取。这样授权码重放、刷新重放都能记下地址。

### 3. 登录和账户事件

在 `internal/server/account.go`：

- 登录成功：`TouchLogin`，记 `login.success`。
- 密码错误或账户停用：记 `login.failure`。失败事件不要写出密码。未知邮箱也记，但 `actor_id` 为空，邮箱放 `detail`。
- 注册成功：记 `user.register`。
- 退出：记 `logout`。
- 用户改密码：记 `password.change`。
- 限流拒绝不必每条都记，避免被刷爆日志。

审计写入失败应记到服务日志，但不要让登录本身失败。存储层的 `Record` 返回错误，调用处决定是否忽略。

### 4. 应用访问控制

在 `internal/server/flow.go` 的 `approve`，以及登录后直接批准请求的路径里，批准前调用 `UserMayUseClient`。不允许时：

- 不批准请求。
- 记 `app.denied`。
- 对浏览器返回明确中文错误，例如“你没有使用这个应用的权限”。OIDC 回调则用 `access_denied`，沿用现有 `handleFlowDeny` 的回跳方式。

允许并完成批准时记 `app.signin`，subject 是应用。

管理 API：

- `AppJSON` 增加 `access` 和 `allowedUserIds`。
- 创建、更新应用接受 `access` 与 `allowedUserIds`。`access` 只能是这三个值。
- `selected` 模式调用 `SetClientUsers`。未知用户返回 404 风格的现有错误码，并在 `respond.go` 加中文说明。
- 访问范围变窄，或指定名单变小之后，调用 `RevokeDisallowed`。
- 管理员把某用户的 `isAdmin` 从 true 改成 false 时，调用 `RevokeAdminOnlyGrants`。停用用户的现有逻辑保持不变。

默认仍是 `all`，所以现有应用和测试行为不变。

### 5. 设备与授权 API

都要求已登录，只能操作自己的数据。

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | `/api/sessions` | 返回未过期设备：`id`、`userAgent`、`ip`、`createdAt`、`lastSeen`、`current` |
| DELETE | `/api/sessions/{id}` | 退出这一台。不能依赖前端传 token |
| POST | `/api/sessions/revoke-others` | 退出除当前设备以外的全部，返回数量 |
| GET | `/api/grants` | `ListGrants`：应用 id、名称、scope、最近使用时间、是否还有刷新令牌 |
| DELETE | `/api/grants/{clientID}` | `RevokeGrant`，并记 `grant.revoke` |

管理员强制退出：

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| POST | `/api/admin/users/{id}/signout` | `RevokeUserAccess(userID, "")`，记 `admin.user_signout` |

不要让管理员通过这个接口退出时保留自己的会话。目标用户是自己时，沿用“不能停用自己”的保护，或者只退出其他设备。推荐直接拒绝，错误码 `cannot_change_self`。

### 6. 操作记录 API

`GET /api/admin/events?group=&user=&before=&limit=`

- `group` 使用 `store.EventGroups` 的 `signin`、`apps`、`admin`、`security`。空值表示全部。
- `user` 是用户 id，匹配 actor 或 subject。
- `before` 是上一页最后一条的数字 id，用于向更旧的记录翻页。
- `limit` 最大 100，默认 50。
- 只返回管理员。响应里不要出现密码、令牌、客户端密钥或授权码。

详情只保留排障需要的字段，例如失败原因码、应用类型、访问范围、重放类型 `code` 或 `refresh`。

### 7. 刷新令牌重放也要记日志

`internal/oidcprovider/storage.go` 里，刷新令牌已撤销再次出现并 `RevokeFamily` 的分支，补一条 `token.reuse`，`detail.kind` 为 `refresh`。授权码那条已经在 `RedeemAuthCode` 写过，不要重复写。

`CreateAccessAndRefreshTokens` 里发现 `ErrTokenReused` 时也会整组撤销。两条路径最终只记一次。建议记在第一次发现重用的地方。

### 8. 签名密钥

OIDC 库每次签名都会调用 `SigningKey`，每次验签都会调用 `KeySet`，所以换密钥不需要重启进程。`loadKeys` 现在只在启动时读一次并缓存在 `Storage.keys`。轮换前必须改成：

- `SigningKey`、`SignatureAlgorithms`、`KeySet` 每次从数据库读未停用的密钥。
- 没有未停用密钥时按现在的方式生成第一把。
- 解析失败要返回错误，不能继续用空列表签名。

管理 API：

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | `/api/admin/keys` | id、创建时间、是否当前签名密钥、是否已停用。不返回私钥 |
| POST | `/api/admin/keys` | 生成新 RSA 2048 密钥，记 `admin.key_rotate`。新密钥因为 `created_at` 最新，会自动成为签名密钥 |
| POST | `/api/admin/keys/{id}/retire` | `RetireSigningKey`。拒绝停用最后一把未停用的密钥，记 `admin.key_retire` |

生成逻辑可以复用 `internal/oidcprovider/keys.go` 里的 thumbprint id 和 PEM 编码。把“生成并保存”抽成存储或 provider 上的一个方法，避免 HTTP 层自己拼私钥。

用户界面要写清楚：轮换后新令牌用新密钥；旧密钥停用前，旧令牌仍可验证；停用后旧访问令牌立即失效。

### 9. 管理操作都要留痕

创建、修改、删除用户和应用，重置密码，轮换客户端密钥，强制退出，轮换和停用签名密钥，都调用对应的 `admin.*` 事件。

`admin.user_update` 的 detail 只记录变了什么，例如 `disabled`、`isAdmin`、`password: true`，不记录密码本身。`admin.app_secret` 不记录新密钥。

## 前端

继续现有的通行证视觉和朴素管理页，不新开一套风格。改完运行 `cd web && npm run typecheck && npm run build`，构建结果进入 `internal/webui/dist`。

### 账户页

在 `web/src/pages/Account.tsx` 增加两块：

- 登录设备。标出当前设备，允许退出其他设备，也允许一次退出其余全部。
- 已授权应用。显示应用名、最近使用时间和是否会保持登录。撤销前要确认，说明对方会在访问令牌过期或下次调用时被拒绝。

### 用户管理

`web/src/pages/Users.tsx`：

- 表格增加最近登录，从未登录显示“从未登录”。
- 搜索框过滤邮箱和名称，前端过滤即可，用户量是自托管规模。
- 行内或编辑框里增加“退出所有设备”。成功后用现有 toast。

### 应用管理

创建和编辑对话框增加访问范围：

- 所有用户。
- 仅管理员。
- 指定用户。选中后从 `/api/admin/users` 列出的用户里勾选。

卡片上用现有的小标签显示范围。

### 新页面

- `/admin/events` 操作记录。分组筛选、用户筛选、加载更早记录。时间用本地格式，类型显示中文而不是原始常量。
- `/admin/keys` 签名密钥。列表、生成新密钥、停用旧密钥。停用最后一把时按钮不可用，并说明原因。

导航在 `web/src/components/Shell.tsx`，管理员目前是“用户”“应用”。加上“记录”和“密钥”。

`web/src/api.ts` 补上对应类型和方法。错误仍走 `ApiError` 和服务器返回的中文 `message`。

## 测试

存储测试已经覆盖最后管理员和授权码单次使用。第二阶段至少再补这些端到端测试，风格沿用 `internal/server/*_test.go`：

- 登录后 `GET /api/sessions` 能看到当前设备，退出其他设备不会丢掉当前 cookie。
- 应用设为 `admins` 后，普通用户的授权请求被拒绝，管理员可以继续。
- 应用从 `all` 改成 `selected` 且不包含现有用户后，该用户已有的 access token 和 refresh token 失效。
- 管理员强制退出后，目标用户的会话和令牌失效，管理员自己的会话还在。
- 轮换密钥后，新 ID token 的 `kid` 是新密钥；停用旧密钥后，旧 access token 的 userinfo 失败，新 token 仍成功。
- 重放授权码和刷新令牌都会产生一条 `token.reuse`，并且不会把 token 明文写进日志。
- 非管理员调用任何一个新的管理接口都是 403，未登录是 401。

改完执行：

```bash
gofmt -l .
go test ./...
cd web && npm run typecheck && npm run build
```

浏览器手测不是必须，但如果改了登录或对话框布局，用第一阶段的方式看一眼：独立 Chrome 配置、真实二进制、桌面和 390 宽。

## 不要做

- 不初始化 git，不提交，除非用户明确要求。
- 不接入 xmail，不创建 `mailbox_shares`。
- 不加多组织、LDAP、社交登录、Passkey、验证码邮件。
- 不把审计日志发给外部系统。
- 不在接口或日志里返回密码、刷新令牌、授权码、客户端密钥、签名私钥。
- 不把签名私钥加密问题留到这一阶段解决。部署约束仍然是数据库文件只能由运行账户读取。

## 完成标准

1. `go test ./...` 通过，第一阶段原有测试没有倒退。
2. 前端类型检查和生产构建通过。
3. 管理员可以在界面上完成：查看记录、管理设备、设置应用访问范围、轮换和停用签名密钥、强制用户退出。
4. 普通用户只能看到和操作自己的设备与授权。
5. README 的“已知限制”里删掉“签名密钥不会自动轮换，也没有审计日志”，改成实际现在的行为：可以手动轮换，日志保留 180 天，没有外部投递。
