import { useEffect, useState } from "react";
import {
  api,
  errorMessage,
  type App,
  type AuditEvent,
  type DeviceSession,
  type Grant,
  type MyMailbox,
  type Passkey,
  type TotpEntry,
  type User,
} from "../api";
import { useToast } from "../components/Dialog";
import { GuillocheBand, Rosette, seedFrom } from "../components/Guilloche";
import { Icon, type IconName } from "../components/Icon";
import { MailViewer } from "../components/MailViewer";
import { Stamp } from "../components/Pass";
import { AppShell } from "../components/Shell";
import { Badge, Button, Copyable, Empty } from "../components/ui";
import { Link } from "../router";
import { displayName, formatDate, useSession } from "../session";

function deviceName(userAgent: string) {
  const browser = /Edg\//.test(userAgent)
    ? "Edge"
    : /Chrome\//.test(userAgent)
      ? "Chrome"
      : /Firefox\//.test(userAgent)
        ? "Firefox"
        : /Safari\//.test(userAgent)
          ? "Safari"
          : "浏览器";
  const os = /Windows/.test(userAgent)
    ? "Windows"
    : /Mac OS/.test(userAgent)
      ? "macOS"
      : /Android/.test(userAgent)
        ? "Android"
        : /iPhone|iPad/.test(userAgent)
          ? "iOS"
          : "未知系统";
  return `${browser} · ${os}`;
}

/** Wide 56-character MRZ lines tailored for the horizontal diplomatic pass. */
function wideMrz(name: string, id: string, role: string): [string, string] {
  const clean = (s: string) =>
    s
      .normalize("NFKD")
      .toUpperCase()
      .replace(/[^A-Z0-9]+/g, "<")
      .replace(/^<+|<+$/g, "");
  const pad = (s: string) => (s + "<".repeat(58)).slice(0, 58);
  const digits = id.toUpperCase().replace(/[^A-Z0-9]/g, "");
  const value = (ch: string) => (/\d/.test(ch) ? Number(ch) : ch.charCodeAt(0) - 55);
  const check = [...digits.slice(0, 16)].reduce((sum, ch, i) => sum + value(ch) * [7, 3, 1][i % 3], 0) % 10;
  return [
    pad(`P<XAU${role}<<${clean(name) || "HOLDER"}<<<<<<<<<<<<<<<<<<<<`),
    pad(`${digits.slice(0, 16)}${check}<<OIDC<1<0<<PKCE<S256<<SOVEREIGN<PASS`),
  ];
}

function eventDotTone(type: string) {
  if (/failure|denied|reuse/.test(type)) return "bg-void ring-4 ring-void/15";
  if (/delete|retire|signout|revoke/.test(type)) return "bg-[#c47a1a] ring-4 ring-[#c47a1a]/15";
  if (type.startsWith("admin.") || type.startsWith("totp.") || type.startsWith("mail.")) return "bg-stamp ring-4 ring-stamp/15";
  if (/success|signin|register/.test(type)) return "bg-seal ring-4 ring-seal/15";
  return "bg-ink-faint ring-4 ring-ink/10";
}

const eventTypeLabels: Record<string, string> = {
  "login.success": "登录成功",
  "login.failure": "登录失败",
  "user.register": "注册账户",
  logout: "退出登录",
  "password.change": "更改密码",
  "session.revoke": "退出设备",
  "app.signin": "登录应用",
  "app.denied": "拒绝访问应用",
  "grant.revoke": "撤销应用授权",
  "token.reuse": "检测到令牌重放",
  "admin.user_create": "添加用户",
  "admin.user_update": "修改用户",
  "admin.user_signout": "强制用户退出",
  "admin.app_create": "添加应用",
  "admin.app_update": "修改应用",
  "admin.app_delete": "删除应用",
  "admin.app_secret": "重新生成应用密钥",
  "admin.key_rotate": "生成签名密钥",
  "admin.key_retire": "停用签名密钥",
  "totp.create": "添加验证器",
  "totp.update": "修改验证器",
  "totp.delete": "删除验证器",
  "totp.assign": "分配验证器",
};

const shortTime = new Intl.DateTimeFormat("zh-CN", {
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
});

/**
 * Tier 1: Sovereign Identity Pass with Authorized External Apps on the right.
 */
