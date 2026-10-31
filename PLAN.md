# XAuth 全部计划

给下一个 agent 的总计划。按阶段做，不要跳到后面的阶段，也不要自行扩大范围。

第二阶段的逐步清单、已改文件和当前编译断点在 `PHASE2.md`。本文说清四阶段边界；做到第二阶段时以 `PHASE2.md` 为准。

## 现在的状态

第一阶段已完成。项目不是 git 仓库，没有提交。

- 后端：Go 1.26、Chi、`modernc.org/sqlite`、`github.com/zitadel/oidc/v3` v3.51.16。
- 前端：React 19、Vite、Tailwind 4，构建后嵌入 `internal/webui/dist`。
- 能力：邮箱密码注册登录、授权码 + 强制 PKCE S256、应用管理、用户管理、账户页。
- 验证：`go test ./...` 在第二阶段改存储签名之前是绿的。现在服务端编译失败，原因和修复写在 `PHASE2.md`。
- 明确不做：多组织、LDAP、社交登录。xmail 和两步验证不要提前做。

## 阶段顺序

| 阶段 | 内容 | 状态 |
| --- | --- | --- |
| 1 | 登录和 OIDC | 完成 |
| 2 | 管理后台补全 | 完成 |
| 3 | 对接精简后的 xmail，并在 XAuth 里分配邮箱 | 完成 |
| 4 | 两步验证保险箱 | 完成 |
| 5 | Passkey、邮箱验证、界面收尾 | 完成 |

用户口头说的“第三阶段 xmail、第四阶段 Passkey”拆成了上面的 3、4、5。两步验证跟 xmail 一起提过，但它引用旧版 xmail，而且当前 xmail 已经没有这套表，所以单独成阶段 4，放在邮箱分配能用之后。

## 阶段 1：已完成，不要重写

保持这些行为：

- 只支持 `response_type=code` 和 `code_challenge_method=S256`。公开客户端没有密钥，Web 客户端用 Basic 或 POST 密钥。
- Discovery 只公布授权码、刷新令牌和 S256。实现在 `internal/server/discovery.go`。
- 授权码和刷新令牌只存哈希，重复使用会撤销整组令牌。
- 密码 bcrypt cost 12。Cookie 是 HttpOnly、SameSite=Lax，HTTPS 下用 `__Host-`。
- JSON API 拒绝跨站请求，管理接口只给管理员。
- 没有邮件验证。`email_verified` 默认 false，管理员可手动勾选。
- 登录页是通行证样式。管理页保持朴素表格。文案用简体中文。
- 签名私钥明文在 SQLite 里。部署要求是限制数据库文件权限，不在本计划里做静态加密。

## 阶段 2：管理后台

目标、接口、页面、测试和当前断点全部在 `PHASE2.md`。完成后才进入阶段 3。

完成后用户能看到自己的登录设备和已授权应用。管理员能看操作记录、限制谁能登录某个应用、轮换和停用签名密钥、强制用户退出。

## 阶段 3：对接 xmail

### 现状

精简后的 xmail 在 `G:\0JHX-code\Project\xmail`。它是 Cloudflare Workers + D1 的收信系统，后台能力已经收成一套 API Key 调用的 REST API。完整清单以它的 `API.md` 和 `docs/INTEGRATION.md` 为准。

当前 xmail 没有普通用户表，也没有邮箱分配。`packages/database/schema.ts` 只有管理员、邮箱、邮件、日志、设置、服务模板、邮箱服务和 API Key。分配关系必须存在 XAuth，不回写到 xmail 的用户体系。

认证是：

```http
Authorization: Bearer sk_live_xxx.yyy
```

Base URL 是 `https://<xmail域名>/api/v1`。响应是 `{ "success": true, "data": ... }` 或 `{ "success": false, "error": "..." }`。

XAuth 管理端要通过自己的后台覆盖这些能力：邮箱、邮件、验证码、服务模板、回收站、统计、域名、设置、日志。用户端只使用分配给自己的邮箱，不接触 xmail 的 API Key，也不看到系统设置、域名、日志和别人的邮箱。

### XAuth 配置

新增环境变量：

| 变量 | 说明 |
| --- | --- |
| `XMAIL_BASE_URL` | 例如 `https://mail.example.com`，不要带 `/api/v1` |
| `XMAIL_API_KEY` | xmail 的 `sk_live_...`。只放在服务端环境变量里 |

