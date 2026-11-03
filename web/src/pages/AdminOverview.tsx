import { useEffect, useState } from "react";
import {
  api,
  type App,
  type AuditEvent,
  type Invite,
  type SigningKey,
  type SystemSettings,
  type TotpEntry,
  type User,
} from "../api";
import { useToast } from "../components/Dialog";
import { Icon, type IconName } from "../components/Icon";
import { AppShell } from "../components/Shell";
import { Badge, Copyable, Empty } from "../components/ui";
import { Link } from "../router";
import { useSession } from "../session";
import { CommandTile } from "./Account";

function eventDotTone(type: string) {
  if (/failure|denied|reuse/.test(type)) return "bg-void ring-4 ring-void/15";
  if (/delete|retire|signout|revoke|unassign|remove/.test(type)) return "bg-[#c47a1a] ring-4 ring-[#c47a1a]/15";
  if (type.startsWith("admin.") || type.startsWith("totp.") || type.startsWith("mail.")) return "bg-stamp ring-4 ring-stamp/15";
  if (/success|signin|register|verify/.test(type)) return "bg-seal ring-4 ring-seal/15";
  return "bg-ink-faint ring-4 ring-ink/10";
}

export const eventTypeLabels: Record<string, string> = {
  "login.success": "登录成功",
  "login.failure": "登录失败",
  "user.register": "注册账户",
  logout: "退出登录",
  "password.change": "更改密码",
  "password.reset_request": "请求重置密码",
  "password.reset": "重置密码",
  "email.verify": "验证邮箱",
  "passkey.add": "添加通行密钥",
  "passkey.remove": "移除通行密钥",
  "session.revoke": "退出设备",
  "app.signin": "登录应用",
  "app.denied": "拒绝访问应用",
  "grant.revoke": "撤销应用授权",
  "token.reuse": "检测到令牌重放",
  "admin.user_create": "添加用户",
  "admin.user_update": "修改用户",
  "admin.user_signout": "强制用户退出",
  "admin.invite_create": "生成邀请码",
  "admin.invite_revoke": "撤销邀请码",
  "admin.settings_update": "修改系统设置",
  "admin.app_create": "添加应用",
  "admin.app_update": "修改应用",
  "admin.app_delete": "删除应用",
  "admin.app_secret": "重新生成应用密钥",
  "admin.key_rotate": "生成签名密钥",
  "admin.key_retire": "停用签名密钥",
  "mail.assign": "分配邮箱",
  "mail.unassign": "取消邮箱分配",
  "mail.admin_view": "管理员查看邮箱",
  "totp.create": "添加验证器",
  "totp.update": "修改验证器",
  "totp.delete": "删除验证器",
  "totp.assign": "分配验证器",
  "avatar.set": "更新头像",
  "avatar.remove": "移除头像",
};

const shortTime = new Intl.DateTimeFormat("zh-CN", {
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
});

