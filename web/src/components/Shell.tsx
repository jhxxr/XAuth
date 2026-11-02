import { useEffect, useId, useState, type ReactNode } from "react";
import { api } from "../api";
import { Link, navigate, useLocation } from "../router";
import { displayName, useSession } from "../session";
import { Icon, type IconName } from "./Icon";
import { Avatar } from "./ui";

/** The seal from the favicon: a futuristic minimalist orbital prism & split-beam X. */
export function Seal({ className = "size-7" }: { className?: string }) {
  const id = useId();
  return (
    <svg viewBox="0 0 32 32" fill="none" aria-hidden="true" className={`shrink-0 ${className}`}>
      <defs>
        <linearGradient id={`${id}-bg`} x1="0" y1="0" x2="32" y2="32" gradientUnits="userSpaceOnUse">
          <stop offset="0%" stopColor="#212b4e" />
          <stop offset="100%" stopColor="#11172b" />
        </linearGradient>
        <linearGradient id={`${id}-accent`} x1="23" y1="9" x2="9" y2="23" gradientUnits="userSpaceOnUse">
          <stop offset="0%" stopColor="#d6dcfa" />
          <stop offset="100%" stopColor="#8b74f7" />
        </linearGradient>
      </defs>
      <rect width="32" height="32" rx="8" fill={`url(#${id}-bg)`} />
      <rect x="0.5" y="0.5" width="31" height="31" rx="7.5" stroke="#fff" strokeOpacity="0.08" />
      <g stroke="#c9d0ea" strokeWidth="1.15" strokeLinecap="round" opacity="0.55">
        <path d="M12.2 5.68A11 11 0 0 1 19.8 5.68" />
        <path d="M26.32 12.2A11 11 0 0 1 26.32 19.8" />
        <path d="M19.8 26.32A11 11 0 0 1 12.2 26.32" />
        <path d="M5.68 19.8A11 11 0 0 1 5.68 12.2" />
      </g>
      <path d="M16 8.6L23.4 16L16 23.4L8.6 16Z" stroke="#8b74f7" strokeWidth="1.05" strokeLinejoin="round" opacity="0.55" />
      <path d="M22.2 9.8L18.55 13.45M13.45 18.55L9.8 22.2" stroke={`url(#${id}-accent)`} strokeWidth="2.35" strokeLinecap="round" />
      <path d="M9.8 9.8L22.2 22.2" stroke="#fff" strokeWidth="2.35" strokeLinecap="round" />
    </svg>
  );
}

export function Wordmark({ className = "" }: { className?: string }) {
  return <span className={`display text-[0.9375rem] tracking-[0.32em] ${className}`}>XAUTH</span>;
}

/** Re-exported for pages that imported it from the shell. */
export { Avatar };

type NavItem = { href: string; label: string; icon: IconName };
type NavGroup = { label: string; items: NavItem[] };

function navFor(isAdmin: boolean): NavGroup[] {
  if (isAdmin) {
    return [
      {
        label: "个人",
        items: [
          { href: "/account", label: "我的账户", icon: "user" },
          { href: "/settings", label: "用户设置", icon: "settings" },
          { href: "/totp", label: "验证器", icon: "timer" },
        ],
      },
      {
        label: "管理",
        items: [
          { href: "/admin/users", label: "用户", icon: "users" },
          { href: "/admin/invites", label: "注册与邀请", icon: "mail" },
          { href: "/admin/apps", label: "应用", icon: "apps" },
          { href: "/admin/mail", label: "邮件中台", icon: "inbox" },
          { href: "/admin/totp", label: "共享验证器", icon: "timer" },
          { href: "/admin/settings", label: "系统设置", icon: "settings" },
        ],
      },
      {
        label: "安全",
        items: [
          { href: "/admin/events", label: "操作记录", icon: "history" },
          { href: "/admin/keys", label: "签名密钥", icon: "key" },
        ],
      },
    ];
  }
  return [
    {
      label: "个人",
      items: [
        { href: "/account", label: "我的账户", icon: "user" },
        { href: "/settings", label: "用户设置", icon: "settings" },
        { href: "/mail", label: "我的邮箱", icon: "inbox" },
        { href: "/totp", label: "验证器", icon: "timer" },
      ],
    },
  ];
}

