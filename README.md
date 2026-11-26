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
| `XMAIL_BASE_URL` | 空 | xmail 地址的初始值（如 `https://mail.example.com`），不带 `/api/v1`。也可在“系统设置”里填写，保存后立即生效并优先于环境变量 |
| `XMAIL_API_KEY` | 空 | xmail API Key 的初始值（`sk_live_...`）。后台保存后只存在数据库里，接口、日志和前端都不会再看到 |
| `XAUTH_SMTP_HOST` | 空 | 发信服务器的初始值。管理员也可在“系统设置”里填写，保存后立即生效并优先于环境变量 |
| `XAUTH_SMTP_PORT` | 587（TLS）/ 25 | 发信端口 |
| `XAUTH_SMTP_USER` | 空 | SMTP 用户名，留空表示不认证 |
| `XAUTH_SMTP_PASS` | 空 | SMTP 密码 |
| `XAUTH_SMTP_FROM` | 空 | 发件地址，例如 `no-reply@example.com` |
| `XAUTH_SMTP_TLS` | `true` | 使用 STARTTLS。只在可信的内网 SMTP 上关闭 |

会话 14 天（活跃时顺延），访问令牌和 ID 令牌 1 小时，刷新令牌 30 天，授权码 2 分钟。这些时长在 `internal/config` 里修改。

## 注册与邀请

