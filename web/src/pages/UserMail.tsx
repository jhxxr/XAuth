import { useEffect, useState } from "react";
import { api, errorMessage, type MyMailbox } from "../api";
import { Icon } from "../components/Icon";
import { MailViewer } from "../components/MailViewer";
import { AppShell } from "../components/Shell";
import { EmptyState, Notice } from "../components/ui";
import { formatDate } from "../session";

export function UserMailPage() {
  const [configured, setConfigured] = useState<boolean | null>(null);
  const [mailboxes, setMailboxes] = useState<MyMailbox[] | null>(null);
  const [activeAddress, setActiveAddress] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  const loadData = async () => {
    setLoading(true);
    setError("");
    try {
      const st = await api.mailStatus();
      setConfigured(st.configured);
      if (st.configured) {
        const r = await api.myMailboxes();
        setMailboxes(r.mailboxes);
        if (r.mailboxes.length === 1 && !activeAddress) {
          setActiveAddress(r.mailboxes[0].address);
        }
      }
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadData();
  }, []);

  return (
    <AppShell title="我的邮箱" description="查看分配给你的专用收信邮箱、收到的邮件与最新验证码。">
      {error && (
        <div className="mb-5">
          <Notice>{error}</Notice>
        </div>
      )}

      {loading ? (
        <div className="grid gap-4 sm:grid-cols-2" aria-busy="true">
          <span className="sr-only">正在载入邮箱...</span>
          {[0, 1].map((i) => (
            <div key={i} aria-hidden="true" className="panel flex items-center gap-4 p-5">
              <span className="size-11 shrink-0 animate-pulse rounded-xl bg-ink/[0.06] motion-reduce:animate-none" />
              <span className="flex-1 space-y-2.5">
                <span className="block h-3.5 w-2/3 animate-pulse rounded bg-ink/[0.06] motion-reduce:animate-none" />
                <span className="block h-3 w-1/3 animate-pulse rounded bg-ink/[0.06] motion-reduce:animate-none" />
              </span>
            </div>
          ))}
        </div>
      ) : configured === false ? (
        <EmptyState icon="inbox" title="尚未连接 xmail" description="系统管理员尚未配置邮件中台，请稍后再试。" />
      ) : activeAddress ? (
        <MailViewer address={activeAddress} onBack={mailboxes && mailboxes.length > 1 ? () => setActiveAddress(null) : undefined} />
      ) : mailboxes && mailboxes.length === 0 ? (
        <EmptyState icon="inbox" title="暂无分配的邮箱" description="管理员尚未为你分配任何收信邮箱。分配后你可以在这里查看收到的邮件和最新验证码。" />
      ) : (
        <section aria-labelledby="my-mailboxes-title">
          <div className="mb-3 flex items-baseline justify-between gap-3">
            <h2 id="my-mailboxes-title" className="eyebrow">
              分配给你的邮箱 · {mailboxes?.length ?? 0}
            </h2>
            <p className="hidden text-[0.8125rem] text-ink-soft sm:block">点击进入收件箱</p>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            {mailboxes?.map((m) => (
              <button
                type="button"
                key={m.address}
                onClick={() => setActiveAddress(m.address)}
                className="panel group flex w-full items-center gap-4 p-5 text-left transition-[border-color,box-shadow] hover:border-stamp/40 hover:shadow-[0_0_0_3px_rgb(90_63_192/0.08),var(--shadow-card)]"
              >
                <span aria-hidden="true" className="grid size-11 shrink-0 place-items-center rounded-xl border border-stamp/20 bg-stamp/[0.06] text-stamp shadow-xs">
                  <Icon name="mail" className="size-5" />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-mono text-[0.84375rem] font-semibold text-ink" title={m.address}>
                    {m.address}
                  </span>
                  <span className="mt-1 block text-[0.8125rem] text-ink-soft">分配于 {formatDate(m.assignedAt)}</span>
                </span>
                <span className="flex shrink-0 items-center gap-1 text-[0.8125rem] font-medium text-stamp">
                  <span className="hidden sm:inline">进入收件箱</span>
                  <Icon name="chevron" className="size-4 transition-transform group-hover:translate-x-0.5" />
                </span>
              </button>
            ))}
          </div>
        </section>
      )}
    </AppShell>
  );
}