function Sidebar({
  groups,
  path,
  onNavigate,
  collapsed = false,
  onToggle,
}: {
  groups: NavGroup[];
  path: string;
  onNavigate?: () => void;
  collapsed?: boolean;
  onToggle?: () => void;
}) {
  const { session, setUser } = useSession();
  const [busy, setBusy] = useState(false);
  const user = session?.user;
  const signOut = async () => {
    setBusy(true);
    try {
      await api.logout();
    } finally {
      setUser(null);
      navigate("/login", { replace: true });
    }
  };

  return (
    <div className="flex h-full flex-col">
      <div className={`flex h-16 shrink-0 items-center gap-2 ${collapsed ? "justify-center px-2" : "px-5"}`}>
        {!collapsed && (
          <Link href="/account" className="flex items-center rounded-md" onClick={onNavigate} aria-label="XAuth 账户">
            <Wordmark className="text-[0.8125rem]" />
          </Link>
        )}
        {onToggle && (
          <button
            type="button"
            onClick={onToggle}
            aria-label={collapsed ? "展开侧边栏" : "收起侧边栏"}
            title={collapsed ? "展开侧边栏" : "收起侧边栏"}
            className={`${collapsed ? "" : "ml-auto"} grid size-8 shrink-0 place-items-center rounded-lg text-ink-faint transition-colors hover:bg-ink/[0.05] hover:text-ink`}
          >
            <Icon name="chevron" className={`size-4 ${collapsed ? "" : "rotate-180"}`} />
          </button>
        )}
      </div>
      <nav aria-label="主导航" className="flex-1 overflow-y-auto px-3 pb-4">
        {groups.map((g) => (
          <div key={g.label} className="mt-4 first:mt-1">
            {!collapsed ? (
              <p className="eyebrow px-3 pb-1.5 !text-[0.625rem] !text-ink-faint">{g.label}</p>
            ) : (
              <p className="sr-only">{g.label}</p>
            )}
            <ul className="space-y-0.5">
              {g.items.map((n) => (
                <li key={n.href}>
                  <Link
                    href={n.href}
                    onClick={onNavigate}
                    aria-current={path === n.href ? "page" : undefined}
                    title={collapsed ? n.label : undefined}
                    className={`group flex h-9 items-center gap-2.5 rounded-lg text-[0.8125rem] font-medium text-ink-soft transition-colors hover:bg-ink/[0.04] hover:text-ink aria-[current=page]:bg-white aria-[current=page]:text-ink aria-[current=page]:shadow-[0_0_0_1px_var(--color-line),0_1px_2px_rgb(27_35_64/0.06)] ${collapsed ? "justify-center px-0" : "px-3"}`}
                  >
                    <Icon name={n.icon} className="size-4 shrink-0 text-ink-faint transition-colors group-hover:text-ink-soft group-aria-[current=page]:text-stamp" />
                    {!collapsed && n.label}
                  </Link>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </nav>
      {user && (
        <div className={`shrink-0 border-t border-line p-3 ${collapsed ? "flex flex-col items-center gap-1" : ""}`}>
          <div className={`flex items-center gap-2.5 rounded-xl p-2 ${collapsed ? "flex-col" : ""}`}>
            <Avatar text={displayName(user)} src={user.avatarUrl} />
            {!collapsed && (
              <div className="min-w-0 flex-1 leading-tight">
                <p className="truncate text-[0.8125rem] font-medium">{displayName(user)}</p>
                <p className="truncate text-[0.71875rem] text-ink-soft">{user.email}</p>
              </div>
            )}
            <button
              type="button"
              onClick={signOut}
              disabled={busy}
              title="退出登录"
              aria-label="退出登录"
              className="grid size-8 shrink-0 place-items-center rounded-lg text-ink-soft transition-colors hover:bg-ink/[0.06] hover:text-void disabled:opacity-60"
            >
              <Icon name="logout" className="size-4" />
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

type ShellProps = {
  title: string;
  description?: ReactNode;
  actions?: ReactNode;
  /** Narrow pages (forms, lists) read better at a shorter measure. */
  width?: "default" | "narrow";
  children: ReactNode;
};

/** Layout for the signed-in pages: a sidebar console with a page header. */
const SIDEBAR_KEY = "xauth.sidebar-collapsed";

export function AppShell({ title, description, actions, width = "default", children }: ShellProps) {
  const { session } = useSession();
  const { path } = useLocation();
  const [open, setOpen] = useState(false);
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem(SIDEBAR_KEY) === "1";
    } catch {
      return false;
    }
  });
  const groups = navFor(!!session?.user?.isAdmin);
  const toggleCollapsed = () => {
    setCollapsed((v) => {
      try {
        localStorage.setItem(SIDEBAR_KEY, v ? "0" : "1");
      } catch {
        // Private browsing may refuse storage; the layout still toggles.
      }
      return !v;
    });
  };

  useEffect(() => {
    document.title = `${title} · XAuth`;
  }, [title]);
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);

  return (
    <div className={`min-h-dvh transition-[padding] duration-200 ${collapsed ? "lg:pl-[4.5rem]" : "lg:pl-60"}`}>
      <aside className={`fixed inset-y-0 left-0 z-30 hidden border-r border-line bg-paper/70 backdrop-blur-md transition-[width] duration-200 lg:block ${collapsed ? "w-[4.5rem]" : "w-60"}`}>
        <Sidebar groups={groups} path={path} collapsed={collapsed} onToggle={toggleCollapsed} />
      </aside>

      {/* Mobile: top bar and drawer. */}
      <header className="sticky top-0 z-30 flex h-14 items-center gap-3 border-b border-line bg-paper/85 px-4 backdrop-blur-md lg:hidden">
        <button type="button" onClick={() => setOpen(true)} aria-label="打开导航" aria-expanded={open} className="-ml-1.5 grid size-9 place-items-center rounded-lg text-ink-soft hover:bg-ink/[0.05] hover:text-ink">
          <Icon name="menu" className="size-5" />
        </button>
        <Link href="/account" className="flex items-center rounded-md">
          <Wordmark className="text-[0.75rem]" />
        </Link>
      </header>
      {open && (
        <div className="fixed inset-0 z-50 lg:hidden" role="dialog" aria-modal="true" aria-label="导航">
          <button type="button" aria-label="关闭导航" className="absolute inset-0 bg-ink/30 backdrop-blur-[2px]" onClick={() => setOpen(false)} />
          <aside className="toast-in absolute inset-y-0 left-0 w-[17rem] max-w-[85vw] border-r border-line bg-paper shadow-2xl">
            <Sidebar groups={groups} path={path} onNavigate={() => setOpen(false)} />
          </aside>
        </div>
      )}

      <main className={`mx-auto px-4 pt-7 pb-16 sm:px-8 sm:pt-10 lg:pt-12 ${width === "narrow" ? "max-w-3xl" : "max-w-[1120px]"}`}>
        <div className="mb-7 flex flex-wrap items-end justify-between gap-x-6 gap-y-4 sm:mb-8">
          <div className="min-w-0 max-w-2xl">
            <h1 className="text-[1.5rem] leading-tight font-semibold tracking-tight sm:text-[1.75rem]">{title}</h1>
            {description && <p className="mt-2 text-sm leading-6 text-ink-soft">{description}</p>}
          </div>
          {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
        </div>
        {children}
      </main>
    </div>
  );
}

/** Layout for the sign-in pages: the pass on the desk. */
export function Centered({ children }: { children: ReactNode }) {
  const { session } = useSession();
  const host = session ? new URL(session.issuer).host : "";
  return (
    <div className="relative flex min-h-dvh flex-col items-center overflow-hidden px-4 py-10 sm:py-16">
      {/* Engraved concentric rings behind the pass, like the field of a banknote. */}
      <svg aria-hidden="true" viewBox="0 0 800 800" className="pointer-events-none absolute top-1/2 left-1/2 -z-0 w-[1100px] max-w-none -translate-x-1/2 -translate-y-1/2 opacity-[0.5]">
        <defs>
          <radialGradient id="rings-fade" cx="50%" cy="50%" r="50%">
            <stop offset="35%" stopColor="#fff" stopOpacity="1" />
            <stop offset="100%" stopColor="#fff" stopOpacity="0" />
          </radialGradient>
          <mask id="rings-mask">
            <rect width="800" height="800" fill="url(#rings-fade)" />
          </mask>
        </defs>
        <g mask="url(#rings-mask)" fill="none" stroke="#1b2340" strokeOpacity="0.07">
          {Array.from({ length: 22 }, (_, i) => (
            <circle key={i} cx="400" cy="400" r={120 + i * 13} />
          ))}
        </g>
      </svg>
      <div className="relative mb-8 flex items-center gap-2.5 text-ink">
        <Seal className="size-8" />
        <Wordmark className="text-[0.875rem]" />
      </div>
      <main className="relative w-full max-w-[440px]">{children}</main>
      {host && (
        <p className="relative mt-8 inline-flex items-center gap-1.5 rounded-full border border-line bg-paper/70 px-3 py-1.5 text-center text-xs text-ink-soft backdrop-blur">
          <Icon name="lock" className="size-3.5 text-seal" />
          你正在 <span className="font-mono text-[0.6875rem] text-ink">{host}</span> 上登录
        </p>
      )}
    </div>
  );
}