未配置时，邮箱菜单显示“尚未连接 xmail”，其余 XAuth 功能照常。不要把 Key 写进数据库、前端包、日志或审计 detail。

### 分配模型

在 XAuth 的下一次迁移里新增：

```text
mailbox_grants (
  mailbox_address TEXT NOT NULL,
  user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  assigned_by     TEXT NOT NULL,
  assigned_at     INTEGER NOT NULL,
  PRIMARY KEY (mailbox_address, user_id)
)
```

地址统一小写保存。一个邮箱可以分给多人，一个人可以有多个邮箱。删除 XAuth 用户时级联删除分配。xmail 里删除邮箱后，分配行保留到下次访问返回 404，然后管理员界面提供清理，不在 xmail 侧做回调。

### 服务端边界

新增 `internal/xmail` 客户端，只由 Go 服务端调用。浏览器永远请求 XAuth 的 `/api/...`。

- 管理员路由 `/api/admin/mail/*` 原样代理 xmail 的 `/api/v1/*`，但只允许 `API.md` 里列出的方法和路径。拒绝任意 URL、拒绝外部域名。
- 用户路由 `/api/mail/*` 先检查 `mailbox_grants`。没有分配就返回 404，避免用 403 泄露邮箱是否存在。
- 用户不能调用：创建和删除邮箱、读取或重置明文密码、域名、系统设置、操作日志、回收站清空、服务模板全局管理。
- 用户可以调用自己邮箱上的：邮件列表、详情、已读、星标、验证码、统计、该邮箱已挂的服务列表。
- 管理员分配、取消分配和代为查看时写 XAuth 审计。事件名用 `mail.assign`、`mail.unassign`、`mail.admin_view`。不要记录邮件正文。

代理要限制响应大小和超时。xmail 的错误原文可以返回给管理员；返回给普通用户时改成统一中文，避免把 xmail 内部信息带出来。

### 管理界面

新增 `/admin/mail`，沿用现有管理页风格：

- 邮箱列表、搜索、创建、批量创建、备注、有效期、停用、软删除和恢复。
- 邮箱详情里分配用户。用户来自 XAuth 用户列表，多选。
- 邮件阅读、验证码、服务模板和回收站放在二级页面，按 `API.md` 的能力做，不把 MCP 端点暴露到界面。
- 系统域名、保留天数和 xmail 日志只放在“邮件系统”设置页，仅管理员可见。

### 用户界面

账户页增加“我的邮箱”。只列出分配给当前用户的地址。点进去是收件箱：列表、未读、验证码突出显示、星标和邮件详情。没有分配时显示空状态，不提供创建入口。

### 测试

- 未配置 xmail 时，邮箱接口返回明确的“未连接”，登录和 OIDC 不受影响。
- 用 `httptest` 模拟 xmail。普通用户只能读到分配给自己的邮箱，猜另一个地址得到 404。
- 管理员分配和解绑后，用户列表立即变化。
- 前端包和审计 JSON 里搜索不到 `XMAIL_API_KEY`。
- 代理拒绝 `..`、绝对 URL 和未登记路径。

## 阶段 4：两步验证保险箱

这不是 XAuth 登录的第二因素，也不改变 OIDC。它是旧版 xmail 的验证器：保存 TOTP 密钥，生成当前验证码，管理员可以分配给用户，用户也可以给自己添加。

参考实现，只读，不要把旧项目拷进 XAuth：

- `G:\0JHX-code\Project\xmail-old\packages\database\schema.ts` 里的 `two_factor_entries`、`user_two_factor_entries`。
- `G:\0JHX-code\Project\xmail-old\packages\database\dao.ts` 里的创建、更新、删除、分配函数。
- `G:\0JHX-code\Project\xmail-old\apps\web\src\lib\totp.ts`：Base32、`otpauth://` 解析、SHA1/SHA256/SHA512、6 到 8 位、周期默认 30 秒。
- 旧界面：`apps/web/src/pages/authenticator.astro`、`components/totp/QrImportPanel.tsx`、`ManualTotpGenerator.tsx`、`AssignedTotpList.tsx`。

### 数据

XAuth 迁移新增：

