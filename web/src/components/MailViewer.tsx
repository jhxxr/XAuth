import { useEffect, useRef, useState, useId } from "react";
import { api, errorMessage, type EmailDetail, type EmailItem, type VerificationCodeResult } from "../api";
import { Icon } from "./Icon";
import { Avatar, Badge, Button, IconTile, Notice, Segmented } from "./ui";

const timeFmt = new Intl.DateTimeFormat("zh-CN", {
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
});

function formatDate(sec: number) {
  if (!sec) return "";
  return timeFmt.format(new Date(sec * 1000));
}

/** The star is not part of the shared icon set; drawn here in the same Lucide style. */
function StarIcon({ filled, className = "size-4" }: { filled?: boolean; className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill={filled ? "currentColor" : "none"} stroke="currentColor" strokeWidth={1.75} strokeLinejoin="round" aria-hidden="true" className={`shrink-0 ${className}`}>
      <path d="M11.525 2.295a.53.53 0 0 1 .95 0l2.31 4.679a2.123 2.123 0 0 0 1.595 1.16l5.166.756a.53.53 0 0 1 .294.904l-3.736 3.638a2.123 2.123 0 0 0-.611 1.878l.882 5.14a.53.53 0 0 1-.771.56l-4.618-2.428a2.122 2.122 0 0 0-1.973 0L6.396 21.01a.53.53 0 0 1-.77-.56l.881-5.139a2.122 2.122 0 0 0-.611-1.879L2.16 9.795a.53.53 0 0 1 .294-.906l5.165-.755a2.122 2.122 0 0 0 1.597-1.16z" />
    </svg>
  );
}

const starOn = "text-[#c98a12]";

/** A copy button for a verification code, with a visible "copied" state. */
function CopyCode({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    } catch {
      setCopied(false);
    }
  };
  return (
    <Button variant="secondary" size="sm" onClick={copy} aria-label="复制验证码" className={copied ? "!border-seal/30 !text-seal" : ""}>
      <Icon name={copied ? "check" : "copy"} className="size-3.5" />
      {copied ? "已复制" : "复制"}
      <span aria-live="polite" className="sr-only">
        {copied ? "已复制" : ""}
      </span>
    </Button>
  );
}

type MailViewerProps = {
  address: string;
  adminMode?: boolean;
  onBack?: () => void;
};

type MailboxSummary = {
  stats?: { total: number; unread: number };
  services?: { name: string; loginUrl: string; note: string | null }[];
};