默认不开放公开注册。管理员在后台“系统设置”页选择注册模式：

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
- 用独立保存的 `age` X25519 私钥加密数据库备份：备份机器只需公钥，私钥另行异地保管。先安装 [`age`](https://github.com/FiloSottile/age)，创建恢复密钥并单独保管：

```bash
age-keygen -o xauth-recovery.key
age-keygen -y xauth-recovery.key > xauth-recovery.pub
```

然后在能访问 SQLite 数据库文件的受信任机器上创建在线、一致性快照。恢复到**新的**数据库路径，先验证隔离实例，再切换 `XAUTH_DB` 并重启服务。现有文件不会被覆盖；恢复命令会校验解密和 SQLite 完整性，再原子发布文件。Docker 环境可在运行中的实例内创建备份，只复制密文离开数据卷；恢复到隔离实例时仍使用原数据卷中的另一个新数据库文件：

```bash
# Linux：备份当前在运行的、采用 WAL 模式的数据库
./xauth backup -output xauth-2026-11-20.db.age -recipient "$(cat xauth-recovery.pub)"

# Windows PowerShell
$recipient = Get-Content -Raw .\xauth-recovery.pub
.\xauth.exe backup -output .\xauth-backup.age -recipient $recipient.Trim()

# Docker：从正在运行的容器生成密文，再只复制密文到异地
docker exec -e XAUTH_BACKUP_RECIPIENT="$(cat xauth-recovery.pub)" xauth \
  /usr/local/bin/xauth backup -output /data/xauth-2026-11-20.db.age
docker cp xauth:/data/xauth-2026-11-20.db.age ./xauth-2026-11-20.db.age

# 先恢复并启动独立实例，验证账号登录、邮件凭据解密与 OIDC 签名密钥
./xauth restore -input xauth-2026-11-20.db.age -identity xauth-recovery.key -db restore/xauth.db
XAUTH_DB=restore/xauth.db XAUTH_ISSUER=https://auth.example.com ./xauth serve

# 在隔离容器中演练恢复；restore.db 是新路径，且仅映射到本机 18081
docker cp ./xauth-2026-11-20.db.age xauth:/data/recovery.age
docker run --rm --user root --volumes-from xauth \
  -v "$PWD/xauth-recovery.key:/run/secrets/xauth-recovery.key:ro" \
  --entrypoint /usr/local/bin/xauth ghcr.io/OWNER/REPO:edge restore \
  -input /data/recovery.age -identity /run/secrets/xauth-recovery.key -db /data/restore.db
# The one-shot restore runs as root so it can read the 0600 host key. Reassign
# only the new restored database to XAuth's runtime UID before the drill.
docker run --rm --user root --volumes-from xauth alpine:3.20 chown 65532:65532 /data/restore.db
docker run -d --name xauth-restore-check -p 127.0.0.1:18081:8080 \
  --volumes-from xauth \
  -v "$PWD/xauth-recovery.key:/run/secrets/xauth-recovery.key:ro" \
  -e XAUTH_LISTEN=0.0.0.0:8080 -e XAUTH_ISSUER=https://auth.example.com \
  -e XAUTH_DB=/data/restore.db --entrypoint /usr/local/bin/xauth \
  ghcr.io/OWNER/REPO:edge serve
# 验证恢复实例能够就绪、登录、读取邮件凭据并签发 OIDC 后，再退出它。
docker rm -f xauth-restore-check
```

恢复密钥**不能和密文备份或运行数据库保存在同一个卷/位置**；丢失它就无法恢复密文中的数据库。恢复命令只写入尚不存在的新数据库路径；演练通过后，再停掉服务并显式改用验证过的数据库文件。不要在服务仍运行时替换其数据库。

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
- 管理员在“系统设置”里切换注册模式（关闭 / 仅限邀请 / 开放）、配置 SMTP，并决定邀请注册是否要求邮箱验证。邀请码仍在“注册与邀请”页生成和撤销。
- 管理员可以在“邮箱”页批量导入格式化的 Outlook 邮箱（`邮箱----密码----client_id----refresh_token`，每行一个）、按分组管理、导出备份，并将邮箱单个或批量分配给指定用户；也可对接 xmail 管理收信邮箱、批量创建、设置域名与保留天数、绑定业务服务。
- 管理员“邮箱操作台”（`/admin/mail-console`）面向代用户注册服务：选择 xmail 邮箱后缀后点击“创建新的”，每次只创建并使用一个邮箱，直接显示 xmail 自动生成的地址、密码和邮件列表，支持同页读信、复制验证码及每 10 秒自动收信（后台暂停）。“创建新的”不会删除旧邮箱；“删除此邮箱”只删除当前邮箱并清空工作区，不会自动新建或切换。旧邮箱仍可在邮件中台管理，删除的邮箱可在回收站恢复。当前邮箱凭据仅保留于页面内存，离开或刷新不会删除邮箱。
- 用户在账户页“我的邮箱”中查阅管理员分配给自己的 Outlook 或 xmail 邮箱（普通用户不可自行添加或修改邮箱，只能查阅被分配邮箱），支持 Graph API / 新版 IMAP / 旧版 IMAP 自动降级收信、自动提取并突出展示 4–8 位验证码、星标及沙箱隔离富文本预览。Outlook 密码与 `refresh_token` 使用 AES-256-GCM 加密存储，永不向普通用户暴露。
- 邮箱分配与解绑、管理员代看审计留痕（`mail.assign`、`mail.unassign`、`mail.admin_view`）。xmail 地址和 API Key 可以在“系统设置”里填写；Key 只存在服务端（环境变量或数据库），不会进入日志、接口响应或前端包。
- 管理员可以在“验证器”页保存共享的 TOTP 密钥，并分配给指定用户。用户在“验证器”页看到当前验证码，也可以添加只有自己可见的私人验证器。添加时可以直接用摄像头扫描、上传二维码截图，或从剪贴板读取；二维码在浏览器本地解析。密钥用 AES-256-GCM 加密后保存，接口和操作记录都不返回明文。
- 验证码使用 `pquerna/otp` 按 RFC 6238 生成。粘贴链接和扫描二维码会读取链接中的算法、位数和周期；支持 SHA1/SHA256/SHA512、6–8 位和 1–86400 秒，缺省为 SHA1/6 位/30 秒。错误或不支持的参数会被拒绝。页面根据服务器返回的有效期刷新，断网时过期码会隐藏并停止复制，恢复网络或返回页面后重新取码。部署机器需保持系统时间同步。早先导入时被默认值覆盖的条目，需要用原始链接或二维码重新导入参数。
- 配置 SMTP 后，用户可以发送一次性邮箱验证链接，也可以从登录页“忘记密码”重置密码。链接 30 分钟过期、只存哈希、只能用一次。重置成功后退出其他设备并撤销令牌。
- 账户页可以添加和删除通行密钥（WebAuthn）。登录页会提供“使用通行密钥”，密码登录继续保留。通行密钥登录会把 `amr` 记为 `webauthn`。

## 部署

1. 放在 HTTPS 反向代理后面，设置 `XAUTH_ISSUER=https://你的域名`。Cookie 会自动加 `Secure` 和 `__Host-` 前缀。
2. 数据库文件包含密码哈希、令牌签名私钥，以及用于加密 TOTP 和 Outlook 凭据的密钥；**必须把 SQLite 数据库和 WAL 文件当作敏感凭据文件保护**。定期用 `xauth backup` 生成 age 加密的一致性快照，将密文与恢复私钥分别异地保存，并定期恢复到隔离实例演练验证。
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

- 邮箱验证和找回密码使用 SMTP，不走 xmail。可以在“系统设置”里填写，或事先配置 `XAUTH_SMTP_*` 作为初始值；都没配时这两个入口不显示，忘记密码仍由管理员重置。后台保存的 SMTP 密码存在数据库里，接口不再返回。
- Passkey 只能登录，不是第二因素；账号仍保留密码，密码改不了时可以用通行密钥登录。
- 首次授权应用、请求尚未同意的权限时会显示确认页；同一用户已同意的权限在后续登录中直接复用。`prompt=consent` 每次都会要求确认，`prompt=select_account` 会要求用户确认当前账户。用户撤销应用授权后，下次登录需要重新确认。
- 操作记录只保存在本地数据库，保留 180 天，不会发送到外部系统。

## 后续计划

后续计划见 `PLAN.md`：阶段 1–5 已完成，没有排期的后续功能。

## 许可证

[MIT](LICENSE)
