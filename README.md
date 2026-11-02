# XAuth

轻量自托管 OpenID Connect 身份提供方（OIDC Provider）。Go + Chi + SQLite（modernc，纯 Go，无 CGO），协议层基于 [zitadel/oidc](https://github.com/zitadel/oidc) v3，前端为 React + Tailwind，构建后嵌入单个二进制。

当前已完成第五阶段：邮箱密码登录、授权码 + PKCE、应用和用户管理、登录设备、操作记录、签名密钥轮换、对接 xmail 收信与邮箱分配、两步验证保险箱。

## 快速开始

需要 Go 1.26+ 和 Node 20.19+。

```bash
cd web && npm install && npm run build && cd ..   # 构建前端到 internal/webui/dist
go build -o bin/xauth ./cmd/xauth                 # 构建服务端（嵌入前端）

./bin/xauth create-admin -email you@example.com -name 你的名字
XAUTH_DEV=true ./bin/xauth                         # 本地开发：http://localhost:8080
```

`create-admin` 会提示输入密码。在 Git Bash（mintty）里提示会回显，可以改用 `winpty ./bin/xauth ...` 或设置 `XAUTH_ADMIN_PASSWORD`。邮箱已存在时，该命令只把账户提升为管理员，不改密码。

## 配置

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `XAUTH_ISSUER` | `http://localhost:8080` | 对外地址，不能带路径。生产环境必须是 https |
| `XAUTH_LISTEN` | `127.0.0.1:8080` | 监听地址 |
| `XAUTH_DB` | `data/xauth.db` | SQLite 文件 |
| `XAUTH_DEV` | `false` | 允许 http 的 issuer 和单页应用回调地址 |
| `XAUTH_SIGNUP` | `false` | 初始注册策略：`false` 表示仅限邀请，`true` 表示开放自助注册。管理员可随时在后台修改 |
| `XAUTH_TRUST_PROXY` | `false` | 限流时使用 `X-Real-IP` / `X-Forwarded-For`。只在反向代理会覆盖这些头时开启 |
| `XMAIL_BASE_URL` | 空 | xmail 部署地址（如 `https://mail.example.com`），不带 `/api/v1`。未配置时邮箱功能关闭 |
| `XMAIL_API_KEY` | 空 | xmail 的 API Key（`sk_live_...`）。仅保存在服务端环境变量中 |
| `XAUTH_SMTP_HOST` | 空 | 发信服务器。设置后才会出现邮箱验证和找回密码入口 |
| `XAUTH_SMTP_PORT` | 587（TLS）/ 25 | 发信端口 |
| `XAUTH_SMTP_USER` | 空 | SMTP 用户名，留空表示不认证 |
| `XAUTH_SMTP_PASS` | 空 | SMTP 密码 |
| `XAUTH_SMTP_FROM` | 空 | 发件地址，例如 `no-reply@example.com` |
| `XAUTH_SMTP_TLS` | `true` | 使用 STARTTLS。只在可信的内网 SMTP 上关闭 |

会话 14 天（活跃时顺延），访问令牌和 ID 令牌 1 小时，刷新令牌 30 天，授权码 2 分钟。这些时长在 `internal/config` 里修改。

## 注册与邀请

默认不开放公开注册。管理员在后台“注册与邀请”页选择注册模式：

- **关闭注册**：只能由管理员在“用户”页创建账户。这段时间邀请码不能兑换；改回邀请模式后，未过期的邀请码仍然有效。
- **仅限邀请**（默认）：只有持有邀请码的人能注册。邀请码在后台生成，可绑定邮箱、设置有效期（1–90 天，默认 7 天）和留言，也可以直接通过 SMTP 发送邀请邮件。邀请码只存哈希，创建后只显示一次，每个只能使用一次。
- **开放注册**：登录页的注册入口对所有人开放，不需要邀请码。

绑定了邮箱的邀请码默认不再发送验证邮件，注册后该地址直接标为已确认。未绑定邮箱的邀请码不会自动确认，避免被拿去注册任意地址。若希望被邀请人仍完成邮箱验证，可在后台打开“邀请注册需要邮箱验证”；此时注册后会通过 SMTP 发送验证链接。未配置 SMTP 时该开关不会生效。`XAUTH_SIGNUP` 只决定首次启动时的默认模式，之后以数据库中的设置为准。

## Docker

镜像发布在 GitHub Container Registry，推送到 `main` 标签为 `edge`，推送 `v*` 标签时按语义化版本发布，同时提供 `linux/amd64` 和 `linux/arm64`。

```bash
docker run -d --name xauth \
  -p 127.0.0.1:8080:8080 \
  -v xauth-data:/data \
  -e XAUTH_ISSUER=https://auth.example.com \
  -e XAUTH_TRUST_PROXY=true \
  ghcr.io/OWNER/REPO:edge
```

- 数据库在容器内是 `/data/xauth.db`，用卷持久化。容器以 UID 65532 运行；镜像里的 `/data` 已属于这个用户，所以新建的命名卷会自动带上正确属主。

```bash
docker volume create xauth-data
docker run --rm -v xauth-data:/data --user root --entrypoint /bin/sh \
  alpine:3.20 -c 'chown -R 65532:65532 /data'   # 仅旧卷属主不对时执行
```

- 创建第一个管理员：`docker exec -it xauth xauth create-admin -email you@example.com`（交互式），或加上 `-e XAUTH_ADMIN_PASSWORD=...` 走非交互。
- 镜像基于 distroless，没有 shell，也没有 gofmt 或 curl；排查时用 `--entrypoint /usr/local/bin/xauth` 覆盖入口。
- 健康检查地址是 `/healthz` 和 `/ready`。

## 接入应用

在管理后台“应用”页添加应用，然后把 Issuer 或 Discovery 地址填进应用的 OIDC 配置：

```
Issuer:     https://auth.example.com
Discovery:  https://auth.example.com/.well-known/openid-configuration
```

- **应用类型**：Web 应用有客户端密钥（`client_secret_basic` 或 `client_secret_post`）。单页应用和原生应用没有密钥。密钥只在创建或重新生成时显示一次。
- **PKCE 必须开启**：所有应用都要发送 `code_challenge_method=S256`。不带 PKCE 或使用 `plain` 的请求会以 `invalid_request` 退回应用。
- **只支持授权码流程**：`response_type=code`。不支持隐式流程、客户端凭据和设备码。
- **Scope**：`openid`、`profile`（`name`、`preferred_username`）、`email`（`email`、`email_verified`）、`offline_access`（刷新令牌，需要应用允许）。用户信息同时写入 ID 令牌和 userinfo。
- **访问令牌**是 RS256 签名的 JWT，可以用 `/keys` 本地验证。撤销后 userinfo 和 introspection 会立即拒绝，但本地验签的资源服务器要等令牌过期。
- **退出**：应用调用 `end_session` 并带上 `id_token_hint`，XAuth 会结束浏览器会话并跳回登记过的退出地址。不带 `id_token_hint` 时，用户需要在 XAuth 页面确认退出，且不会跳回应用。
- `email_verified` 取决于注册方式：开放注册、以及未绑定邮箱的邀请注册，默认是 false。绑定了邮箱且未开启“邀请注册需要邮箱验证”时为 true。管理员也可以手动标记“邮箱已确认”。
- 应用可以限制为所有用户、仅管理员或指定用户。收紧范围后，无权用户已获得的令牌会立即失效。
- 用户可以在账户页退出其他登录设备，也可以撤销某个应用的登录。
- 用户可以在“用户设置”里上传头像：支持 PNG、JPEG、WebP、GIF，原图最大 5 MB；浏览器会先裁成方形并压缩到 512px，服务端再按 1 MB 上限校验一次。头像只存进 SQLite，登录用户通过 `/api/users/{id}/avatar` 读取，未登录访问返回 401。
- 管理员可以在“记录”页查看最近 180 天的操作，在“密钥”页生成新的签名密钥并停用旧密钥。停用后，旧密钥签发的访问令牌立即失效。
- 管理员可以在“注册与邀请”页切换注册模式（关闭 / 仅限邀请 / 开放）、生成和撤销邀请码，并决定邀请注册是否要求邮箱验证。
- 管理员可以在“邮箱”页管理 xmail 邮箱、批量创建、设置域名与保留天数、绑定业务服务，并将邮箱分配给指定用户。
- 用户在账户页“我的邮箱”中查阅分配给自己的邮箱，收信、突出展示验证码、星标并以沙箱隔离形式安全预览富文本内容。未分配时普通用户无法访问，杜绝横向越权。
- 邮箱分配与解绑、管理员代看审计留痕（`mail.assign`、`mail.unassign`、`mail.admin_view`）；xmail API Key 仅保留在服务端环境变量中，绝不写入数据库、日志或前端包。
- 管理员可以在“验证器”页保存共享的 TOTP 密钥，并分配给指定用户。用户在“验证器”页看到当前验证码，也可以添加只有自己可见的私人验证器。添加时可以直接用摄像头扫描、上传二维码截图，或从剪贴板读取；二维码在浏览器本地解析。密钥用 AES-256-GCM 加密后保存，接口和操作记录都不返回明文。
- 配置 SMTP 后，用户可以发送一次性邮箱验证链接，也可以从登录页“忘记密码”重置密码。链接 30 分钟过期、只存哈希、只能用一次。重置成功后退出其他设备并撤销令牌。
- 账户页可以添加和删除通行密钥（WebAuthn）。登录页会提供“使用通行密钥”，密码登录继续保留。通行密钥登录会把 `amr` 记为 `webauthn`。

## 部署

1. 放在 HTTPS 反向代理后面，设置 `XAUTH_ISSUER=https://你的域名`。Cookie 会自动加 `Secure` 和 `__Host-` 前缀。
2. 数据库文件里有密码哈希和**未加密的令牌签名私钥**，只能让运行 XAuth 的账户读取，并定期备份（`sqlite3 xauth.db ".backup backup.db"`）。
3. 只运行一个实例。限流计数存在内存里，重启后清零。

## 开发

```bash
go test ./...        # 存储层测试 + 端到端 OIDC 测试（真实 HTTP，覆盖 PKCE、刷新轮换、重放检测、CSRF 等）
cd web && npm run dev   # 前端开发服务器 :5173，API 代理到 :8080 上的 XAUTH_DEV=true xauth
```

完整的 OIDC 流程（应用跳转到 `/authorize`）请用 :8080 上的构建版本测试，因为 discovery 里的地址指向 issuer。

目录：`cmd/xauth` 命令行，`internal/store` SQLite 存储与迁移，`internal/oidcprovider` zitadel/oidc 的存储适配，`internal/server` HTTP 路由、登录流程和管理 API，`web/` 前端。

## 安全设计

- 会话、授权码、刷新令牌和客户端密钥只存 SHA-256 哈希。密码用 bcrypt（cost 12）。
- 授权码只能兑换一次。重复使用会撤销第一次兑换签发的全部令牌。
- 刷新令牌每次使用都会轮换。旧令牌再次出现时，整组令牌全部撤销。
- 修改密码、管理员重置密码或停用账户会退出所有设备并撤销令牌。
- JSON API 只接受同源请求（`Sec-Fetch-Site` / `Origin` 校验，且必须是 `application/json`），Cookie 为 `HttpOnly` + `SameSite=Lax`。
- 前端页面使用严格 CSP（无内联脚本），禁止被嵌入 iframe。
- 登录失败按 IP 和邮箱限流，未知邮箱与错误密码耗时相同。

## 已知限制

- 邮箱验证和找回密码需要单独配置 `XAUTH_SMTP_*`，不使用 xmail 发信；未配置时这两个入口不显示，忘记密码仍由管理员重置。
- Passkey 只能登录，不是第二因素；账号仍保留密码，密码改不了时可以用通行密钥登录。
- 没有授权同意记录：应用都由管理员登记，视为可信，已登录用户会直接跳回应用。`prompt=consent` 和 `prompt=select_account` 会显示“以此身份继续”。
- 操作记录只保存在本地数据库，保留 180 天，不会发送到外部系统。

## 后续计划

后续计划见 `PLAN.md`：阶段 1–5 已完成，没有排期的后续功能。

## 许可证

[MIT](LICENSE)