```text
totp_entries (
  id               TEXT PRIMARY KEY,
  owner_user_id    TEXT REFERENCES users(id) ON DELETE CASCADE,
  name             TEXT NOT NULL,
  issuer           TEXT NOT NULL DEFAULT '',
  account_name     TEXT NOT NULL DEFAULT '',
  secret_encrypted BLOB NOT NULL,
  digits           INTEGER NOT NULL DEFAULT 6,
  period           INTEGER NOT NULL DEFAULT 30,
  algorithm        TEXT NOT NULL DEFAULT 'SHA1',
  note             TEXT NOT NULL DEFAULT '',
  created_by       TEXT NOT NULL,
  created_at       INTEGER NOT NULL,
  updated_at       INTEGER NOT NULL
)

totp_grants (
  entry_id    TEXT NOT NULL REFERENCES totp_entries(id) ON DELETE CASCADE,
  user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  assigned_by TEXT NOT NULL,
  assigned_at INTEGER NOT NULL,
  PRIMARY KEY (entry_id, user_id)
)
```

`owner_user_id` 为空表示管理员创建的共享条目。非空表示用户自己添加的私人条目，只能所有者看到，管理员也不能分配。

密钥用 AES-256-GCM 加密。密钥材料放在 `settings` 表，沿用 `Store.Setting` 第一次生成 32 字节的方式，key 名 `totp_key`。接口和审计不返回密钥明文。列表接口只在生成验证码时解密。

### 行为

- 管理员在 `/admin/totp` 添加、粘贴 `otpauth://` 或 Base32、编辑、删除共享条目，并勾选分配给哪些用户。
- 用户在 `/account/totp` 看到分配给自己的共享条目和自己的私人条目。可以添加、编辑、删除私人条目。不能修改共享条目。
- 验证码每 30 秒刷新，显示剩余秒数。算法和参数以条目为准。
- 删除用户时，私人条目和分配关系级联删除。共享条目保留。

### 测试

- 同一密钥、固定时间，生成的验证码与 RFC 6238 的 SHA1 测试向量一致。
- 用户看不到别人的私人条目，也不能读未分配的共享条目。
- 管理员分配后，目标用户能生成验证码。取消分配后立即不能。
- 数据库里的 `secret_encrypted` 不等于原始 Base32。

## 阶段 5：账户安全和界面收尾

阶段 4 完成后再做。每一项都可以单独交付。

### 邮箱验证

- 用户注册后 `email_verified` 仍是 false。
- 用本系统的 SMTP 配置发验证信，不借 xmail 发信。xmail 是收信，不是 XAuth 的发信通道。
- 验证链接一次性、30 分钟过期、只存哈希。
- 管理员仍可手动标记已验证。未验证用户可以登录，但 ID token 里 `email_verified` 必须是 false。

### 密码重置

- 登录页增加“忘记密码”。发送重置链接，不透露邮箱是否存在。
- 重置后沿用现有逻辑：退出其他设备并撤销令牌。
- 没有 SMTP 配置时，入口不显示，仍由管理员重置。

### Passkey

- 用 WebAuthn。用户在账户页注册和删除自己的通行密钥。
- 登录页在已有 Passkey 时优先提供通行密钥，密码保留。
- OIDC 的 `amr` 从现在的 `pwd` 增加 `webauthn`。Passkey 登录同样更新 `last_login_at` 和会话。
- 不把 Passkey 做成唯一登录方式。最后一种登录方式不能删掉。

### 界面

不换视觉体系。只在前几个阶段的页面上补齐空状态、加载、窄屏和错误文案。登录通行证、管理表格、中文文案保持不变。

## 始终遵守

- 不初始化 git，不提交，除非用户明确要求。
- 不把 xmail、两步验证或 Passkey 混进当前未完成的阶段。
- 浏览器不持有 xmail API Key、TOTP 密钥、客户端密钥或签名私钥。
- 新管理接口沿用登录会话、管理员中间件和同源 JSON 校验。
- 新用户接口只能访问自己的会话、设备、授权、邮箱分配和私人验证器。
- 改完运行 `gofmt -l .`、`go test ./...`、`cd web && npm run typecheck && npm run build`。
- 每个阶段结束时更新 `README.md` 的能力列表和已知限制，不提前把未做的功能写成已经支持。
