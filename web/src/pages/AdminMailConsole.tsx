import { useEffect, useId, useRef, useState } from "react";
import { api, errorMessage } from "../api";
import { useToast } from "../components/Dialog";
import { Icon } from "../components/Icon";
import { MailViewer } from "../components/MailViewer";
import { AppShell } from "../components/Shell";
import { Button, Check, Copyable, EmptyState, Notice } from "../components/ui";
import { Link } from "../router";

type CurrentMailbox = { address: string; password?: string | null };

export function AdminMailConsolePage() {
  const toast = useToast();
  const id = useId();
  const [configured, setConfigured] = useState<boolean | null>(null);
  const [domains, setDomains] = useState<string[]>([]);
  const [domain, setDomain] = useState("");
  const [current, setCurrent] = useState<CurrentMailbox | null>(null);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [loading, setLoading] = useState(true);
  const [action, setAction] = useState<"" | "create" | "delete">("");
  const [error, setError] = useState("");
  const [reload, setReload] = useState(0);
  const actionRef = useRef(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError("");
    const load = async () => {
      try {
        const status = await api.mailStatus();
        if (cancelled) return;
        setConfigured(status.configured);
        if (!status.configured) return;
        const result = await api.adminMail<{ domains: string[]; defaultDomain: string }>("GET", "/system/domains");
        if (cancelled) return;
        const available = result.domains || [];
        setDomains(available);
        setDomain((selected) => available.includes(selected) ? selected : available.includes(result.defaultDomain) ? result.defaultDomain : available[0] || "");
      } catch (err) {
        if (!cancelled) setError(errorMessage(err));
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    void load();
    return () => { cancelled = true; };
  }, [reload]);

  const busy = loading || action !== "";
  const create = async () => {
    if (loading || actionRef.current) return;
    if (!domains.includes(domain)) {
      setError("请先选择邮箱后缀。");
      return;
    }
    actionRef.current = true;
    setAction("create");
    setError("");
    try {
      // xmail's automatic generator supplies both the random address and its
      // password. Always request exactly one; creating never deletes a mailbox.
      const result = await api.adminMail<{ items: CurrentMailbox[] }>("POST", "/mailboxes/batch", { count: 1, domain });
      if (result.items?.length !== 1 || !result.items[0].address) throw new Error("xmail 未返回一个有效的新邮箱，请到邮件中台核对。");
      setCurrent(result.items[0]);
      toast("新邮箱已创建，旧邮箱会保留在邮件中台");
    } catch (err) {
      setError(`${errorMessage(err)} 如请求已发送，请先到邮件中台核对，避免重复创建。`);
    } finally {
      actionRef.current = false;
      setAction("");
    }
  };

  const remove = async () => {
    if (!current || loading || actionRef.current) return;
    const address = current.address;
    actionRef.current = true;
    setAction("delete");
    setError("");
    try {
      await api.adminMail("DELETE", `/mailboxes/${encodeURIComponent(address)}`);
      setCurrent(null);
      toast("当前邮箱已删除，可在邮件中台回收站恢复");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      actionRef.current = false;
      setAction("");
    }
  };

  return (
    <AppShell
      title="邮箱操作台"
      description="创建一个邮箱，直接查看密码、邮件和验证码。创建新的会保留旧邮箱；删除只影响当前邮箱。"
      actions={<>
        <Button variant="secondary" size="sm" disabled={busy} onClick={() => setReload((value) => value + 1)}><Icon name="refresh" className="size-3.5" />重新加载后缀</Button>
        <Link href="/admin/mail" className="inline-flex items-center gap-1.5 rounded-lg text-sm font-medium text-stamp hover:underline"><Icon name="inbox" />邮件中台</Link>
      </>}
    >
      <div className="space-y-5">
        {error && <Notice>{error}</Notice>}
        {configured === false ? (
          <EmptyState icon="inbox" title="尚未连接 xmail" description="先在系统设置中配置 xmail 地址与 API Key，再使用自建邮箱操作台。" action={<Link href="/admin/settings" className="text-sm font-medium text-stamp hover:underline">前往系统设置</Link>} />
        ) : configured === null ? (
          <div role="status" className="panel p-6 text-sm text-ink-soft">{loading ? "正在连接邮件服务…" : "连接失败，请重新加载。"}</div>
        ) : (
          <>
            <section className="panel space-y-5 p-4 sm:p-5" aria-label="邮箱操作">
              <form onSubmit={(event) => { event.preventDefault(); void create(); }} className="flex flex-wrap items-end gap-3">
                <div className="min-w-0 flex-1 basis-64">
                  <label htmlFor={`${id}-domain`} className="mb-1.5 block text-[0.8125rem] font-medium">新邮箱后缀</label>
                  <select id={`${id}-domain`} value={domain} onChange={(event) => setDomain(event.target.value)} disabled={busy || !domains.length} className="field min-w-0 font-mono text-sm" required>
                    {!domains.length && <option value="">{loading ? "正在加载…" : "暂无可用后缀"}</option>}
                    {domains.map((item) => <option key={item} value={item}>@{item}</option>)}
                  </select>
                </div>
                <Button type="submit" busy={action === "create"} disabled={busy || !domains.includes(domain)}>
                  {action !== "create" && <Icon name="plus" />}创建新的
                </Button>
                <Button variant="danger" disabled={busy || !current} busy={action === "delete"} onClick={() => void remove()} title="删除当前邮箱，不会创建或切换到其他邮箱">
                  {action !== "delete" && <Icon name="trash" />}删除此邮箱
                </Button>
              </form>
              {!loading && !domains.length ? (
                <p className="text-sm text-ink-soft">尚无收件域名，请在<Link href="/admin/mail" className="mx-1 text-stamp hover:underline">邮件中台 → 邮件设置</Link>中添加后缀后重新加载。</p>
              ) : (
                <p className="text-xs leading-5 text-ink-soft">地址和密码由 xmail 自动生成。点击“创建新的”只会新建并使用一个邮箱，旧邮箱可在邮件中台继续管理。</p>
              )}
              {current && <MailboxCredentials key={current.address} mailbox={current} />}
            </section>

            {current ? (
              <section className="min-w-0 space-y-4" aria-label="当前邮箱工作区">
                <Check label="每 10 秒自动收信（后台暂停）" checked={autoRefresh} onChange={setAutoRefresh} />
                <MailViewer key={current.address} address={current.address} adminMode autoRefreshSeconds={autoRefresh ? 10 : 0} showCleanup={false} showAddress={false} />
              </section>
            ) : (
              <EmptyState icon="mail" title="点击“创建新的”开始使用" description="选好后缀，一键创建邮箱后，这里会显示该邮箱的邮件列表；删除后不会自动创建另一个邮箱。" />
            )}
          </>
        )}
      </div>
    </AppShell>
  );
}

function MailboxCredentials({ mailbox }: { mailbox: CurrentMailbox }) {
  const [password, setPassword] = useState(mailbox.password || "");
  const [loading, setLoading] = useState(!mailbox.password);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);

  useEffect(() => {
    if (mailbox.password) return;
    let cancelled = false;
    setLoading(true);
    setError("");
    api.adminMail<{ password: string | null }>("GET", `/mailboxes/${encodeURIComponent(mailbox.address)}/password`)
      .then((result) => {
        if (cancelled) return;
        if (!result.password) throw new Error("xmail 暂未返回该邮箱的密码。");
        setPassword(result.password);
      })
      .catch((err) => { if (!cancelled) setError(errorMessage(err)); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [mailbox.address, mailbox.password, retry]);

  return (
    <div className="grid min-w-0 gap-4 border-t border-line pt-5 sm:grid-cols-2" aria-label="当前邮箱凭据">
      <div className="min-w-0 space-y-1.5">
        <p className="text-[0.8125rem] font-medium">邮箱地址</p>
        <Copyable value={mailbox.address} label="当前邮箱地址" wrap />
      </div>
      <div className="min-w-0 space-y-1.5">
        <p className="text-[0.8125rem] font-medium">邮箱密码</p>
        {password ? <Copyable value={password} label="当前邮箱密码" wrap /> : loading ? (
          <p role="status" className="py-2 text-sm text-ink-soft">正在读取邮箱密码…</p>
        ) : (
          <div className="space-y-2"><Notice>{error || "暂时无法读取邮箱密码。"}</Notice><Button variant="secondary" size="sm" onClick={() => setRetry((value) => value + 1)}>重试读取密码</Button></div>
        )}
      </div>
    </div>
  );
}