function SharedTotpOverview({ entries }: { entries: TotpEntry[] | null }) {
  const toast = useToast();
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const [expanded, setExpanded] = useState(false);
  const visible = expanded ? entries : entries?.slice(0, 4);

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
            <h2 className="text-[0.9375rem] font-semibold">共享验证器速览 (Shared TOTP)</h2>
            <p className="text-xs text-ink-soft">全站共享双因素令牌实时动态码，点击卡片即可一键复制</p>
          </div>
        </div>
        <Link
          href="/admin/totp"
          className="inline-flex items-center gap-1 rounded-lg border border-line bg-white px-2.5 py-1.5 text-xs font-medium text-ink hover:border-stamp/40 hover:text-stamp"
        >
          管理共享验证器
          <Icon name="chevron" className="size-3.5" />
        </Link>
      </header>

      <div className="p-5 sm:p-6">
        {!entries || entries.length === 0 ? (
          <Empty icon="timer">
            尚未创建团队共享验证器。前往{" "}
            <Link href="/admin/totp" className="font-medium text-stamp underline">
              共享验证器
            </Link>{" "}
            添加并分配给成员。
          </Empty>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2">
            {visible?.map((e) => {
              const isCopied = copiedId === e.id;
              const userCount = e.userIds?.length ?? 0;
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
                    <Badge tone={userCount > 0 ? "stamp" : "plain"}>
                      {userCount > 0 ? `已授权 ${userCount} 人` : "未分配"}
                    </Badge>
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
      {entries && entries.length > 4 && (
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          aria-expanded={expanded}
          className="flex w-full items-center justify-center gap-1.5 border-t border-line bg-desk/20 px-5 py-3 text-xs font-medium text-ink-soft transition-colors hover:bg-desk/40 hover:text-stamp"
        >
          <Icon name="chevron" className={`size-3.5 transition-transform ${expanded ? "-rotate-90" : "rotate-90"}`} />
          {expanded ? "收起" : `展开全部 ${entries.length} 个`}
        </button>
      )}
    </section>
  );
}

const regModeLabel: Record<string, string> = {
  closed: "关闭注册",
  invite: "仅限邀请",
  open: "开放注册",
};

export function AdminOverviewPage() {
  const { session } = useSession();
  const user = session?.user;

  const [adminUsers, setAdminUsers] = useState<User[] | null>(null);
  const [adminApps, setAdminApps] = useState<App[] | null>(null);
  const [adminEvents, setAdminEvents] = useState<AuditEvent[] | null>(null);
  const [adminTotp, setAdminTotp] = useState<TotpEntry[] | null>(null);
  const [adminMailCount, setAdminMailCount] = useState<number | null>(null);
  const [invites, setInvites] = useState<Invite[] | null>(null);
  const [sysSettings, setSysSettings] = useState<SystemSettings | null>(null);
  const [keys, setKeys] = useState<SigningKey[] | null>(null);

  useEffect(() => {
    if (!user?.isAdmin) return;
    api.users().then((r) => setAdminUsers(r.users)).catch(() => {});
    api.apps().then((r) => setAdminApps(r.apps)).catch(() => {});
    api.events("?limit=6").then((r) => setAdminEvents(r.events)).catch(() => {});
    api.adminTotp().then((r) => setAdminTotp(r.entries)).catch(() => {});
    api.mailGrants().then((r) => setAdminMailCount(r.grants.length)).catch(() => {});
    api.invites().then((r) => setInvites(r.invites)).catch(() => {});
    api.registrationSettings().then((r) => setSysSettings(r)).catch(() => {});
    api.keys().then((r) => setKeys(r.keys)).catch(() => {});
  }, [user?.id, user?.isAdmin]);

  // Keep shared TOTP codes ticking fresh
  useEffect(() => {
    const soonest = adminTotp?.reduce((min, e) => Math.min(min, e.remaining || e.period || 30), 30) ?? 0;
    if (!soonest) return;
    const timer = window.setTimeout(() => {
      api.adminTotp().then((r) => setAdminTotp(r.entries)).catch(() => {});
    }, soonest * 1000);
    return () => window.clearTimeout(timer);
  }, [adminTotp]);

  if (!session || !user) return null;

  const issuer = session.issuer;
  const pendingInvites = invites?.filter((i) => i.status === "pending").length ?? 0;
  const activeKeys = keys?.filter((k) => !k.retiredAt).length ?? 0;
  const regMode = sysSettings?.registration.mode ?? session.registrationMode;

  const tiles = [
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
  ];

  return (
    <AppShell
      title="管理面板"
      description="全站用户目录、OIDC 接入应用、邮件中台、共享验证器与安全审计总览。"
    >
      <div className="space-y-6">
        {/* Tier 1: 4-Column Admin Command Deck */}
        <div className="grid gap-3.5 sm:grid-cols-2 lg:grid-cols-4">
          {tiles.map((t) => (
            <CommandTile key={t.index} {...t} />
          ))}
        </div>

        {/* Tier 2: Admin Bento Workspace */}
        <div className="grid gap-6 lg:grid-cols-12">
          {/* Left 7 Cols: Shared TOTP Vault + Live Audit Telemetry */}
          <div className="space-y-6 lg:col-span-7">
            <SharedTotpOverview entries={adminTotp} />

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
                {!adminEvents || adminEvents.length === 0 ? (
                  <Empty icon="history">暂无审计事件记录。</Empty>
                ) : (
                  <div className="relative space-y-3.5 before:absolute before:top-2 before:bottom-2 before:left-[7px] before:w-px before:bg-line">
                    {adminEvents.slice(0, 6).map((e) => (
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

          {/* Right 5 Cols: OIDC Discovery Quick-Copy & System Status */}
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

                {adminApps && adminApps.length > 0 && (
                  <div className="border-t border-line pt-3.5">
                    <p className="eyebrow mb-2.5">已接入客户端 ({adminApps.length})</p>
                    <div className="space-y-2">
                      {adminApps.slice(0, 3).map((app) => (
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

            <section className="panel overflow-hidden">
              <header className="flex items-center justify-between gap-3 border-b border-line bg-desk/25 px-5 py-4 sm:px-6">
                <div className="flex items-center gap-3">
                  <span className="grid size-9 shrink-0 place-items-center rounded-[10px] border border-line bg-white text-ink-soft shadow-xs">
                    <Icon name="settings" className="size-[1.0625rem]" />
                  </span>
                  <div>
                    <h2 className="text-[0.9375rem] font-semibold">系统与安全状态</h2>
                    <p className="text-xs text-ink-soft">注册策略、邮件服务与签名密钥速览</p>
                  </div>
                </div>
                <Link href="/admin/settings" className="text-xs font-medium text-stamp hover:underline">
                  系统设置 →
                </Link>
              </header>

              <div className="divide-y divide-line/80 px-5 py-2 sm:px-6">
                <Link
                  href="/admin/invites"
                  className="group flex items-center justify-between gap-3 py-3 transition-colors"
                >
                  <div className="min-w-0">
                    <p className="text-sm font-medium text-ink group-hover:text-stamp">注册与邀请码</p>
                    <p className="mt-0.5 text-xs text-ink-soft">
                      当前为「{regModeLabel[regMode] ?? regMode}」· {pendingInvites} 个待使用邀请码
                    </p>
                  </div>
                  <Badge tone={regMode === "open" ? "seal" : regMode === "invite" ? "stamp" : "plain"}>
                    {regModeLabel[regMode] ?? regMode}
                  </Badge>
                </Link>

                <Link
                  href="/admin/settings"
                  className="group flex items-center justify-between gap-3 py-3 transition-colors"
                >
                  <div className="min-w-0">
                    <p className="text-sm font-medium text-ink group-hover:text-stamp">SMTP 发信与 xmail 连接</p>
                    <p className="mt-0.5 text-xs text-ink-soft">
                      SMTP {sysSettings?.smtp.enabled ? "已启用" : "未配置"} · xmail {sysSettings?.xmail.enabled ? "已连接" : "未配置"}
                    </p>
                  </div>
                  <Badge tone={sysSettings?.smtp.enabled || sysSettings?.xmail.enabled ? "seal" : "plain"}>
                    {sysSettings?.smtp.enabled && sysSettings?.xmail.enabled
                      ? "全部就绪"
                      : sysSettings?.smtp.enabled || sysSettings?.xmail.enabled
                        ? "部分启用"
                        : "待配置"}
                  </Badge>
                </Link>

                <Link
                  href="/admin/keys"
                  className="group flex items-center justify-between gap-3 py-3 transition-colors"
                >
                  <div className="min-w-0">
                    <p className="text-sm font-medium text-ink group-hover:text-stamp">JWKS 签名密钥</p>
                    <p className="mt-0.5 text-xs text-ink-soft">
                      RS256 非对称签名 · {keys ? `${activeKeys} 把可用密钥` : "正在加载…"}
                    </p>
                  </div>
                  <Badge tone="seal" dot>
                    RS256
                  </Badge>
                </Link>
              </div>
            </section>
          </div>
        </div>
      </div>
    </AppShell>
  );
}
