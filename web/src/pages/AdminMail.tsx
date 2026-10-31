import { useEffect, useId, useState, type FormEvent, type ReactNode } from "react";
import { api, errorMessage, type MailGrant, type User } from "../api";
import { Dialog, useToast } from "../components/Dialog";
import { Icon, type IconName } from "../components/Icon";
import { MailViewer } from "../components/MailViewer";
import { AppShell } from "../components/Shell";
import {
  Badge,
  Button,
  Check,
  Copyable,
  Empty,
  EmptyState,
  Field,
  GroupLabel,
  IconTile,
  ListRow,
  Notice,
  RadioCard,
  SearchField,
  Section,
  Segmented,
  TextArea,
  UserChecklist,
} from "../components/ui";
import { formatDate } from "../session";

type MailboxItem = {
  address: string;
  note: string | null;
  createdAt: number;
  expiresAt: number | null;
  lastLoginAt: number | null;
  isActive: boolean;
  hasPassword: boolean;
  deletedAt: number | null;
};

type ServiceItem = {
  id: string;
  name: string;
  loginUrl: string;
  note: string | null;
  isCustom: boolean;
  templateId: string | null;
  expiresAt: number | null;
};

type TemplateItem = {
  id: string;
  name: string;
  loginUrl: string;
  note: string | null;
  createdAt: number;
};

type SystemSettings = {
  retentionDays: number;
  defaultDomain: string;
  domains: string[];
};

type SystemStats = {
  totalMailboxes: number;
  totalEmails: number;
  deletedMailboxes: number;
};

type SystemLog = {
  id: string;
  action: string;
  target: string | null;
  details: unknown;
  createdAt: number;
};

type Tab = "mailboxes" | "services" | "trash" | "settings";

const tabDescriptions: Record<Tab, string> = {
  mailboxes: "管理 xmail 收信邮箱，并分配给 XAuth 用户查看邮件与验证码。",
  services: "全局服务模板可一键关联到各邮箱，方便用户一键直达对应平台的登录入口。",
  trash: "被软删除的邮箱保存在回收站中，可随时恢复或永久彻底删除。",
  settings: "收件域名、邮件保留策略与 xmail 审计日志。",
};

/** Pulsing placeholder rows while a table loads. */
function TableSkeleton({ rows = 4 }: { rows?: number }) {
  return (
    <div className="panel divide-y divide-line/70" aria-busy="true">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} aria-hidden="true" className="flex items-center gap-4 px-5 py-4">
          <span className="h-3.5 w-1/4 animate-pulse rounded bg-ink/[0.06] motion-reduce:animate-none" />
          <span className="h-3 w-1/6 animate-pulse rounded bg-ink/[0.05] motion-reduce:animate-none" />
          <span className="ml-auto h-3 w-16 animate-pulse rounded bg-ink/[0.05] motion-reduce:animate-none" />
        </div>
      ))}
    </div>
  );
}