function SovereignDossierHero({
  user,
  issuer,
  sessions,
  passkeys,
  grants,
  onReload,
}: {
  user: User;
  issuer: string;
  sessions: DeviceSession[] | null;
  passkeys: Passkey[] | null;
  grants: Grant[] | null;
  onReload: () => void;
}) {
  const toast = useToast();
  const s = seedFrom(user.id);
  const name = displayName(user);
  const mark = name.slice(0, 1).toUpperCase();
  const mrzLines = wideMrz(user.email.split("@")[0], user.id, user.isAdmin ? "ADM" : "USR");
  const currentDevice = sessions?.find((d) => d.current) ?? sessions?.[0];
  const passkeyCount = passkeys?.length ?? 0;
  const serial = `XAU-${user.id.slice(0, 4).toUpperCase()}-${user.id.slice(-4).toUpperCase()}`;

  const revokeApp = async (row: Grant) => {
    if (!window.confirm(`撤销应用「${row.clientName}」的授权？`)) return;
    try {
      await api.revokeGrant(row.clientId);
      toast("已撤销应用授权");
      onReload();
    } catch (e) {
      toast(errorMessage(e));
    }
  };

  return (
    <article className="pass relative overflow-hidden">
      <div className="grid lg:grid-cols-[minmax(0,1fr)_320px]">
        {/* Left: Main Diplomatic Pass */}
        <div className="flex min-w-0 flex-col justify-between">
          <header className="relative overflow-hidden bg-[linear-gradient(180deg,#fff_0%,rgb(90_63_192/0.045)_100%)] px-6 pt-6 pb-5 sm:px-8">
            <GuillocheBand seed={s} className="pointer-events-none absolute inset-x-0 top-0 h-full w-full" />

            <div className="relative flex items-center justify-between gap-4">
              <div className="flex min-w-0 items-center gap-4 sm:gap-5">
                <Rosette seed={s} mark={mark} className="size-16 sm:size-[4.5rem]" />
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="eyebrow !text-stamp">IDENTITY CREDENTIAL · 身份通行证</span>
                    <span className="rounded border border-line-strong/80 bg-white/80 px-1.5 py-0.5 font-mono text-[0.65rem] font-semibold tracking-wider text-ink-soft">
                      {serial}
                    </span>
                  </div>
                  <h2 className="display mt-1.5 truncate text-[1.5rem] leading-tight sm:text-[1.875rem]" title={name}>
                    {name}
                  </h2>
                  <div className="mt-2 flex flex-wrap items-center gap-2">
                    {user.emailVerified ? <Badge tone="seal" dot>邮箱已核验</Badge> : <Badge tone="void" dot>邮箱未验证</Badge>}
                    {user.isAdmin ? <Badge tone="ink">系统管理员 · ADMIN</Badge> : <Badge>正式成员 · MEMBER</Badge>}
                    {passkeyCount > 0 && <Badge tone="stamp">{passkeyCount} 把通行密钥</Badge>}
                  </div>
                </div>
              </div>

              <div className="hidden shrink-0 sm:block">
                <Stamp
                  tone={user.emailVerified ? "issued" : "void"}
                  label={user.emailVerified ? "已签发 · VALID" : "待验证 · UNVERIFIED"}
                  detail={new URL(issuer).host.toUpperCase()}
                />
              </div>
            </div>
          </header>

          {/* 4-Cell Cryptographic Data Matrix */}
          <dl className="grid divide-y divide-line/80 border-t border-line bg-white/65 sm:grid-cols-2 sm:divide-y-0 xl:grid-cols-4 xl:divide-x">
            <div className="px-6 py-4 sm:border-r sm:border-b sm:border-line/80 sm:px-7 xl:border-b-0">
              <dt className="eyebrow flex items-center justify-between">
                <span>01 / 电子邮箱</span>
                <Icon name="mail" className="size-3.5 text-ink-faint" />
              </dt>
              <dd className="mt-2 truncate font-mono text-[0.84375rem] font-semibold text-ink" title={user.email}>
                {user.email}
              </dd>
              <dd className="mt-1 text-xs text-ink-soft">
                {user.emailVerified ? "可用于找回密码与安全通知" : "请前往设置完成邮件验证"}
              </dd>
            </div>

            <div className="px-6 py-4 sm:border-b sm:border-line/80 sm:px-7 xl:border-b-0">
              <dt className="eyebrow flex items-center justify-between">
                <span>02 / 认证凭证</span>
                <Icon name="fingerprint" className="size-3.5 text-ink-faint" />
              </dt>
              <dd className="mt-2 text-[0.90625rem] font-semibold text-ink">
                {passkeyCount > 0 ? `密码 + ${passkeyCount} 个通行密钥` : "仅密码认证"}
              </dd>
              <dd className="mt-1 text-xs text-ink-soft">
                <Link href="/settings" className="inline-flex items-center gap-1 font-medium text-stamp hover:underline">
                  {passkeyCount > 0 ? "管理密钥与密码" : "立即绑定通行密钥"}
                  <Icon name="chevron" className="size-3" />
                </Link>
              </dd>
            </div>

            <div className="px-6 py-4 sm:border-r sm:border-line/80 sm:px-7">
              <dt className="eyebrow flex items-center justify-between">
                <span>03 / 当前终端</span>
                <span className="relative flex size-2">
                  <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-seal opacity-75" />
                  <span className="relative inline-flex size-2 rounded-full bg-seal" />
                </span>
              </dt>
              <dd className="mt-2 truncate text-[0.90625rem] font-semibold text-ink">
                {currentDevice ? deviceName(currentDevice.userAgent) : "当前浏览器会话"}
              </dd>
              <dd className="mt-1 truncate font-mono text-xs text-ink-soft">
                IP: {currentDevice?.ip || "本地环回"} · 共 {sessions?.length ?? 1} 台在线
              </dd>
            </div>

            <div className="px-6 py-4 sm:px-7">
              <dt className="eyebrow flex items-center justify-between">
                <span>04 / 签发与活跃</span>
                <Icon name="clock" className="size-3.5 text-ink-faint" />
              </dt>
              <dd className="mt-2 font-mono text-[0.875rem] font-semibold tabular-nums text-ink">
                {user.lastLoginAt ? formatDate(user.lastLoginAt) : formatDate(user.createdAt)}
              </dd>
              <dd className="mt-1 text-xs text-ink-soft tabular-nums">
                首次签发于 {formatDate(user.createdAt)}
              </dd>
            </div>
          </dl>

          {/* Bottom Machine Readable Zone */}
          <div
            aria-hidden="true"
            className="flex items-center justify-between gap-4 overflow-hidden border-t border-dashed border-line-strong bg-[#f4f5f1] px-6 py-2.5 font-mono text-[0.625rem] leading-[1.65] tracking-[0.12em] text-ink-soft/80 [font-stretch:75%] sm:px-8"
          >
            <div className="truncate whitespace-pre">
              {mrzLines[0]}
              {"\n"}
              {mrzLines[1]}
            </div>
            <div className="hidden shrink-0 text-right tracking-[0.15em] text-ink-faint md:block">
              <div>PKCE · S256</div>
              <div>JWKS · RS256</div>
            </div>
          </div>
        </div>

        {/* Right: Authorized External Apps Panel */}
        <div className="relative flex flex-col justify-between border-t-[1.5px] border-dashed border-line-strong bg-[linear-gradient(180deg,#fafaf7_0%,#f3f4ef_100%)] p-6 lg:border-t-0 lg:border-l-[1.5px]">
          {/* Top & Bottom Semicircular Punch Notches on Desktop */}
          <span
            aria-hidden="true"
            className="pointer-events-none absolute -top-2.5 -left-2.5 hidden size-5 rounded-full border border-line bg-desk lg:block"
          />
          <span
            aria-hidden="true"
            className="pointer-events-none absolute -bottom-2.5 -left-2.5 hidden size-5 rounded-full border border-line bg-desk lg:block"
          />

          <div className="flex flex-1 flex-col">
            <div className="flex items-center justify-between gap-2">
              <div className="flex items-center gap-2">
                <Icon name="apps" className="size-4 text-stamp" />
                <h3 className="text-sm font-semibold text-ink">已授权的外部应用</h3>
              </div>
              <Badge tone="plain">{grants?.length ?? 0} 个</Badge>
            </div>
            <p className="mt-1 text-xs text-ink-soft">通过 OIDC 协议连接的应用通行许可</p>

            <div className="mt-3.5 flex flex-1 flex-col justify-center">
              {!grants || grants.length === 0 ? (
                <div className="rounded-xl border border-dashed border-line-strong bg-white/60 px-3.5 py-4 text-center text-xs leading-5 text-ink-soft">
                  <Icon name="apps" className="mx-auto mb-1.5 size-4 text-ink-faint" />
                  当前还没有外部应用保持登录授权。
                </div>
              ) : (
                <div className="divide-y divide-line/80 rounded-xl border border-line bg-white/80 px-3">
                  {grants.slice(0, 3).map((row) => (
                    <div key={row.clientId} className="flex items-center justify-between gap-2 py-2.5">
                      <div className="min-w-0">
                        <div className="flex items-center gap-1.5">
                          <span className="truncate text-xs font-semibold text-ink">{row.clientName}</span>
                          {row.refresh && <Badge tone="stamp">持久</Badge>}
                        </div>
                        <p className="mt-0.5 text-[0.6875rem] text-ink-soft">最近使用 {formatDate(row.lastUsed)}</p>
                      </div>
                      <Button variant="ghost" size="sm" className="!h-7 !px-2 text-xs hover:text-void" onClick={() => revokeApp(row)}>
                        撤销
                      </Button>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>

          <div className="mt-5 border-t border-dashed border-line-strong pt-4">
            <Link
              href="/settings"
              className="group flex w-full items-center justify-center gap-2 rounded-xl bg-ink px-4 py-2.5 text-[0.8125rem] font-semibold text-paper shadow-xs transition-all hover:bg-[#29335c] active:translate-y-px"
            >
              <Icon name="settings" className="size-4 transition-transform duration-300 group-hover:rotate-45" />
              账户与安全设置
              <Icon name="chevron" className="size-3.5 opacity-75 transition-transform group-hover:translate-x-0.5" />
            </Link>
          </div>
        </div>
      </div>
    </article>
  );
}

/**
 * Tier 2: 4-Column Numbered Tactile Command Deck
 */
function CommandTile({
  index,
  href,
  icon,
  title,
  metric,
  metricLabel,
  description,
}: {
  index: string;
  href: string;
  icon: IconName;
  title: string;
  metric: string | number;
  metricLabel: string;
  description: string;
}) {
  return (
    <Link
      href={href}
      className="panel group relative flex flex-col justify-between overflow-hidden bg-[linear-gradient(180deg,#fff_0%,#fcfcfa_100%)] p-5 transition-[border-color,box-shadow,transform] duration-150 hover:-translate-y-0.5 hover:border-stamp/45 hover:shadow-[0_0_0_3px_rgb(90_63_192/0.08),0_14px_28px_-12px_rgb(27_35_64/0.16)]"
    >
      {/* Subtle Decorative Corner Index */}
      <div className="flex items-start justify-between gap-3">
        <span className="grid size-10 shrink-0 place-items-center rounded-xl border border-line bg-desk/50 text-ink shadow-xs transition-colors duration-150 group-hover:border-stamp/25 group-hover:bg-stamp/[0.08] group-hover:text-stamp">
          <Icon name={icon} className="size-5" />
        </span>
        <span className="font-mono text-[0.6875rem] font-semibold tracking-widest text-ink-faint group-hover:text-stamp/70">
          #{index}
        </span>
      </div>

      <div className="mt-4 flex items-baseline gap-2">
        <span className="display text-2xl leading-none text-ink tabular-nums group-hover:text-stamp">
          {metric}
        </span>
        <span className="text-xs font-medium text-ink-soft">{metricLabel}</span>
      </div>

      <div className="mt-3 border-t border-line/70 pt-3">
        <div className="flex items-center justify-between gap-2">
          <span className="text-[0.9375rem] font-semibold text-ink group-hover:text-stamp">{title}</span>
          <Icon
            name="chevron"
            className="size-4 shrink-0 text-ink-faint transition-all duration-150 group-hover:translate-x-0.5 group-hover:text-stamp"
          />
        </div>
        <p className="mt-0.5 truncate text-xs text-ink-soft">{description}</p>
      </div>
    </Link>
  );
}

/**
 * Live TOTP Vault Widget with countdown progress bar and one-click copy.
 */
function LiveTotpVault({ entries, isAdmin }: { entries: TotpEntry[] | null; isAdmin: boolean }) {
  const toast = useToast();
  const [copiedId, setCopiedId] = useState<string | null>(null);

  const copy = async (entry: TotpEntry) => {
    if (!entry.code) return;
    try {
      await navigator.clipboard.writeText(entry.code);
      setCopiedId(entry.id);
      toast(`已复制「${entry.name}」验证码：${entry.code}`);
      setTimeout(() => setCopiedId((prev) => (prev === entry.id ? null : prev)), 1500);
    } catch {
      // Ignore
    }
  };

  return (
    <section className="panel flex flex-col justify-between overflow-hidden">
      <header className="flex items-center justify-between gap-3 border-b border-line bg-desk/25 px-5 py-4 sm:px-6">
        <div className="flex items-center gap-3">
          <span className="grid size-9 shrink-0 place-items-center rounded-[10px] border border-stamp/20 bg-stamp/[0.06] text-stamp">
            <Icon name="timer" className="size-[1.0625rem]" />
          </span>
          <div>
            <h2 className="text-[0.9375rem] font-semibold">实时动态密码保险箱 (TOTP)</h2>
            <p className="text-xs text-ink-soft">点击卡片即可一键复制 6 位动态验证码</p>
          </div>
        </div>
        <Link
          href={isAdmin ? "/admin/totp" : "/totp"}
          className="inline-flex items-center gap-1 rounded-lg border border-line bg-white px-2.5 py-1.5 text-xs font-medium text-ink hover:border-stamp/40 hover:text-stamp"
        >
          {isAdmin ? "管理共享" : "全部验证器"}
          <Icon name="chevron" className="size-3.5" />
        </Link>
      </header>

      <div className="p-5 sm:p-6">
        {!entries || entries.length === 0 ? (
          <Empty icon="timer">
            尚未配置动态验证器。前往 <Link href="/totp" className="font-medium text-stamp underline">验证器页面</Link> 添加私人或团队共享密钥。
          </Empty>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2">
            {entries.slice(0, 4).map((e) => {
              const isCopied = copiedId === e.id;
              return (
                <button
                  key={e.id}
                  type="button"
                  onClick={() => copy(e)}
                  className={`group relative flex flex-col justify-between overflow-hidden rounded-xl border p-3.5 text-left transition-all ${
                    isCopied
                      ? "border-seal bg-seal/[0.04] shadow-[0_0_0_3px_rgb(35_112_77/0.12)]"
                      : "border-line bg-white shadow-xs hover:border-stamp/45 hover:shadow-[0_0_0_3px_rgb(90_63_192/0.08)]"
                  }`}
                >
                  <div className="flex w-full items-center justify-between gap-2">
                    <span className="truncate text-xs font-semibold text-ink">{e.name}</span>
                    <Badge tone={e.shared ? "stamp" : "plain"}>{e.shared ? "共享" : "私人"}</Badge>
                  </div>
                  <div className="mt-2.5 flex w-full items-baseline justify-between gap-2">
                    <span className="font-mono text-xl font-bold tracking-[0.14em] text-ink tabular-nums group-hover:text-stamp">
                      {e.code ? `${e.code.slice(0, 3)} ${e.code.slice(3)}` : "••• •••"}
                    </span>
                    <span className="font-mono text-[0.6875rem] text-ink-faint group-hover:text-stamp">
                      {isCopied ? "✓ 已复制" : `${e.remaining ?? 30}s`}
                    </span>
                  </div>
                </button>
              );
            })}
          </div>
        )}
      </div>
    </section>
  );
}

/**
 * Dedicated Mailbox & Verification Code Radar
 */
function MailboxRadar({
  configured,
  mailboxes,
  error,
}: {
  configured: boolean | null;
  mailboxes: MyMailbox[] | null;
  error: string;
}) {
  const [activeAddress, setActiveAddress] = useState<string | null>(null);

  return (
    <section className="panel overflow-hidden">
      <header className="flex items-center justify-between gap-3 border-b border-line bg-desk/25 px-5 py-4 sm:px-6">
        <div className="flex items-center gap-3">
          <span className="grid size-9 shrink-0 place-items-center rounded-[10px] border border-stamp/20 bg-stamp/[0.06] text-stamp">
            <Icon name="inbox" className="size-[1.0625rem]" />
          </span>
          <div>
            <h2 className="text-[0.9375rem] font-semibold">专用收信箱与验证码雷达</h2>
            <p className="text-xs text-ink-soft">点击邮箱即可在页内直接展开收件箱并提取最新验证码</p>
          </div>
        </div>
        <Link
          href="/mail"
          className="inline-flex items-center gap-1 rounded-lg border border-line bg-white px-2.5 py-1.5 text-xs font-medium text-ink hover:border-stamp/40 hover:text-stamp"
        >
          独立收件箱
          <Icon name="chevron" className="size-3.5" />
        </Link>
      </header>

      <div className="p-5 sm:p-6">
        {error && <p className="mb-3 text-sm text-void">{error}</p>}
        {configured === false ? (
          <Empty icon="inbox">系统尚未连接 xmail 邮件服务。</Empty>
        ) : activeAddress ? (
          <MailViewer address={activeAddress} onBack={() => setActiveAddress(null)} />
        ) : !mailboxes || mailboxes.length === 0 ? (
          <Empty icon="inbox">当前暂无分配给你的专用收信邮箱。</Empty>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2">
            {mailboxes.map((m) => (
              <button
                type="button"
                key={m.address}
                onClick={() => setActiveAddress(m.address)}
                className="group flex w-full items-center gap-3.5 rounded-xl border border-line bg-white p-3.5 text-left shadow-xs transition-all hover:border-stamp/45 hover:shadow-[0_0_0_3px_rgb(90_63_192/0.08)]"
              >
                <span className="grid size-10 shrink-0 place-items-center rounded-xl border border-stamp/20 bg-stamp/[0.06] text-stamp">
                  <Icon name="mail" className="size-5" />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-mono text-[0.8125rem] font-semibold text-ink group-hover:text-stamp">
                    {m.address}
                  </span>
                  <span className="mt-0.5 block text-xs text-ink-soft">分配于 {formatDate(m.assignedAt)}</span>
                </span>
                <Icon name="chevron" className="size-4 shrink-0 text-ink-faint transition-transform group-hover:translate-x-0.5 group-hover:text-stamp" />
              </button>
            ))}
          </div>
        )}
      </div>
    </section>
  );
}

/**
 * Interactive Active Terminals Manager
 */
function SecurityTerminals({
  sessions,
  onReload,
}: {
  sessions: DeviceSession[] | null;
  onReload: () => void;
}) {
  const toast = useToast();

  const revokeDevice = async (row: DeviceSession) => {
    if (!window.confirm(`退出设备「${deviceName(row.userAgent)}」？`)) return;
    try {
      await api.deleteSession(row.id);
      toast("已退出该设备");
      onReload();
    } catch (e) {
      toast(errorMessage(e));
    }
  };

  return (
    <section className="panel overflow-hidden">
      <header className="flex items-center justify-between gap-3 border-b border-line bg-desk/25 px-5 py-4 sm:px-6">
        <div className="flex items-center gap-3">
          <span className="grid size-9 shrink-0 place-items-center rounded-[10px] border border-line bg-white text-ink-soft shadow-xs">
            <Icon name="monitor" className="size-[1.0625rem]" />
          </span>
          <div>
            <h2 className="text-[0.9375rem] font-semibold">在线登录终端</h2>
            <p className="text-xs text-ink-soft">实时监控并管理你的活动会话</p>
          </div>
        </div>
        <Link href="/settings" className="text-xs font-medium text-stamp hover:underline">
          设置 →
        </Link>
      </header>

      <div className="divide-y divide-line/80 px-5 py-3 sm:px-6">
        {!sessions || sessions.length === 0 ? (
          <div className="py-3">
            <Empty icon="monitor">暂无在线会话记录。</Empty>
          </div>
        ) : (
          sessions.slice(0, 4).map((row) => (
            <div key={row.id} className="flex items-center justify-between gap-3 py-3">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="truncate text-sm font-medium text-ink">{deviceName(row.userAgent)}</span>
                  {row.current && <Badge tone="seal" dot>本机</Badge>}
                </div>
                <p className="mt-0.5 font-mono text-xs text-ink-soft tabular-nums">
                  {row.ip || "本地"} · {formatDate(row.lastSeen)}
                </p>
              </div>
              {!row.current && (
                <Button variant="ghost" size="sm" className="hover:text-void" onClick={() => revokeDevice(row)}>
                  下线
                </Button>
              )}
            </div>
          ))
        )}
      </div>
    </section>
  );
}

/**
 * Admin Telemetry & OIDC Registry Console
 */
function AdminCommandDeck({
  events,
  apps,
  issuer,
  totp,
  sessions,
  onReload,
}: {
  events: AuditEvent[] | null;
  apps: App[] | null;
  issuer: string;
  totp: TotpEntry[] | null;
  sessions: DeviceSession[] | null;
  onReload: () => void;
}) {
  return (
    <div className="grid gap-6 lg:grid-cols-12">
      {/* Left 7 Cols: Live Audit Telemetry + TOTP Vault */}
      <div className="space-y-6 lg:col-span-7">
        <LiveTotpVault entries={totp} isAdmin />

        <section className="panel overflow-hidden">
          <header className="flex items-center justify-between gap-3 border-b border-line bg-desk/25 px-5 py-4 sm:px-6">
            <div className="flex items-center gap-3">
              <span className="grid size-9 shrink-0 place-items-center rounded-[10px] border border-line bg-white text-ink-soft shadow-xs">
                <Icon name="history" className="size-[1.0625rem]" />
              </span>
              <div>
                <h2 className="text-[0.9375rem] font-semibold">实时安全审计遥测 (Audit Stream)</h2>
                <p className="text-xs text-ink-soft">全站最新登录、令牌签发与管理操作轨迹</p>
              </div>
            </div>
            <Link
              href="/admin/events"
              className="inline-flex items-center gap-1 rounded-lg border border-line bg-white px-2.5 py-1.5 text-xs font-medium text-ink hover:border-stamp/40 hover:text-stamp"
            >
              完整审计日志
              <Icon name="chevron" className="size-3.5" />
            </Link>
          </header>

          <div className="p-5 sm:p-6">
            {!events || events.length === 0 ? (
              <Empty icon="history">暂无审计事件记录。</Empty>
            ) : (
              <div className="relative space-y-3.5 before:absolute before:top-2 before:bottom-2 before:left-[7px] before:w-px before:bg-line">
                {events.slice(0, 6).map((e) => (
                  <div key={e.id} className="relative flex items-start justify-between gap-4 pl-6">
                    <span
                      aria-hidden="true"
                      className={`absolute top-1.5 left-1 size-2 rounded-full ${eventDotTone(e.type)}`}
                    />
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-sm font-medium text-ink">
                          {eventTypeLabels[e.type] ?? e.type}
                        </span>
                        {e.subjectName && (
                          <span className="truncate rounded border border-line bg-desk/50 px-1.5 py-0.5 font-mono text-[0.6875rem] text-ink-soft">
                            {e.subjectName}
                          </span>
                        )}
                      </div>
                      <p className="mt-0.5 truncate text-xs text-ink-soft">
                        操作人：{e.actorEmail || "系统"} {e.ip ? `· IP ${e.ip}` : ""}
                      </p>
                    </div>
                    <span className="shrink-0 font-mono text-xs text-ink-faint tabular-nums">
                      {shortTime.format(new Date(e.at))}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </div>
        </section>
      </div>

      {/* Right 5 Cols: OIDC Discovery Quick-Copy & Terminals */}
      <div className="space-y-6 lg:col-span-5">
        <section className="panel overflow-hidden">
          <header className="flex items-center justify-between gap-3 border-b border-line bg-desk/25 px-5 py-4 sm:px-6">
            <div className="flex items-center gap-3">
              <span className="grid size-9 shrink-0 place-items-center rounded-[10px] border border-stamp/20 bg-stamp/[0.06] text-stamp">
                <Icon name="globe" className="size-[1.0625rem]" />
              </span>
              <div>
                <h2 className="text-[0.9375rem] font-semibold">OIDC 认证中心端点</h2>
                <p className="text-xs text-ink-soft">快速复制 Issuer 与已注册客户端</p>
              </div>
            </div>
            <Link
              href="/admin/apps"
              className="inline-flex items-center gap-1 rounded-lg border border-line bg-white px-2.5 py-1.5 text-xs font-medium text-ink hover:border-stamp/40 hover:text-stamp"
            >
              管理应用
              <Icon name="chevron" className="size-3.5" />
            </Link>
          </header>

          <div className="space-y-4 p-5 sm:p-6">
            <div>
              <p className="eyebrow mb-1.5">OIDC ISSUER 地址</p>
              <Copyable value={issuer} label="Issuer" />
            </div>
            <div>
              <p className="eyebrow mb-1.5">OIDC DISCOVERY 配置发现</p>
              <Copyable value={`${issuer.replace(/\/$/, "")}/.well-known/openid-configuration`} label="Discovery" />
            </div>

            {apps && apps.length > 0 && (
              <div className="border-t border-line pt-3.5">
                <p className="eyebrow mb-2.5">已接入客户端 ({apps.length})</p>
                <div className="space-y-2">
                  {apps.slice(0, 3).map((app) => (
                    <div
                      key={app.id}
                      className="flex items-center justify-between gap-2 rounded-xl border border-line bg-desk/30 px-3 py-2"
                    >
                      <div className="min-w-0">
                        <p className="truncate text-xs font-semibold text-ink">{app.name}</p>
                        <p className="truncate font-mono text-[0.6875rem] text-ink-soft">{app.id}</p>
                      </div>
                      <Badge tone="plain">{app.type.toUpperCase()}</Badge>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </section>

        <SecurityTerminals sessions={sessions} onReload={onReload} />
      </div>
    </div>
  );
}

export function AccountPage() {
  const { session } = useSession();
  const user = session?.user;

  const [sessions, setSessions] = useState<DeviceSession[] | null>(null);
  const [passkeys, setPasskeys] = useState<Passkey[] | null>(null);
  const [grants, setGrants] = useState<Grant[] | null>(null);
  const [totp, setTotp] = useState<TotpEntry[] | null>(null);

  // User mail state
  const [mailConfigured, setMailConfigured] = useState<boolean | null>(null);
  const [mailboxes, setMailboxes] = useState<MyMailbox[] | null>(null);
  const [mailError, setMailError] = useState("");

  // Admin state
  const [adminUsers, setAdminUsers] = useState<User[] | null>(null);
  const [adminApps, setAdminApps] = useState<App[] | null>(null);
  const [adminEvents, setAdminEvents] = useState<AuditEvent[] | null>(null);
  const [adminTotp, setAdminTotp] = useState<TotpEntry[] | null>(null);
  const [adminMailCount, setAdminMailCount] = useState<number | null>(null);

  const loadPersonal = () => {
    api.sessions().then((r) => setSessions(r.sessions)).catch(() => {});
    api.passkeys().then((r) => setPasskeys(r.passkeys)).catch(() => {});
    api.grants().then((r) => setGrants(r.grants)).catch(() => {});
    api.myTotp().then((r) => setTotp(r.entries)).catch(() => {});
  };

  useEffect(() => {
    if (!user) return;
    loadPersonal();

    if (user.isAdmin) {
      api.users().then((r) => setAdminUsers(r.users)).catch(() => {});
      api.apps().then((r) => setAdminApps(r.apps)).catch(() => {});
      api.events("?limit=6").then((r) => setAdminEvents(r.events)).catch(() => {});
      api.adminTotp().then((r) => setAdminTotp(r.entries)).catch(() => {});
      api.mailGrants().then((r) => setAdminMailCount(r.grants.length)).catch(() => {});
    } else {
      api
        .mailStatus()
        .then((st) => {
          setMailConfigured(st.configured);
          if (st.configured) {
            return api.myMailboxes().then((r) => setMailboxes(r.mailboxes));
          }
        })
        .catch((e) => setMailError(errorMessage(e)));
    }
  }, [user?.id, user?.isAdmin]);

  // Keep TOTP codes ticking fresh while on the dashboard
  useEffect(() => {
    const list = user?.isAdmin ? (totp?.length ? totp : adminTotp) : totp;
    const soonest = list?.reduce((min, e) => Math.min(min, e.remaining || e.period || 30), 30) ?? 0;
    if (!soonest) return;
    const timer = window.setTimeout(() => {
      api.myTotp().then((r) => setTotp(r.entries)).catch(() => {});
      if (user?.isAdmin) {
        api.adminTotp().then((r) => setAdminTotp(r.entries)).catch(() => {});
      }
    }, soonest * 1000);
    return () => window.clearTimeout(timer);
  }, [totp, adminTotp, user?.isAdmin]);

  if (!user) return null;

  const displayedTotp = user.isAdmin ? (totp && totp.length > 0 ? totp : adminTotp) : totp;

  const tiles = user.isAdmin
    ? [
        {
          index: "01",
          href: "/admin/users",
          icon: "users" as IconName,
          title: "用户目录",
          metric: adminUsers ? adminUsers.length : "—",
          metricLabel: `位注册成员 (${adminUsers ? adminUsers.filter((u) => u.isAdmin).length : 0} 管理员)`,
          description: "管理账户状态、角色权限与安全重置",
        },
        {
          index: "02",
          href: "/admin/apps",
          icon: "apps" as IconName,
          title: "接入应用",
          metric: adminApps ? adminApps.length : "—",
          metricLabel: "个 OIDC 客户端",
          description: "配置应用回调地址、访问策略与密钥",
        },
        {
          index: "03",
          href: "/admin/mail",
          icon: "inbox" as IconName,
          title: "邮件中台",
          metric: adminMailCount !== null ? adminMailCount : "—",
          metricLabel: "个已分配邮箱",
          description: "统一分配收信邮箱与验证码路由规则",
        },
        {
          index: "04",
          href: "/admin/totp",
          icon: "timer" as IconName,
          title: "共享验证器",
          metric: adminTotp ? adminTotp.length : "—",
          metricLabel: "枚团队动态令牌",
          description: "集中托管并向团队成员授权双因素验证码",
        },
      ]
    : [
        {
          index: "01",
          href: "/settings",
          icon: "user" as IconName,
          title: "个人资料与授权",
          metric: grants ? grants.length : "—",
          metricLabel: "个已授权应用",
          description: "修改对外显示名称与管理 OIDC 应用授权",
        },
        {
          index: "02",
          href: "/settings",
          icon: "lock" as IconName,
          title: "登录与安全",
          metric: (passkeys?.length ?? 0) + 1,
          metricLabel: `种登录凭证 · ${sessions?.length ?? 1} 台设备在线`,
          description: "管理登录密码、生物识别通行密钥与会话",
        },
        {
          index: "03",
          href: "/mail",
          icon: "inbox" as IconName,
          title: "我的专用邮箱",
          metric: mailboxes ? mailboxes.length : "—",
          metricLabel: "个分配收件箱",
          description: "实时接收邮件并自动提取一次性登录验证码",
        },
        {
          index: "04",
          href: "/totp",
          icon: "timer" as IconName,
          title: "双因素验证器",
          metric: totp ? totp.length : "—",
          metricLabel: "枚动态验证码",
          description: "查看管理员分配的共享令牌或管理私人密钥",
        },
      ];

  return (
    <AppShell
      title={`你好，${displayName(user)}`}
      description="你的主权身份通行证、已授权应用与实时认证控制台。"
    >
      <div className="space-y-6">
        {/* Tier 1: Perforated Diplomatic Pass + Authorized External Apps */}
        <SovereignDossierHero
          user={user}
          issuer={session.issuer}
          sessions={sessions}
          passkeys={passkeys}
          grants={grants}
          onReload={loadPersonal}
        />

        {/* Tier 2: 4-Column Numbered Tactile Command Deck */}
        <div className="grid gap-3.5 sm:grid-cols-2 lg:grid-cols-4">
          {tiles.map((t) => (
            <CommandTile key={t.index} {...t} />
          ))}
        </div>

        {/* Tier 3: Live Bento Workspace */}
        {user.isAdmin ? (
          <AdminCommandDeck
            events={adminEvents}
            apps={adminApps}
            issuer={session.issuer}
            totp={displayedTotp}
            sessions={sessions}
            onReload={loadPersonal}
          />
        ) : (
          <div className="grid gap-6 lg:grid-cols-12">
            <div className="space-y-6 lg:col-span-7">
              <MailboxRadar configured={mailConfigured} mailboxes={mailboxes} error={mailError} />
              <LiveTotpVault entries={totp} isAdmin={false} />
            </div>
            <div className="lg:col-span-5">
              <SecurityTerminals sessions={sessions} onReload={loadPersonal} />
            </div>
          </div>
        )}
      </div>
    </AppShell>
  );
}