export function MailViewer({ address, adminMode = false, onBack }: MailViewerProps) {
  const [emails, setEmails] = useState<EmailItem[] | null>(null);
  const [summary, setSummary] = useState<MailboxSummary | null>(null);
  const [code, setCode] = useState<VerificationCodeResult | null>(null);
  const [codeLoading, setCodeLoading] = useState(false);
  const [selectedEmail, setSelectedEmail] = useState<EmailDetail | null>(null);
  const [search, setSearch] = useState("");
  const [filterRead, setFilterRead] = useState<string>("");
  const [filterStarred, setFilterStarred] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [cleaningUp, setCleaningUp] = useState(false);
  const searchId = useId();
  const readerRef = useRef<HTMLDivElement>(null);

  const loadSummary = async () => {
    try {
      if (adminMode) {
        const res = await api.adminMail<{ stats: { total: number; unread: number }; services: { name: string; loginUrl: string; note: string | null }[] }>(
          "GET",
          `/mailboxes/${encodeURIComponent(address)}`,
        );
        setSummary({ stats: res.stats, services: res.services });
      } else {
        const res = await api.myMailbox(address);
        setSummary({ stats: res.stats, services: res.services });
      }
    } catch {
      // Non-critical
    }
  };

  const loadEmails = async () => {
    setLoading(true);
    setError("");
    try {
      if (adminMode) {
        const queryParams = new URLSearchParams();
        if (search) queryParams.set("q", search);
        if (filterRead) queryParams.set("is_read", filterRead);
        if (filterStarred) queryParams.set("is_starred", "true");
        queryParams.set("limit", "50");
        const res = await api.adminMail<{ items: EmailItem[] }>(
          "GET",
          `/mailboxes/${encodeURIComponent(address)}/emails?${queryParams.toString()}`,
        );
        setEmails(res.items || []);
      } else {
        let q = "?limit=50";
        if (search) q += `&q=${encodeURIComponent(search)}`;
        if (filterRead) q += `&is_read=${filterRead}`;
        if (filterStarred) q += `&is_starred=true`;
        const res = await api.myEmails(address, q);
        setEmails(res.items || []);
      }
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  const loadLatestCode = async () => {
    setCodeLoading(true);
    try {
      if (adminMode) {
        const res = await api.adminMail<VerificationCodeResult>(
          "GET",
          `/mailboxes/${encodeURIComponent(address)}/verification-code?seconds=600`,
        );
        setCode(res && res.code ? res : null);
      } else {
        const res = await api.myCode(address, 600);
        setCode(res && res.code ? res : null);
      }
    } catch {
      // Non-critical
    } finally {
      setCodeLoading(false);
    }
  };

  useEffect(() => {
    void loadSummary();
    void loadEmails();
    void loadLatestCode();
  }, [address, filterRead, filterStarred]);

  // On narrow layouts the reader replaces the list; bring its top into view.
  useEffect(() => {
    const el = readerRef.current;
    if (selectedEmail && el && el.getBoundingClientRect().top < 0) el.scrollIntoView({ block: "start" });
  }, [selectedEmail?.id]);

  const deleteEmail = async (e: React.MouseEvent, id: string) => {
    e.stopPropagation();
    if (!window.confirm("确定删除这封邮件？")) return;
    try {
      await api.adminMail("DELETE", `/emails/${encodeURIComponent(id)}`);
      setEmails((prev) => prev?.filter((x) => x.id !== id) ?? null);
      if (selectedEmail?.id === id) setSelectedEmail(null);
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const cleanupEmails = async () => {
    const daysStr = window.prompt("清理多少天前的旧邮件（星标邮件不会被删除）：", "30");
    if (!daysStr) return;
    const days = parseInt(daysStr, 10);
    if (isNaN(days) || days < 0) return;
    setCleaningUp(true);
    try {
      const res = await api.adminMail<{ deleted: number; days: number }>("POST", "/emails/cleanup", { days });
      alert(`已成功清理 ${res.deleted} 封旧邮件（${res.days} 天前）。`);
      void loadEmails();
      void loadSummary();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setCleaningUp(false);
    }
  };

  const viewEmail = async (item: EmailItem) => {
    try {
      if (adminMode) {
        const res = await api.adminMail<{ email: EmailDetail }>("GET", `/emails/${item.id}`);
        setSelectedEmail(res.email);
        if (!item.isRead) {
          await api.adminMail("POST", `/emails/${item.id}/read`);
          setEmails((prev) => prev?.map((e) => (e.id === item.id ? { ...e, isRead: true } : e)) ?? null);
        }
      } else {
        const res = await api.myEmail(item.id);
        setSelectedEmail(res.email);
        if (!item.isRead) {
          await api.markEmailRead(item.id);
          setEmails((prev) => prev?.map((e) => (e.id === item.id ? { ...e, isRead: true } : e)) ?? null);
        }
      }
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const toggleStar = async (e: React.MouseEvent, item: EmailItem) => {
    e.stopPropagation();
    try {
      const nextStar = !item.isStarred;
      if (adminMode) {
        await api.adminMail("POST", `/emails/${item.id}/star`, { isStarred: nextStar });
      } else {
        await api.toggleEmailStar(item.id, nextStar);
      }
      setEmails((prev) => prev?.map((x) => (x.id === item.id ? { ...x, isStarred: nextStar } : x)) ?? null);
      if (selectedEmail?.id === item.id) {
        setSelectedEmail({ ...selectedEmail, isStarred: nextStar });
      }
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  return (
    <div className="@container space-y-5">
      {/* Top Bar */}
      <div>
        {onBack && (
          <Button variant="ghost" size="sm" onClick={onBack} className="mb-3 -ml-2.5">
            <Icon name="back" className="size-4" />
            返回列表
          </Button>
        )}
        <div className="flex flex-wrap items-center gap-x-4 gap-y-3">
          <div className="flex min-w-0 flex-1 basis-64 items-center gap-3.5">
            <IconTile name="inbox" tone="stamp" />
            <div className="min-w-0">
              <p className="truncate font-mono text-[0.9375rem] font-semibold text-ink" title={address}>
                {address}
              </p>
              <div className="mt-1 flex flex-wrap items-center gap-1.5">
                {summary?.stats ? (
                  <>
                    <Badge>共 {summary.stats.total} 封</Badge>
                    <Badge tone={summary.stats.unread > 0 ? "stamp" : "plain"} dot={summary.stats.unread > 0}>
                      {summary.stats.unread} 封未读
                    </Badge>
                  </>
                ) : (
                  <span aria-hidden="true" className="h-[1.1875rem] w-28 animate-pulse rounded-full bg-ink/[0.06] motion-reduce:animate-none" />
                )}
              </div>
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            {adminMode && (
              <Button variant="ghost" size="sm" className="hover:text-void" onClick={() => void cleanupEmails()} busy={cleaningUp}>
                {!cleaningUp && <Icon name="trash" className="size-3.5" />}
                清理旧邮件
              </Button>
            )}
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                void loadEmails();
                void loadLatestCode();
                void loadSummary();
              }}
              busy={loading}
            >
              {!loading && <Icon name="refresh" className="size-3.5" />}
              刷新收件箱
            </Button>
          </div>
        </div>

        {/* Attached Services Quick Links */}
        {summary?.services && summary.services.length > 0 && (
          <div className="mt-4 flex flex-wrap items-center gap-2">
            <span className="eyebrow mr-1">快捷服务入口</span>
            {summary.services.map((sv, idx) => (
              <a
                key={idx}
                href={sv.loginUrl}
                target="_blank"
                rel="noreferrer"
                title={sv.note || sv.loginUrl}
                className="group inline-flex h-7 items-center gap-1.5 rounded-lg border border-line bg-white px-2.5 text-[0.8125rem] font-medium text-ink shadow-xs transition-colors hover:border-stamp/35 hover:text-stamp"
              >
                <Icon name="link" className="size-3.5 text-ink-faint transition-colors group-hover:text-stamp" />
                <span>{sv.name}</span>
                <span aria-hidden="true" className="text-[0.6875rem] text-ink-faint">
                  ↗
                </span>
              </a>
            ))}
          </div>
        )}
      </div>

      {error && <Notice>{error}</Notice>}

      {/* Latest verification code banner */}
      {code?.code ? (
        <div className="rounded-[14px] border border-stamp/25 bg-gradient-to-br from-stamp/[0.08] via-stamp/[0.035] to-transparent p-4 shadow-xs sm:px-5">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-3">
            <span aria-hidden="true" className="hidden size-11 shrink-0 place-items-center rounded-xl border border-stamp/20 bg-white text-stamp shadow-xs @md:grid">
              <Icon name="shield" className="size-5" />
            </span>
            <div className="min-w-0 flex-1">
              <p className="eyebrow !text-stamp">最新收到的验证码</p>
              <p className="mt-1.5 font-mono text-[1.625rem] leading-none font-semibold tracking-[0.18em] text-stamp tabular-nums sm:text-[1.875rem]">{code.code}</p>
              <p className="mt-2 truncate text-xs text-ink-soft">
                {[code.from ? `来自 ${code.fromName || code.from}` : "", formatDate(code.receivedAt || 0)].filter(Boolean).join(" · ")}
              </p>
            </div>
            <CopyCode value={code.code} />
          </div>
        </div>
      ) : codeLoading ? (
        <div className="flex items-center gap-2.5 rounded-[14px] border border-dashed border-line-strong bg-desk/35 px-4 py-3 text-[0.8125rem] text-ink-soft">
          <span className="size-3.5 animate-spin rounded-full border-2 border-current border-r-transparent motion-reduce:animate-none" aria-hidden="true" />
          正在检查最新验证码...
        </div>
      ) : null}

      <div className="panel overflow-hidden">
        {/* Filters */}
        <div className="flex flex-wrap items-center gap-2 border-b border-line bg-desk/30 px-3 py-2.5 sm:px-4">
          <div className="relative min-w-0 flex-1 basis-56">
            <Icon name="search" className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-ink-faint" />
            <input
              id={searchId}
              type="search"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") void loadEmails();
              }}
              placeholder="搜索主题、发件人..."
              aria-label="搜索邮件"
              className="field !h-9 !pr-16 !pl-9 text-sm"
            />
            <button
              type="button"
              onClick={() => void loadEmails()}
              className="absolute top-1/2 right-1 h-7 -translate-y-1/2 rounded-md px-2.5 text-[0.8125rem] font-medium text-ink-soft transition-colors hover:bg-ink/[0.06] hover:text-ink"
            >
              搜索
            </button>
          </div>
          <div className="flex items-center gap-2">
            <Segmented
              label="读取状态"
              options={[
                ["", "全部"],
                ["false", "未读"],
                ["true", "已读"],
              ]}
              value={filterRead}
              onChange={setFilterRead}
            />
            <button
              type="button"
              aria-pressed={filterStarred}
              onClick={() => setFilterStarred((cur) => !cur)}
              className={`inline-flex h-9 shrink-0 items-center gap-1.5 rounded-[11px] border px-3 text-[0.8125rem] font-medium transition-colors ${
                filterStarred ? "border-[#c98a12]/35 bg-[#c98a12]/[0.08] text-[#8a5d06]" : "border-line bg-white text-ink-soft hover:text-ink"
              }`}
            >
              <StarIcon filled={filterStarred} className={`size-3.5 ${filterStarred ? starOn : ""}`} />
              仅星标
            </button>
          </div>
        </div>

        <div className="@3xl:grid @3xl:h-[640px] @3xl:grid-cols-[minmax(0,22rem)_minmax(0,1fr)]">
          {/* Email List */}
          <div className={`min-h-0 @3xl:overflow-y-auto @3xl:border-r @3xl:border-line ${selectedEmail ? "hidden @3xl:block" : ""}`}>
            {emails === null && !error && (
              <ul aria-hidden="true" className="divide-y divide-line/70">
                {[0, 1, 2, 3].map((i) => (
                  <li key={i} className="flex gap-3 px-5 py-4">
                    <span className="size-7 shrink-0 animate-pulse rounded-full bg-ink/[0.06] motion-reduce:animate-none" />
                    <span className="flex-1 space-y-2">
                      <span className="block h-3 w-1/3 animate-pulse rounded bg-ink/[0.06] motion-reduce:animate-none" />
                      <span className="block h-3 w-3/4 animate-pulse rounded bg-ink/[0.06] motion-reduce:animate-none" />
                    </span>
                  </li>
                ))}
              </ul>
            )}
            {emails?.length === 0 && (
              <div className="flex flex-col items-center px-6 py-14 text-center">
                <span aria-hidden="true" className="grid size-11 place-items-center rounded-xl border border-dashed border-line-strong text-ink-faint">
                  <Icon name="inbox" className="size-5" />
                </span>
                <p className="mt-3 text-[0.8125rem] text-ink-soft">收件箱里暂时没有符合条件的邮件。</p>
              </div>
            )}
            <ul className="divide-y divide-line/70">
              {emails?.map((item) => {
                const active = selectedEmail?.id === item.id;
                const sender = item.fromName || item.from;
                return (
                  <li
                    key={item.id}
                    data-mail-item
                    className={`group relative transition-colors ${
                      active
                        ? "bg-stamp/[0.055] before:absolute before:inset-y-0 before:left-0 before:w-[3px] before:bg-stamp"
                        : !item.isRead
                          ? "bg-white hover:bg-desk/40"
                          : "hover:bg-desk/40"
                    }`}
                  >
                    <button
                      type="button"
                      onClick={() => void viewEmail(item)}
                      aria-current={active ? "true" : undefined}
                      className="flex w-full items-start gap-3 py-3.5 pr-4 pl-5 text-left focus-visible:outline-offset-[-2px]"
                    >
                      {/* Unread indicator */}
                      <span
                        className={`absolute top-[1.375rem] left-2 size-1.5 rounded-full ${!item.isRead ? "bg-stamp" : "bg-transparent"}`}
                        title={!item.isRead ? "未读" : "已读"}
                      />
                      {!item.isRead && <span className="sr-only">未读</span>}
                      <Avatar text={sender} size="sm" tone={!item.isRead ? "ink" : "plain"} />

                      {/* Content */}
                      <span className="min-w-0 flex-1">
                        <span className="flex items-baseline gap-2">
                          <span
                            className={`min-w-0 flex-1 truncate text-[0.8125rem] ${!item.isRead ? "font-semibold text-ink" : "font-medium text-ink-soft"}`}
                            title={item.fromName ? `${item.fromName} <${item.from}>` : item.from}
                          >
                            {sender}
                          </span>
                          <span className="shrink-0 text-[0.71875rem] text-ink-faint tabular-nums">{formatDate(item.createdAt)}</span>
                        </span>
                        <span className={`mt-0.5 block truncate text-sm ${!item.isRead ? "font-semibold text-ink" : "text-ink"}`}>{item.subject || "（无主题）"}</span>
                        <span className={`mt-1 flex min-w-0 items-center gap-2 ${adminMode ? "pr-14" : "pr-7"}`}>
                          {item.verificationCode && (
                            <span className="shrink-0 rounded-md border border-stamp/20 bg-stamp/[0.07] px-1.5 py-px font-mono text-[0.6875rem] font-semibold text-stamp">
                              <span className="sr-only">验证码: </span>
                              {item.verificationCode}
                            </span>
                          )}
                          <span className="truncate text-xs text-ink-faint">{item.preview}</span>
                        </span>
                      </span>
                    </button>

                    <div className="absolute right-2.5 bottom-2 flex items-center">
                      {/* Admin Delete button */}
                      {adminMode && (
                        <button
                          type="button"
                          onClick={(e) => void deleteEmail(e, item.id)}
                          className="grid size-7 place-items-center rounded-md text-ink-faint opacity-0 transition-[opacity,color,background-color] group-hover:opacity-100 hover:bg-void/[0.06] hover:text-void focus-visible:opacity-100"
                          title="删除邮件"
                          aria-label="删除邮件"
                        >
                          <Icon name="trash" className="size-3.5" />
                        </button>
                      )}
                      {/* Star toggle */}
                      <button
                        type="button"
                        onClick={(e) => void toggleStar(e, item)}
                        className={`grid size-7 place-items-center rounded-md transition-colors hover:bg-ink/[0.05] ${item.isStarred ? starOn : "text-ink-faint hover:text-[#c98a12]"}`}
                        aria-label={item.isStarred ? "取消星标" : "标记星标"}
                        aria-pressed={item.isStarred}
                      >
                        <StarIcon filled={item.isStarred} className="size-3.5" />
                      </button>
                    </div>
                  </li>
                );
              })}
            </ul>
          </div>

          {/* Email Reader (an inline pane; replaces the list on narrow layouts) */}
          <div ref={readerRef} className={`min-h-0 min-w-0 scroll-mt-20 @3xl:overflow-y-auto ${selectedEmail ? "" : "hidden @3xl:flex"}`}>
            {selectedEmail ? (
              <EmailReader
                key={selectedEmail.id}
                email={selectedEmail}
                adminMode={adminMode}
                onClose={() => setSelectedEmail(null)}
                onDelete={() => {
                  if (!window.confirm("确定删除这封邮件？")) return;
                  void api.adminMail("DELETE", `/emails/${encodeURIComponent(selectedEmail.id)}`).then(() => {
                    setEmails((prev) => prev?.filter((x) => x.id !== selectedEmail.id) ?? null);
                    setSelectedEmail(null);
                  });
                }}
                onToggleStar={() => {
                  const next = !selectedEmail.isStarred;
                  setSelectedEmail({ ...selectedEmail, isStarred: next });
                  setEmails((prev) => prev?.map((x) => (x.id === selectedEmail.id ? { ...x, isStarred: next } : x)) ?? null);
                  if (adminMode) {
                    void api.adminMail("POST", `/emails/${selectedEmail.id}/star`, { isStarred: next });
                  } else {
                    void api.toggleEmailStar(selectedEmail.id, next);
                  }
                }}
              />
            ) : (
              <div className="m-auto flex flex-col items-center px-6 py-16 text-center">
                <span aria-hidden="true" className="relative grid size-14 place-items-center">
                  <span className="absolute inset-0 rounded-2xl border border-line bg-white shadow-xs" />
                  <span className="absolute -inset-2 rounded-[1.25rem] border border-dashed border-line" />
                  <Icon name="mail" className="relative size-6 text-ink-faint" />
                </span>
                <p className="mt-6 text-sm font-medium text-ink">选择一封邮件开始阅读</p>
                <p className="mt-1 max-w-xs text-[0.8125rem] leading-5 text-ink-soft">邮件中识别到的验证码会高亮显示，可一键复制。</p>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function EmailReader({
  email,
  adminMode,
  onClose,
  onDelete,
  onToggleStar,
}: {
  email: EmailDetail;
  adminMode: boolean;
  onClose: () => void;
  onDelete?: () => void;
  onToggleStar: () => void;
}) {
  const [viewHtml, setViewHtml] = useState(email.hasHtml);
  const htmlSrc = adminMode ? `/api/admin/mail-html/${email.id}` : `/api/mail/emails/${email.id}/html`;
  const sender = email.fromName || email.from;
  const iconBtn = "grid size-8 place-items-center rounded-lg transition-colors";

  return (
    <article aria-label={email.subject || "（无主题）"} className="flex w-full flex-col">
      {/* Back to the list on narrow layouts, where the reader replaces it. */}
      <div className="border-b border-line bg-desk/30 px-2.5 py-2 @3xl:hidden">
        <Button variant="ghost" size="sm" onClick={onClose}>
          <Icon name="back" className="size-4" />
          返回收件箱
        </Button>
      </div>

      {/* Header */}
      <header className="border-b border-line px-5 pt-5 pb-4 sm:px-6">
        <div className="flex items-start gap-3">
          <h3 className="min-w-0 flex-1 text-[1.0625rem] leading-snug font-semibold tracking-tight break-words sm:text-lg">{email.subject || "（无主题）"}</h3>
          <div className="-mt-1 -mr-2 flex shrink-0 items-center gap-0.5">
            <button
              type="button"
              onClick={onToggleStar}
              className={`${iconBtn} hover:bg-ink/[0.05] ${email.isStarred ? starOn : "text-ink-soft hover:text-[#c98a12]"}`}
              title={email.isStarred ? "取消星标" : "标记星标"}
              aria-label={email.isStarred ? "取消星标" : "标记星标"}
              aria-pressed={email.isStarred}
            >
              <StarIcon filled={email.isStarred} />
            </button>
            {adminMode && onDelete && (
              <button type="button" onClick={onDelete} className={`${iconBtn} text-ink-soft hover:bg-void/[0.06] hover:text-void`} title="永久删除此邮件" aria-label="删除邮件">
                <Icon name="trash" className="size-4" />
              </button>
            )}
            <button type="button" onClick={onClose} className={`${iconBtn} hidden text-ink-soft hover:bg-ink/[0.06] hover:text-ink @3xl:grid`} title="关闭" aria-label="关闭">
              <Icon name="x" className="size-4" />
            </button>
          </div>
        </div>
        <div className="mt-4 flex items-center gap-3">
          <Avatar text={sender} />
          <dl className="min-w-0 flex-1 text-[0.8125rem] leading-5">
            <div className="flex min-w-0 items-baseline">
              <dt className="sr-only">发件人：</dt>
              <dd className="min-w-0 truncate">
                <span className="font-semibold text-ink">{sender}</span>
                {email.fromName && <span className="ml-1.5 font-mono text-[0.71875rem] text-ink-soft">&lt;{email.from}&gt;</span>}
              </dd>
            </div>
            <div className="flex min-w-0 items-baseline gap-1 text-ink-soft">
              <dt className="shrink-0">收件人：</dt>
              <dd className="min-w-0 truncate font-mono text-[0.71875rem]">{email.to || email.mailbox}</dd>
            </div>
          </dl>
          <p className="shrink-0 self-start text-xs text-ink-faint tabular-nums">
            <span className="sr-only">时间：</span>
            {formatDate(email.createdAt)}
          </p>
        </div>
      </header>

      {/* Verification code highlight banner if detected */}
      {email.verificationCode && (
        <div className="mx-5 mt-5 flex flex-wrap items-center gap-x-4 gap-y-2.5 rounded-xl border border-stamp/25 bg-stamp/[0.05] px-4 py-3 sm:mx-6">
          <div className="min-w-0 flex-1">
            <p className="eyebrow !text-stamp">提取到验证码</p>
            <p className="mt-1.5 font-mono text-2xl leading-none font-semibold tracking-[0.16em] text-stamp tabular-nums">{email.verificationCode}</p>
          </div>
          <CopyCode value={email.verificationCode} />
        </div>
      )}

      {/* Body View Mode Selector */}
      {email.hasHtml && (
        <div className="px-5 pt-5 sm:px-6">
          <Segmented
            label="正文显示方式"
            options={[
              ["html", "富文本预览（安全沙箱）"],
              ["text", "纯文本"],
            ]}
            value={viewHtml ? "html" : "text"}
            onChange={(v) => setViewHtml(v === "html")}
          />
        </div>
      )}

      {/* Body Content */}
      <div className="flex-1 px-5 py-5 sm:px-6">
        {viewHtml && email.hasHtml ? (
          <iframe
            src={htmlSrc}
            title="邮件富文本预览"
            className="h-[460px] w-full rounded-xl border border-line bg-white shadow-xs"
            sandbox="allow-popups allow-popups-to-escape-sandbox"
          />
        ) : (
          <div className="text-sm leading-7 break-words whitespace-pre-wrap text-ink selection:bg-stamp/20">{email.text || email.preview || "（正文为空）"}</div>
        )}
      </div>
    </article>
  );
}