/** Left: description or filters. Right: actions. Wraps on narrow screens. */
function Toolbar({ children, actions }: { children: ReactNode; actions?: ReactNode }) {
  return (
    <div className="mb-4 flex flex-wrap items-center justify-between gap-x-4 gap-y-3">
      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">{children}</div>
      {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );
}

export function AdminMailPage() {
  const [configured, setConfigured] = useState<boolean | null>(null);
  const [tab, setTab] = useState<Tab>("mailboxes");
  const [error, setError] = useState("");

  // Viewing inbox
  const [viewingInbox, setViewingInbox] = useState<string | null>(null);

  useEffect(() => {
    api
      .mailStatus()
      .then((r) => setConfigured(r.configured))
      .catch((e) => setError(errorMessage(e)));
  }, []);

  if (configured === false) {
    return (
      <AppShell title="邮件中台" description="通过 xmail 统一管理收信邮箱、用户分配与验证码。">
        <EmptyState
          icon="inbox"
          title="尚未连接 xmail"
          description={
            <>
              XAuth 尚未配置 xmail。请在服务器环境变量中设置 <code className="rounded bg-ink/[0.06] px-1 py-0.5 font-mono text-[0.71875rem] text-ink">XMAIL_BASE_URL</code> 与{" "}
              <code className="rounded bg-ink/[0.06] px-1 py-0.5 font-mono text-[0.71875rem] text-ink">XMAIL_API_KEY</code> 后重启 XAuth 服务。
            </>
          }
        />
      </AppShell>
    );
  }

  return (
    <AppShell title="邮件中台" description={viewingInbox ? "以管理员身份查看收件箱，查看操作会记入审计日志。" : tabDescriptions[tab]}>
      {error && (
        <div className="mb-5">
          <Notice>{error}</Notice>
        </div>
      )}

      {!viewingInbox && (
        <div className="mb-6">
          <Segmented
            label="邮件中台视图"
            options={[
              ["mailboxes", "邮箱"],
              ["services", "服务模板"],
              ["trash", "回收站"],
              ["settings", "邮件设置"],
            ]}
            value={tab}
            onChange={setTab}
          />
        </div>
      )}

      {viewingInbox ? (
        <MailViewer address={viewingInbox} adminMode onBack={() => setViewingInbox(null)} />
      ) : tab === "mailboxes" ? (
        <MailboxesTab onOpenInbox={(addr) => setViewingInbox(addr)} />
      ) : tab === "services" ? (
        <ServicesTab />
      ) : tab === "trash" ? (
        <TrashTab />
      ) : (
        <SettingsTab />
      )}
    </AppShell>
  );
}

// -------------------------------------------------------------
// Mailboxes Tab
// -------------------------------------------------------------

function MailboxesTab({ onOpenInbox }: { onOpenInbox: (addr: string) => void }) {
  const toast = useToast();
  const [mailboxes, setMailboxes] = useState<MailboxItem[] | null>(null);
  const [grants, setGrants] = useState<Record<string, string[]>>({});
  const [users, setUsers] = useState<User[]>([]);
  const [domains, setDomains] = useState<string[]>([]);
  const [search, setSearch] = useState("");
  const [filterStatus, setFilterStatus] = useState<"all" | "active" | "disabled" | "expired" | "unassigned">("all");
  const [selected, setSelected] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [creatingSingle, setCreatingSingle] = useState(false);
  const [creatingBatch, setCreatingBatch] = useState(false);
  const [editingMailbox, setEditingMailbox] = useState<string | null>(null);

  const loadData = async () => {
    try {
      const [mbRes, grRes, uRes, domRes] = await Promise.all([
        api.adminMail<{ items: MailboxItem[] }>("GET", `/mailboxes?limit=500`),
        api.mailGrants(),
        api.users(),
        api.adminMail<{ domains: string[]; defaultDomain: string }>("GET", "/system/domains").catch(() => ({ domains: [], defaultDomain: "" })),
      ]);
      setMailboxes(mbRes.items || []);
      const gMap: Record<string, string[]> = {};
      grRes.grants.forEach((g) => {
        gMap[g.address] = g.userIds;
      });
      setGrants(gMap);
      setUsers(uRes.users || []);
      setDomains(domRes.domains || []);
      setSelected([]);
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  useEffect(() => {
    void loadData();
  }, []);

  const userMap = new Map(users.map((u) => [u.id, u.name || u.email]));

  const softDelete = async (addr: string) => {
    if (!window.confirm(`确认将邮箱 ${addr} 移入回收站？`)) return;
    try {
      await api.adminMail("DELETE", `/mailboxes/${encodeURIComponent(addr)}`);
      toast(`已将 ${addr} 移入回收站`);
      void loadData();
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  const batchDelete = async () => {
    if (!window.confirm(`确认将选中的 ${selected.length} 个邮箱移入回收站？`)) return;
    try {
      await api.adminMail("DELETE", "/mailboxes/batch", { addresses: selected });
      toast(`已批量移入回收站 ${selected.length} 个邮箱`);
      setSelected([]);
      void loadData();
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  const filtered = mailboxes?.filter((m) => {
    const matchSearch =
      m.address.toLowerCase().includes(search.trim().toLowerCase()) ||
      (m.note || "").toLowerCase().includes(search.trim().toLowerCase());
    if (!matchSearch) return false;

    const assigned = grants[m.address] || [];
    const isExpired = m.expiresAt ? m.expiresAt * 1000 < Date.now() : false;

    if (filterStatus === "active") return m.isActive && !isExpired;
    if (filterStatus === "disabled") return !m.isActive;
    if (filterStatus === "expired") return isExpired;
    if (filterStatus === "unassigned") return assigned.length === 0;
    return true;
  });

  const toggleSelectAll = () => {
    if (!filtered) return;
    if (selected.length === filtered.length && filtered.length > 0) {
      setSelected([]);
    } else {
      setSelected(filtered.map((m) => m.address));
    }
  };

  const toggleSelectOne = (addr: string) => {
    setSelected((prev) => (prev.includes(addr) ? prev.filter((a) => a !== addr) : [...prev, addr]));
  };

  const allChecked = !!filtered && filtered.length > 0 && selected.length === filtered.length;

  return (
    <div>
      {error && (
        <div className="mb-4">
          <Notice>{error}</Notice>
        </div>
      )}

      <Toolbar
        actions={
          <>
            <Button variant="secondary" size="sm" onClick={() => setCreatingBatch(true)}>
              <Icon name="apps" className="size-3.5" />
              批量创建
            </Button>
            <Button size="sm" onClick={() => setCreatingSingle(true)}>
              <Icon name="plus" className="size-3.5" />
              添加邮箱
            </Button>
          </>
        }
      >
        <SearchField value={search} onChange={setSearch} placeholder="搜索邮箱地址或备注..." label="搜索邮箱" className="w-full sm:w-64" />
        <Segmented
          label="按状态筛选"
          options={[
            ["all", "全部"],
            ["active", "正常"],
            ["disabled", "已停用"],
            ["expired", "已过期"],
            ["unassigned", "未分配"],
          ]}
          value={filterStatus}
          onChange={setFilterStatus}
        />
      </Toolbar>

      {mailboxes === null && !error ? (
        <TableSkeleton />
      ) : mailboxes?.length === 0 ? (
        <EmptyState
          icon="inbox"
          title="还没有邮箱"
          description="创建邮箱后即可分配给 XAuth 用户，用户会在“我的邮箱”中收到邮件与验证码。"
          action={
            <Button size="sm" onClick={() => setCreatingSingle(true)}>
              <Icon name="plus" className="size-3.5" />
              添加邮箱
            </Button>
          }
        />
      ) : (
        <div className="panel overflow-hidden">
          {/* Selection bar */}
          {selected.length > 0 && (
            <div className="flex flex-wrap items-center gap-3 border-b border-stamp/15 bg-stamp/[0.045] px-4 py-2.5">
              <p className="text-[0.8125rem] font-medium text-ink">
                已选择 <span className="tabular-nums">{selected.length}</span> 个邮箱
              </p>
              <Button variant="ghost" size="sm" onClick={() => setSelected([])}>
                取消选择
              </Button>
              <Button variant="danger" size="sm" className="ml-auto" onClick={() => void batchDelete()}>
                <Icon name="trash" className="size-3.5" />
                批量删除选中的 {selected.length} 个邮箱
              </Button>
            </div>
          )}
          <div className="overflow-x-auto">
            <table className="data-table min-w-[820px]">
              <thead>
                <tr>
                  <th scope="col" className="w-10 !pr-0">
                    <input type="checkbox" className="size-4 accent-ink align-middle" checked={allChecked} onChange={toggleSelectAll} aria-label="全选" />
                  </th>
                  {["邮箱地址", "备注", "状态", "分配给 (XAuth 用户)", ""].map((h, i) => (
                    <th key={i} scope="col">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {filtered?.map((m) => {
                  const assigned = grants[m.address] || [];
                  const isExpired = m.expiresAt ? m.expiresAt * 1000 < Date.now() : false;
                  const isChecked = selected.includes(m.address);
                  return (
                    <tr key={m.address} className={isChecked ? "!bg-stamp/[0.035]" : ""}>
                      <td className="w-10 !pr-0">
                        <input
                          type="checkbox"
                          className="size-4 accent-ink align-middle"
                          checked={isChecked}
                          onChange={() => toggleSelectOne(m.address)}
                          aria-label={`选择 ${m.address}`}
                        />
                      </td>
                      <td>
                        <button
                          type="button"
                          onClick={() => onOpenInbox(m.address)}
                          className="group flex max-w-[18rem] items-center gap-2 rounded text-left"
                          title="点击打开收件箱"
                        >
                          <span className="truncate font-mono text-[0.78125rem] font-semibold text-ink underline-offset-2 group-hover:text-stamp group-hover:underline">{m.address}</span>
                        </button>
                        {m.expiresAt && (
                          <p className={`mt-0.5 text-xs ${isExpired ? "text-void" : "text-ink-faint"}`}>
                            {isExpired ? "已于" : "到期"} {formatDate(new Date(m.expiresAt * 1000).toISOString())}
                            {isExpired ? " 过期" : ""}
                          </p>
                        )}
                      </td>
                      <td className="text-ink-soft">
                        <p className="max-w-[14rem] truncate" title={m.note || undefined}>
                          {m.note || <span className="text-ink-faint">—</span>}
                        </p>
                      </td>
                      <td>
                        {!m.isActive ? (
                          <Badge tone="void" dot>
                            已停用
                          </Badge>
                        ) : isExpired ? (
                          <Badge tone="ink" dot>
                            已过期
                          </Badge>
                        ) : (
                          <Badge tone="seal" dot>
                            正常
                          </Badge>
                        )}
                      </td>
                      <td>
                        {assigned.length === 0 ? (
                          <span className="text-[0.8125rem] text-ink-faint">未分配</span>
                        ) : (
                          <div className="flex max-w-[16rem] flex-wrap gap-1">
                            {assigned.map((uid) => (
                              <Badge key={uid} tone="plain">
                                {userMap.get(uid) || uid}
                              </Badge>
                            ))}
                          </div>
                        )}
                      </td>
                      <td className="text-right whitespace-nowrap">
                        <div className="flex justify-end gap-0.5">
                          <Button variant="ghost" size="sm" className="!text-stamp hover:!bg-stamp/[0.07]" onClick={() => onOpenInbox(m.address)}>
                            收件箱
                          </Button>
                          <Button variant="ghost" size="sm" onClick={() => setEditingMailbox(m.address)}>
                            管理 / 分配
                          </Button>
                          <Button variant="ghost" size="sm" className="hover:text-void" onClick={() => void softDelete(m.address)}>
                            删除
                          </Button>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          {filtered?.length === 0 && <p className="p-8 text-center text-sm text-ink-soft">暂无匹配的邮箱。</p>}
          {mailboxes && mailboxes.length > 0 && (
            <div className="border-t border-line bg-desk/30 px-4 py-2.5 text-xs text-ink-soft">
              显示 <span className="tabular-nums">{filtered?.length ?? 0}</span> / <span className="tabular-nums">{mailboxes.length}</span> 个邮箱
            </div>
          )}
        </div>
      )}

      {creatingSingle && (
        <CreateMailboxDialog
          open={creatingSingle}
          domains={domains}
          onClose={() => setCreatingSingle(false)}
          onCreated={() => {
            setCreatingSingle(false);
            void loadData();
          }}
        />
      )}

      {creatingBatch && (
        <BatchCreateDialog
          open={creatingBatch}
          domains={domains}
          onClose={() => setCreatingBatch(false)}
          onCreated={() => {
            setCreatingBatch(false);
            void loadData();
          }}
        />
      )}

      {editingMailbox && (
        <MailboxDetailDialog
          address={editingMailbox}
          users={users}
          currentAssigned={grants[editingMailbox] || []}
          onClose={() => setEditingMailbox(null)}
          onUpdated={() => {
            setEditingMailbox(null);
            void loadData();
          }}
          onOpenInbox={(addr) => {
            setEditingMailbox(null);
            onOpenInbox(addr);
          }}
        />
      )}
    </div>
  );
}

// -------------------------------------------------------------
// Create Single Mailbox Dialog
// -------------------------------------------------------------

function CreateMailboxDialog({
  open,
  domains,
  onClose,
  onCreated,
}: {
  open: boolean;
  domains: string[];
  onClose: () => void;
  onCreated: () => void;
}) {
  const toast = useToast();
  const id = useId();
  const [localPart, setLocalPart] = useState("");
  const [domain, setDomain] = useState(domains[0] || "");
  const [note, setNote] = useState("");
  const [customPassword, setCustomPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const payload: Record<string, unknown> = {
        localPart: localPart.trim(),
        note: note.trim() || undefined,
      };
      if (domain) payload.domain = domain;
      if (customPassword) payload.password = customPassword;
      const res = await api.adminMail<{ mailbox: MailboxItem; password?: string }>("POST", "/mailboxes", payload);
      toast(`邮箱已创建：${res.mailbox.address}`);
      onCreated();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="添加邮箱"
      description="在 xmail 中创建一个新的收信邮箱。"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button type="submit" form="create-mb" busy={busy}>
            创建邮箱
          </Button>
        </>
      }
    >
      <form id="create-mb" onSubmit={submit} className="space-y-4">
        {error && <Notice>{error}</Notice>}
        <div>
          <label htmlFor={`${id}-local`} className="mb-1.5 block text-[0.8125rem] font-medium text-ink">
            邮箱地址
          </label>
          <div className="flex items-center gap-2">
            <input
              id={`${id}-local`}
              value={localPart}
              onChange={(e) => setLocalPart(e.target.value)}
              placeholder="前缀，如 support"
              required
              className="field min-w-0 flex-1 font-mono text-[0.875rem]"
            />
            <span aria-hidden="true" className="font-mono text-ink-faint">
              @
            </span>
            {domains.length > 0 ? (
              <select value={domain} onChange={(e) => setDomain(e.target.value)} aria-label="域名" className="field w-[46%] max-w-[220px] font-mono text-[0.8125rem]">
                {domains.map((d) => (
                  <option key={d} value={d}>
                    {d}
                  </option>
                ))}
              </select>
            ) : (
              <input
                value={domain}
                onChange={(e) => setDomain(e.target.value)}
                placeholder="域名"
                aria-label="域名"
                required
                className="field w-[46%] max-w-[220px] font-mono text-[0.8125rem]"
              />
            )}
          </div>
        </div>
        <Field label="备注" value={note} onChange={(e) => setNote(e.target.value)} placeholder="可选，如 客户A" />
        <Field
          label="自定义密码"
          type="password"
          value={customPassword}
          onChange={(e) => setCustomPassword(e.target.value)}
          placeholder="留空则自动生成高强度随机密码"
          autoComplete="new-password"
        />
      </form>
    </Dialog>
  );
}

// -------------------------------------------------------------
// Batch Create Dialog
// -------------------------------------------------------------

function BatchCreateDialog({
  open,
  domains,
  onClose,
  onCreated,
}: {
  open: boolean;
  domains: string[];
  onClose: () => void;
  onCreated: () => void;
}) {
  const id = useId();
  const [mode, setMode] = useState<"count" | "addresses">("count");
  const [count, setCount] = useState(5);
  const [domain, setDomain] = useState(domains[0] || "");
  const [addressesText, setAddressesText] = useState("");
  const [resultItems, setResultItems] = useState<{ address: string; password?: string }[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      if (mode === "count") {
        const res = await api.adminMail<{ items: { address: string; password?: string }[] }>(
          "POST",
          "/mailboxes/batch",
          { count, domain: domain || undefined },
        );
        setResultItems(res.items);
      } else {
        const list = addressesText
          .split("\n")
          .map((s) => s.trim())
          .filter(Boolean);
        const res = await api.adminMail<{ items: { address: string; password?: string }[] }>(
          "POST",
          "/mailboxes/batch",
          { addresses: list },
        );
        setResultItems(res.items);
      }
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={resultItems ? "批量创建完成" : "批量创建邮箱"}
      description={resultItems ? undefined : "一次生成多个收信邮箱，密码会自动生成。"}
      footer={
        resultItems ? (
          <Button
            onClick={() => {
              onCreated();
            }}
          >
            完成并返回
          </Button>
        ) : (
          <>
            <Button variant="ghost" onClick={onClose}>
              取消
            </Button>
            <Button type="submit" form="batch-mb" busy={busy}>
              开始生成
            </Button>
          </>
        )
      }
    >
      {resultItems ? (
        <div className="space-y-4">
          <Notice tone="info">共创建了 {resultItems.length} 个邮箱。密码仅在此展示一次，请及时保存：</Notice>
          <div className="max-h-72 divide-y divide-line/70 overflow-y-auto rounded-xl border border-line bg-white">
            {resultItems.map((item) => (
              <div key={item.address} className="flex flex-wrap items-center gap-x-3 gap-y-1.5 px-3.5 py-2.5">
                <span className="min-w-0 flex-1 truncate font-mono text-[0.78125rem] font-medium text-ink" title={item.address}>
                  {item.address}
                </span>
                {item.password ? (
                  <div className="w-full sm:w-48">
                    <Copyable value={item.password} label={`${item.address} 的密码`} />
                  </div>
                ) : (
                  <span className="text-xs text-ink-faint">（无密码）</span>
                )}
              </div>
            ))}
          </div>
        </div>
      ) : (
        <form id="batch-mb" onSubmit={submit} className="space-y-5">
          {error && <Notice>{error}</Notice>}
          <fieldset>
            <GroupLabel>生成方式</GroupLabel>
            <div className="grid gap-2.5 sm:grid-cols-2">
              <RadioCard name={`${id}-mode`} checked={mode === "count"} onChange={() => setMode("count")} icon="refresh" title="按数量随机生成" description="随机前缀，统一使用所选域名。" />
              <RadioCard name={`${id}-mode`} checked={mode === "addresses"} onChange={() => setMode("addresses")} icon="edit" title="指定邮箱地址列表" description="逐行填写完整的邮箱地址。" />
            </div>
          </fieldset>

          {mode === "count" ? (
            <div className="grid gap-4 sm:grid-cols-[minmax(0,10rem)_1fr]">
              <Field
                label="生成数量 (1~500)"
                type="number"
                min={1}
                max={500}
                value={count}
                onChange={(e) => setCount(Number(e.target.value))}
                required
              />
              <div>
                <label htmlFor={`${id}-domain`} className="mb-1.5 block text-[0.8125rem] font-medium text-ink">
                  域名
                </label>
                <select id={`${id}-domain`} value={domain} onChange={(e) => setDomain(e.target.value)} className="field font-mono text-[0.8125rem]">
                  {domains.map((d) => (
                    <option key={d} value={d}>
                      {d}
                    </option>
                  ))}
                </select>
              </div>
            </div>
          ) : (
            <TextArea
              label="邮箱地址列表 (每行一个)"
              value={addressesText}
              onChange={(e) => setAddressesText(e.target.value)}
              placeholder={"user1@example.com\nuser2@example.com"}
              rows={6}
              required
            />
          )}
        </form>
      )}
    </Dialog>
  );
}

// -------------------------------------------------------------
// Mailbox Detail & User Grant Management Dialog
// -------------------------------------------------------------

/** One block of the mailbox dialog: an icon, a title and an optional action. */
function DialogBlock({ icon, title, aside, children }: { icon: IconName; title: string; aside?: ReactNode; children: ReactNode }) {
  return (
    <section className="py-5 first:pt-0 last:pb-0">
      <div className="mb-3.5 flex items-center gap-2.5">
        <Icon name={icon} className="size-4 text-ink-faint" />
        <h3 className="flex-1 text-sm font-semibold">{title}</h3>
        {aside}
      </div>
      {children}
    </section>
  );
}

function MailboxDetailDialog({
  address,
  users,
  currentAssigned,
  onClose,
  onUpdated,
  onOpenInbox,
}: {
  address: string;
  users: User[];
  currentAssigned: string[];
  onClose: () => void;
  onUpdated: () => void;
  onOpenInbox: (addr: string) => void;
}) {
  const toast = useToast();
  const id = useId();
  const [detail, setDetail] = useState<{ mailbox: MailboxItem; services: ServiceItem[] } | null>(null);
  const [plainPassword, setPlainPassword] = useState<string | null>(null);
  const [note, setNote] = useState("");
  const [isActive, setIsActive] = useState(true);
  const [expiresAtDate, setExpiresAtDate] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [assignedUserIds, setAssignedUserIds] = useState<string[]>(currentAssigned);
  const [templates, setTemplates] = useState<TemplateItem[]>([]);
  const [selectedTemplateId, setSelectedTemplateId] = useState("");
  const [customServiceName, setCustomServiceName] = useState("");
  const [customServiceUrl, setCustomServiceUrl] = useState("");
  const [customServiceNote, setCustomServiceNote] = useState("");
  const [showCustomService, setShowCustomService] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const load = async () => {
    try {
      const [dRes, pwRes, tRes] = await Promise.all([
        api.adminMail<{ mailbox: MailboxItem; services: ServiceItem[] }>("GET", `/mailboxes/${encodeURIComponent(address)}`),
        api.adminMail<{ password: string | null }>("GET", `/mailboxes/${encodeURIComponent(address)}/password`).catch(() => ({ password: null })),
        api.adminMail<{ items: TemplateItem[] }>("GET", "/services").catch(() => ({ items: [] })),
      ]);
      setDetail(dRes);
      setNote(dRes.mailbox.note || "");
      setIsActive(dRes.mailbox.isActive);
      if (dRes.mailbox.expiresAt) {
        const d = new Date(dRes.mailbox.expiresAt * 1000);
        setExpiresAtDate(d.toISOString().slice(0, 10));
      } else {
        setExpiresAtDate("");
      }
      setPlainPassword(pwRes.password);
      setTemplates(tRes.items || []);
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  useEffect(() => {
    void load();
  }, [address]);

  const saveDetailsAndGrants = async () => {
    setBusy(true);
    setError("");
    try {
      // 1. Update mailbox properties in xmail
      let expTimestamp: number | null = null;
      if (expiresAtDate) {
        expTimestamp = Math.floor(new Date(expiresAtDate + "T23:59:59Z").getTime() / 1000);
      }
      await api.adminMail("PATCH", `/mailboxes/${encodeURIComponent(address)}`, {
        note: note.trim() || null,
        isActive,
        expiresAt: expTimestamp,
      });

      // 2. Update grants in XAuth
      await api.setMailGrants(address, assignedUserIds);

      toast("邮箱信息与用户分配已更新");
      onUpdated();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const regeneratePassword = async () => {
    if (!window.confirm("确定重新生成随机密码？")) return;
    try {
      const res = await api.adminMail<{ password: string }>("POST", `/mailboxes/${encodeURIComponent(address)}/password`);
      setPlainPassword(res.password);
      toast("已重新生成随机密码");
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const setManualPassword = async () => {
    if (!newPassword.trim()) return;
    try {
      await api.adminMail("POST", `/mailboxes/${encodeURIComponent(address)}/password`, {
        password: newPassword.trim(),
      });
      setPlainPassword(newPassword.trim());
      setNewPassword("");
      toast("已更新邮箱密码");
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const bindServiceTemplate = async () => {
    if (!selectedTemplateId) return;
    try {
      await api.adminMail("POST", `/mailboxes/${encodeURIComponent(address)}/services`, {
        templateId: selectedTemplateId,
      });
      toast("已绑定服务模板");
      setSelectedTemplateId("");
      void load();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const addCustomService = async () => {
    if (!customServiceName.trim() || !customServiceUrl.trim()) return;
    try {
      await api.adminMail("POST", `/mailboxes/${encodeURIComponent(address)}/services`, {
        name: customServiceName.trim(),
        loginUrl: customServiceUrl.trim(),
        note: customServiceNote.trim() || undefined,
      });
      toast("已添加自定义服务");
      setCustomServiceName("");
      setCustomServiceUrl("");
      setCustomServiceNote("");
      setShowCustomService(false);
      void load();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const removeService = async (serviceId: string) => {
    try {
      await api.adminMail("DELETE", `/mailboxes/${encodeURIComponent(address)}/services?serviceId=${encodeURIComponent(serviceId)}`);
      toast("已移除服务");
      void load();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  return (
    <Dialog
      open={true}
      onClose={onClose}
      wide
      title="管理邮箱"
      description={address}
      footer={
        <>
          <Button variant="secondary" size="sm" className="mr-auto self-center" onClick={() => onOpenInbox(address)}>
            <Icon name="inbox" className="size-3.5" />
            打开收件箱
          </Button>
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button onClick={() => void saveDetailsAndGrants()} busy={busy}>
            保存更改
          </Button>
        </>
      }
    >
      {error && (
        <div className="mb-5">
          <Notice>{error}</Notice>
        </div>
      )}
      <div className="divide-y divide-line">
        {/* Basic Info */}
        <DialogBlock icon="edit" title="邮箱设置">
          <div className="space-y-4">
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="备注" value={note} onChange={(e) => setNote(e.target.value)} placeholder="如 业务邮箱" />
              <div>
                <label htmlFor={`${id}-exp`} className="mb-1.5 block text-[0.8125rem] font-medium text-ink">
                  到期日期
                </label>
                <div className="flex items-center gap-2">
                  <input id={`${id}-exp`} type="date" value={expiresAtDate} onChange={(e) => setExpiresAtDate(e.target.value)} aria-describedby={`${id}-exp-hint`} className="field min-w-0 flex-1" />
                  {expiresAtDate && (
                    <Button variant="ghost" size="sm" className="hover:text-void" onClick={() => setExpiresAtDate("")} title="清除到期时间" aria-label="清除到期时间">
                      清除
                    </Button>
                  )}
                </div>
              </div>
            </div>
            <p id={`${id}-exp-hint`} className="-mt-2 text-[0.8125rem] text-ink-soft sm:ml-[calc(50%+0.5rem)]">
              留空表示永久有效。过期后用户将无法读取邮件。
            </p>
            <div className="rounded-xl border border-line bg-desk/30 px-3.5 py-3">
              <Check label="启用此邮箱" description="禁用后普通用户将无法访问此收件箱。" checked={isActive} onChange={setIsActive} />
            </div>
          </div>
        </DialogBlock>

        <DialogBlock
          icon="lock"
          title="邮箱密码"
          aside={
            <Button variant="ghost" size="sm" className="-my-1 !text-stamp hover:!bg-stamp/[0.07]" onClick={() => void regeneratePassword()}>
              <Icon name="refresh" className="size-3.5" />
              重新生成随机密码
            </Button>
          }
        >
          <div className="space-y-3">
            {plainPassword ? (
              <Copyable value={plainPassword} label="邮箱密码" />
            ) : (
              <Empty icon="lock">当前未设置明文密码。</Empty>
            )}
            <div className="flex gap-2">
              <input
                type="password"
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
                placeholder="设置新密码"
                aria-label="设置新密码"
                autoComplete="new-password"
                className="field min-w-0 flex-1"
              />
              <Button variant="secondary" className="!h-10" size="sm" onClick={() => void setManualPassword()} disabled={!newPassword.trim()}>
                修改密码
              </Button>
            </div>
          </div>
        </DialogBlock>

        {/* User Allocation */}
        <DialogBlock icon="users" title="分配给用户 (XAuth)" aside={<Badge tone={assignedUserIds.length > 0 ? "stamp" : "plain"}>已选择 {assignedUserIds.length} 人</Badge>}>
          <p className="mb-3 text-[0.8125rem] leading-5 text-ink-soft">只有被勾选的用户能在其账户页的“我的邮箱”中查阅并接收此邮箱的验证码与邮件。</p>
          <UserChecklist users={users} selected={assignedUserIds} onChange={setAssignedUserIds} />
        </DialogBlock>

        {/* Linked Services */}
        <DialogBlock
          icon="link"
          title="快捷服务入口"
          aside={
            <Button variant="ghost" size="sm" className="-my-1 !text-stamp hover:!bg-stamp/[0.07]" onClick={() => setShowCustomService(!showCustomService)}>
              {showCustomService ? (
                "收起自定义服务"
              ) : (
                <>
                  <Icon name="plus" className="size-3.5" />
                  自定义服务
                </>
              )}
            </Button>
          }
        >
          <div className="space-y-3">
            {showCustomService ? (
              <div className="space-y-3 rounded-xl border border-line bg-desk/30 p-4">
                <div className="grid gap-3 sm:grid-cols-2">
                  <Field label="服务名称" value={customServiceName} onChange={(e) => setCustomServiceName(e.target.value)} placeholder="如 内部CRM" />
                  <Field label="登录链接" value={customServiceUrl} onChange={(e) => setCustomServiceUrl(e.target.value)} placeholder="https://crm.example.com" />
                </div>
                <Field label="备注" value={customServiceNote} onChange={(e) => setCustomServiceNote(e.target.value)} placeholder="选填" />
                <div className="flex justify-end gap-2 pt-1">
                  <Button variant="ghost" size="sm" onClick={() => setShowCustomService(false)}>
                    取消
                  </Button>
                  <Button size="sm" onClick={() => void addCustomService()} disabled={!customServiceName.trim() || !customServiceUrl.trim()}>
                    添加服务
                  </Button>
                </div>
              </div>
            ) : (
              <div className="flex gap-2">
                <select value={selectedTemplateId} onChange={(e) => setSelectedTemplateId(e.target.value)} aria-label="选择全局服务模板" className="field min-w-0 flex-1 text-sm">
                  <option value="">-- 选择全局服务模板绑定 --</option>
                  {templates.map((t) => (
                    <option key={t.id} value={t.id}>
                      {t.name} ({t.loginUrl})
                    </option>
                  ))}
                </select>
                <Button variant="secondary" size="sm" className="!h-10" onClick={() => void bindServiceTemplate()} disabled={!selectedTemplateId}>
                  绑定
                </Button>
              </div>
            )}

            {detail?.services && detail.services.length > 0 ? (
              <div className="divide-y divide-line/80 rounded-xl border border-line bg-white px-3.5 py-3">
                {detail.services.map((sv) => (
                  <ListRow
                    key={sv.id}
                    icon={sv.isCustom ? "link" : "globe"}
                    title={sv.name}
                    badge={sv.isCustom ? <Badge>自定义</Badge> : <Badge tone="ink">模板</Badge>}
                    meta={
                      <a href={sv.loginUrl} target="_blank" rel="noreferrer" className="font-mono text-[0.71875rem] hover:text-ink hover:underline">
                        {sv.loginUrl}
                      </a>
                    }
                    action={
                      <Button variant="ghost" size="sm" className="hover:text-void" onClick={() => void removeService(sv.id)}>
                        解除绑定
                      </Button>
                    }
                  />
                ))}
              </div>
            ) : (
              <Empty icon="link">暂无挂载的业务服务。</Empty>
            )}
          </div>
        </DialogBlock>
      </div>
    </Dialog>
  );
}

// -------------------------------------------------------------
// Service Templates Tab
// -------------------------------------------------------------

function ServicesTab() {
  const toast = useToast();
  const [templates, setTemplates] = useState<TemplateItem[] | null>(null);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<TemplateItem | null>(null);
  const [error, setError] = useState("");

  const load = async () => {
    try {
      const res = await api.adminMail<{ items: TemplateItem[] }>("GET", "/services");
      setTemplates(res.items || []);
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const deleteTemplate = async (t: TemplateItem) => {
    if (!window.confirm(`确定删除模板“${t.name}”？这会级联解除所有关联邮箱上的该服务。`)) return;
    try {
      await api.adminMail("DELETE", `/services/${encodeURIComponent(t.id)}`);
      toast(`已删除模板 ${t.name}`);
      void load();
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  const addButton = (
    <Button size="sm" onClick={() => setAdding(true)}>
      <Icon name="plus" className="size-3.5" />
      新建模板
    </Button>
  );

  return (
    <div>
      {error && (
        <div className="mb-4">
          <Notice>{error}</Notice>
        </div>
      )}

      {templates === null && !error ? (
        <TableSkeleton rows={3} />
      ) : templates?.length === 0 ? (
        <EmptyState icon="globe" title="暂无服务模板。" description="新建模板后，可在邮箱的“管理 / 分配”中一键绑定。" action={addButton} />
      ) : (
        <>
          <Toolbar actions={addButton}>
            <p className="text-[0.8125rem] text-ink-soft">
              共 <span className="tabular-nums">{templates?.length ?? 0}</span> 个模板
            </p>
          </Toolbar>
          <div className="panel overflow-x-auto">
            <table className="data-table min-w-[640px]">
              <thead>
                <tr>
                  {["服务名称", "登录链接", "备注", ""].map((h, i) => (
                    <th key={i} scope="col">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {templates?.map((t) => (
                  <tr key={t.id}>
                    <td>
                      <div className="flex items-center gap-3">
                        <IconTile name="globe" />
                        <span className="font-medium">{t.name}</span>
                      </div>
                    </td>
                    <td className="max-w-[20rem]">
                      <a href={t.loginUrl} target="_blank" rel="noreferrer" className="group inline-flex max-w-full items-center gap-1.5 text-ink-soft hover:text-ink">
                        <span className="truncate font-mono text-[0.75rem] group-hover:underline">{t.loginUrl}</span>
                        <span aria-hidden="true" className="text-[0.6875rem] text-ink-faint">
                          ↗
                        </span>
                      </a>
                    </td>
                    <td className="text-ink-soft">{t.note || <span className="text-ink-faint">—</span>}</td>
                    <td className="text-right whitespace-nowrap">
                      <div className="flex justify-end gap-0.5">
                        <Button variant="ghost" size="sm" onClick={() => setEditing(t)}>
                          编辑
                        </Button>
                        <Button variant="ghost" size="sm" className="hover:text-void" onClick={() => void deleteTemplate(t)}>
                          删除
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}

      {(adding || editing) && (
        <ServiceTemplateDialog
          item={editing}
          onClose={() => {
            setAdding(false);
            setEditing(null);
          }}
          onSaved={() => {
            setAdding(false);
            setEditing(null);
            void load();
          }}
        />
      )}
    </div>
  );
}

function ServiceTemplateDialog({
  item,
  onClose,
  onSaved,
}: {
  item: TemplateItem | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const toast = useToast();
  const [name, setName] = useState(item?.name || "");
  const [loginUrl, setLoginUrl] = useState(item?.loginUrl || "");
  const [note, setNote] = useState(item?.note || "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      if (item) {
        await api.adminMail("PATCH", `/services/${encodeURIComponent(item.id)}`, {
          name: name.trim(),
          loginUrl: loginUrl.trim(),
          note: note.trim() || null,
        });
        toast("模板已更新");
      } else {
        await api.adminMail("POST", "/services", {
          name: name.trim(),
          loginUrl: loginUrl.trim(),
          note: note.trim() || undefined,
        });
        toast("模板已创建");
      }
      onSaved();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={true}
      onClose={onClose}
      title={item ? "编辑服务模板" : "新建服务模板"}
      description="模板绑定到邮箱后，用户可在收件箱顶部一键打开登录页。"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button type="submit" form="tpl-form" busy={busy}>
            保存
          </Button>
        </>
      }
    >
      <form id="tpl-form" onSubmit={submit} className="space-y-4">
        {error && <Notice>{error}</Notice>}
        <Field label="名称" value={name} onChange={(e) => setName(e.target.value)} placeholder="如 GitHub" required />
        <Field
          label="登录链接"
          value={loginUrl}
          onChange={(e) => setLoginUrl(e.target.value)}
          placeholder="如 https://github.com/login"
          required
        />
        <Field label="备注" value={note} onChange={(e) => setNote(e.target.value)} placeholder="选填" />
      </form>
    </Dialog>
  );
}

// -------------------------------------------------------------
// Trash Tab
// -------------------------------------------------------------

function TrashTab() {
  const toast = useToast();
  const [items, setItems] = useState<MailboxItem[] | null>(null);
  const [error, setError] = useState("");

  const load = async () => {
    try {
      const res = await api.adminMail<{ items: MailboxItem[] }>("GET", "/trash");
      setItems(res.items || []);
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const restore = async (addr: string) => {
    try {
      await api.adminMail("POST", `/trash/${encodeURIComponent(addr)}/restore`);
      toast(`已恢复邮箱：${addr}`);
      void load();
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  const deleteForever = async (addr: string) => {
    if (!window.confirm(`确定永久删除邮箱 ${addr} 及其全部邮件？此操作无法撤销！`)) return;
    try {
      await api.adminMail("DELETE", `/trash/${encodeURIComponent(addr)}`);
      // Also delete any grants in XAuth
      await api.deleteMailGrants(addr).catch(() => {});
      toast(`已永久删除 ${addr}`);
      void load();
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  const emptyTrash = async () => {
    if (!window.confirm("确定清空回收站？所有回收站中的邮箱与邮件将被永久删除！")) return;
    try {
      await api.adminMail("DELETE", "/trash");
      toast("回收站已清空");
      void load();
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  return (
    <div>
      {error && (
        <div className="mb-4">
          <Notice>{error}</Notice>
        </div>
      )}

      {items === null && !error ? (
        <TableSkeleton rows={3} />
      ) : items?.length === 0 ? (
        <EmptyState icon="trash" title="回收站为空。" description="从邮箱列表删除的邮箱会先进入这里，可随时恢复。" />
      ) : (
        <>
          <Toolbar
            actions={
              items &&
              items.length > 0 && (
                <Button variant="danger" size="sm" onClick={() => void emptyTrash()}>
                  <Icon name="trash" className="size-3.5" />
                  清空回收站
                </Button>
              )
            }
          >
            <p className="text-[0.8125rem] text-ink-soft">
              回收站中有 <span className="tabular-nums">{items?.length ?? 0}</span> 个邮箱
            </p>
          </Toolbar>
          <div className="panel overflow-x-auto">
            <table className="data-table min-w-[640px]">
              <thead>
                <tr>
                  {["邮箱地址", "删除时间", "备注", ""].map((h, i) => (
                    <th key={i} scope="col">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {items?.map((item) => (
                  <tr key={item.address}>
                    <td className="font-mono text-[0.78125rem] font-semibold text-ink-soft">{item.address}</td>
                    <td className="whitespace-nowrap text-ink-soft">{item.deletedAt ? formatDate(new Date(item.deletedAt * 1000).toISOString()) : "—"}</td>
                    <td className="text-ink-soft">{item.note || <span className="text-ink-faint">—</span>}</td>
                    <td className="text-right whitespace-nowrap">
                      <div className="flex justify-end gap-0.5">
                        <Button variant="ghost" size="sm" className="!text-stamp hover:!bg-stamp/[0.07]" onClick={() => void restore(item.address)}>
                          <Icon name="history" className="size-3.5" />
                          恢复
                        </Button>
                        <Button variant="ghost" size="sm" className="hover:text-void" onClick={() => void deleteForever(item.address)}>
                          永久删除
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </div>
  );
}

// -------------------------------------------------------------
// Settings Tab
// -------------------------------------------------------------

function StatCard({ icon, label, value, muted }: { icon: IconName; label: string; value: number; muted?: boolean }) {
  return (
    <div className="panel flex items-start justify-between gap-3 p-4 sm:p-5">
      <div className="min-w-0">
        <p className="eyebrow truncate">{label}</p>
        <p className={`mt-2 text-2xl leading-none font-semibold tracking-tight tabular-nums sm:text-[1.75rem] ${muted ? "text-ink-soft" : "text-ink"}`}>{value}</p>
      </div>
      <span className="hidden sm:block">
        <IconTile name={icon} />
      </span>
    </div>
  );
}

function SettingsTab() {
  const toast = useToast();
  const [stats, setStats] = useState<SystemStats | null>(null);
  const [settings, setSettings] = useState<SystemSettings | null>(null);
  const [newDomain, setNewDomain] = useState("");
  const [retentionDays, setRetentionDays] = useState(30);
  const [logs, setLogs] = useState<SystemLog[]>([]);
  const [grants, setGrants] = useState<MailGrant[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const load = async () => {
    try {
      const [stRes, setRes, logRes, gRes] = await Promise.all([
        api.adminMail<SystemStats>("GET", "/system/stats"),
        api.adminMail<SystemSettings>("GET", "/system/settings"),
        api.adminMail<{ items: SystemLog[] }>("GET", "/system/logs?limit=20").catch(() => ({ items: [] })),
        api.mailGrants(true).catch(() => ({ grants: [] })),
      ]);
      setStats(stRes);
      setSettings(setRes);
      setRetentionDays(setRes.retentionDays || 30);
      setLogs(logRes.items || []);
      setGrants(gRes.grants || []);
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const addDomain = async (e: FormEvent) => {
    e.preventDefault();
    if (!newDomain.trim()) return;
    try {
      await api.adminMail("POST", "/system/domains", { domain: newDomain.trim() });
      toast(`已添加域名：${newDomain.trim()}`);
      setNewDomain("");
      void load();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const setDefaultDomain = async (domain: string) => {
    try {
      await api.adminMail("PUT", "/system/domains", { domain });
      toast(`已将 ${domain} 设为默认域名`);
      void load();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const deleteDomain = async (domain: string) => {
    if (!window.confirm(`确定删除域名 ${domain}？`)) return;
    try {
      await api.adminMail("DELETE", `/system/domains?domain=${encodeURIComponent(domain)}`);
      toast(`已删除域名 ${domain}`);
      void load();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const updateRetention = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await api.adminMail("PATCH", "/system/settings", { retentionDays });
      toast("邮件保留策略已保存");
      void load();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const cleanLogs = async (scope: "3d" | "7d" | "all") => {
    if (!window.confirm(`确定清理日志 (${scope})？`)) return;
    try {
      await api.adminMail("DELETE", `/system/logs?scope=${scope}`);
      toast("日志已清理");
      void load();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  // Orphaned grants (in XAuth but missing in xmail)
  const orphaned = grants.filter((g) => g.status === "missing" || g.status === "trashed");

  const cleanupOrphaned = async () => {
    if (!window.confirm(`确定清理 ${orphaned.length} 个无效的分配记录？`)) return;
    try {
      for (const g of orphaned) {
        await api.deleteMailGrants(g.address);
      }
      toast("已清理失效分配");
      void load();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  return (
    <div className="space-y-5">
      {error && <Notice>{error}</Notice>}

      {/* Stats Summary */}
      <div className="grid grid-cols-3 gap-3 sm:gap-5">
        {stats ? (
          <>
            <StatCard icon="inbox" label="活跃邮箱" value={stats.totalMailboxes} />
            <StatCard icon="mail" label="总收件数" value={stats.totalEmails} />
            <StatCard icon="trash" label="回收站中邮箱" value={stats.deletedMailboxes} muted />
          </>
        ) : (
          [0, 1, 2].map((i) => (
            <div key={i} aria-hidden="true" className="panel p-4 sm:p-5">
              <span className="block h-3 w-16 animate-pulse rounded bg-ink/[0.06] motion-reduce:animate-none" />
              <span className="mt-3 block h-6 w-10 animate-pulse rounded bg-ink/[0.06] motion-reduce:animate-none" />
            </div>
          ))
        )}
      </div>

      {/* Orphaned Grants Cleanup */}
      {orphaned.length > 0 && (
        <div className="flex flex-wrap items-start gap-x-4 gap-y-3 rounded-[14px] border border-void/25 bg-void/[0.04] p-4 sm:p-5">
          <span aria-hidden="true" className="grid size-9 shrink-0 place-items-center rounded-[10px] border border-void/20 bg-white text-void shadow-xs">
            <Icon name="alert" className="size-[1.0625rem]" />
          </span>
          <div className="min-w-0 flex-1 basis-64">
            <h4 className="text-sm font-semibold text-ink">发现 {orphaned.length} 个失效的邮箱分配</h4>
            <p className="mt-1 text-[0.8125rem] leading-5 text-ink-soft">这些邮箱已在 xmail 中被删除或移入回收站，但 XAuth 中仍保留着分配记录：</p>
            <div className="mt-2 flex flex-wrap gap-1.5">
              {orphaned.map((g) => (
                <span key={g.address} className="rounded-md border border-line bg-white px-1.5 py-0.5 font-mono text-[0.71875rem] text-ink">
                  {g.address}
                </span>
              ))}
            </div>
          </div>
          <Button variant="danger" size="sm" onClick={() => void cleanupOrphaned()}>
            一键清理失效记录
          </Button>
        </div>
      )}

      <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)]">
        {/* Domain Management */}
        <Section icon="globe" title="收件域名设置" description="添加域名后，还需要在 Cloudflare Email Routing 中将该域名规则路由到 xmail 的 Email Worker 才能正常接收邮件。">
          <form onSubmit={addDomain} className="flex gap-2">
            <input
              value={newDomain}
              onChange={(e) => setNewDomain(e.target.value)}
              placeholder="例如 mail.example.com"
              aria-label="新域名"
              className="field min-w-0 flex-1 font-mono text-[0.8125rem]"
              required
            />
            <Button type="submit" variant="secondary" size="sm" className="!h-10">
              <Icon name="plus" className="size-3.5" />
              添加域名
            </Button>
          </form>

          <div className="mt-4 divide-y divide-line/80 overflow-hidden rounded-xl border border-line bg-white">
            {settings?.domains?.map((d) => {
              const isDefault = d === settings.defaultDomain;
              return (
                <div key={d} className="flex items-center gap-3 px-3.5 py-2.5">
                  <Icon name="globe" className="size-4 text-ink-faint" />
                  <span className="min-w-0 flex-1 truncate font-mono text-[0.78125rem] font-medium text-ink">{d}</span>
                  {isDefault && (
                    <Badge tone="ink" dot>
                      默认
                    </Badge>
                  )}
                  <div className="flex shrink-0 items-center gap-0.5">
                    {!isDefault && (
                      <Button variant="ghost" size="sm" onClick={() => void setDefaultDomain(d)}>
                        设为默认
                      </Button>
                    )}
                    {settings.domains.length > 1 && (
                      <Button variant="ghost" size="sm" className="hover:text-void" onClick={() => void deleteDomain(d)}>
                        删除
                      </Button>
                    )}
                  </div>
                </div>
              );
            })}
            {settings && settings.domains.length === 0 && <p className="px-3.5 py-4 text-center text-[0.8125rem] text-ink-soft">还没有收件域名。</p>}
          </div>
        </Section>

        {/* Retention Settings */}
        <Section
          icon="clock"
          title="邮件自动保留天数"
          description="超过该天数的非星标邮件将由 xmail 定期清理；星标邮件将永久保留。"
          onSubmit={updateRetention}
          footer={
            <Button type="submit" size="sm" busy={busy}>
              保存策略
            </Button>
          }
        >
          <Field
            label="保留天数"
            type="number"
            min={1}
            max={3650}
            value={retentionDays}
            onChange={(e) => setRetentionDays(Number(e.target.value))}
            hint="可设置 1 ~ 3650 天。"
            className="max-w-[12rem]"
          />
        </Section>
      </div>

      {/* Operation Logs */}
      <Section
        icon="history"
        title="xmail 审计日志"
        description="最近 20 条 xmail 侧的管理操作。"
        action={
          <div className="flex items-center gap-0.5">
            <Button variant="ghost" size="sm" onClick={() => void cleanLogs("3d")}>
              清理 3 天前
            </Button>
            <Button variant="ghost" size="sm" className="hover:text-void" onClick={() => void cleanLogs("all")}>
              清空全部
            </Button>
          </div>
        }
      >
        {logs.length === 0 ? (
          <Empty icon="history">暂无操作日志。</Empty>
        ) : (
          <div className="max-h-80 divide-y divide-line/70 overflow-y-auto rounded-xl border border-line bg-white">
            {logs.map((log) => (
              <div key={log.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3.5 py-2.5">
                <code className="rounded-md border border-line bg-desk/50 px-1.5 py-0.5 font-mono text-[0.71875rem] font-medium text-ink">{log.action}</code>
                {log.target && <span className="min-w-0 flex-1 truncate font-mono text-[0.71875rem] text-ink-soft">{log.target}</span>}
                <span className="ml-auto shrink-0 text-xs text-ink-faint tabular-nums">{formatDate(new Date(log.createdAt * 1000).toISOString())}</span>
              </div>
            ))}
          </div>
        )}
      </Section>
    </div>
  );
}
