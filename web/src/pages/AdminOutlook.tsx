import { useEffect, useMemo, useState, type FormEvent } from "react";
import { api, errorMessage, type OutlookAccount, type OutlookGroup, type User } from "../api";
import { Dialog, useToast } from "../components/Dialog";
import { Icon } from "../components/Icon";
import {
  Avatar,
  Badge,
  Button,
  Check,
  EmptyState,
  Field,
  GroupLabel,
  Notice,
  RadioCard,
  SearchField,
  Segmented,
  TextArea,
  UserChecklist,
} from "../components/ui";
import { formatDate } from "../session";

const GROUP_COLORS = ["#5a3fc0", "#23704d", "#1b2340", "#0284c7", "#c98a12", "#be123c", "#666666"];

function downloadTextFile(filename: string, content: string) {
  const blob = new Blob([content], { type: "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

export function OutlookTab({ onOpenInbox }: { onOpenInbox: (addr: string) => void }) {
  const toast = useToast();
  const [groups, setGroups] = useState<OutlookGroup[]>([]);
  const [accounts, setAccounts] = useState<OutlookAccount[] | null>(null);
  const [users, setUsers] = useState<User[]>([]);
  const [activeGroupId, setActiveGroupId] = useState<number>(0);
  const [search, setSearch] = useState("");
  const [filterStatus, setFilterStatus] = useState<"all" | "active" | "disabled" | "assigned" | "unassigned">("all");
  const [selected, setSelected] = useState<string[]>([]);
  const [error, setError] = useState("");

  // Dialog states
  const [importing, setImporting] = useState(false);
  const [editingAccount, setEditingAccount] = useState<OutlookAccount | null>(null);
  const [creatingGroup, setCreatingGroup] = useState(false);
  const [editingGroup, setEditingGroup] = useState<OutlookGroup | null>(null);
  const [batchAssigning, setBatchAssigning] = useState(false);
  const [batchMovingGroup, setBatchMovingGroup] = useState(false);

  const loadData = async () => {
    setError("");
    try {
      const [grpRes, accRes, usrRes] = await Promise.all([
        api.outlookGroups(),
        api.outlookAccounts(0),
        api.users(),
      ]);
      setGroups(grpRes.groups || []);
      setAccounts(accRes.accounts || []);
      setUsers(usrRes.users || []);
      setSelected([]);
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  useEffect(() => {
    void loadData();
  }, []);

  const userMap = useMemo(() => new Map(users.map((u) => [u.id, u])), [users]);
  const activeGroup = useMemo(() => groups.find((g) => g.id === activeGroupId) || null, [groups, activeGroupId]);

  const filtered = useMemo(() => {
    if (!accounts) return null;
    const kw = search.trim().toLowerCase();
    return accounts.filter((a) => {
      if (activeGroupId > 0 && a.groupId !== activeGroupId) return false;
      if (kw) {
        const matchEmail = a.email.toLowerCase().includes(kw);
        const matchRemark = (a.remark || "").toLowerCase().includes(kw);
        const matchGroup = (a.groupName || "").toLowerCase().includes(kw);
        if (!matchEmail && !matchRemark && !matchGroup) return false;
      }
      if (filterStatus === "active") return a.status === "active";
      if (filterStatus === "disabled") return a.status === "disabled";
      if (filterStatus === "assigned") return (a.userIds?.length ?? 0) > 0;
      if (filterStatus === "unassigned") return (a.userIds?.length ?? 0) === 0;
      return true;
    });
  }, [accounts, activeGroupId, search, filterStatus]);

  const allChecked = !!filtered && filtered.length > 0 && selected.length === filtered.length;

  const toggleSelectAll = () => {
    if (!filtered) return;
    if (allChecked) setSelected([]);
    else setSelected(filtered.map((a) => a.id));
  };

  const toggleSelectOne = (id: string) => {
    setSelected((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]));
  };

  const handleDeleteOne = async (acc: OutlookAccount) => {
    if (!window.confirm(`确定删除 Outlook 邮箱 ${acc.email}？已分配的用户将立即失去访问权限。`)) return;
    try {
      await api.deleteOutlookAccount(acc.id);
      toast(`已删除 ${acc.email}`);
      void loadData();
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  const handleBatchDelete = async () => {
    if (!window.confirm(`确定删除选中的 ${selected.length} 个 Outlook 邮箱？`)) return;
    try {
      const res = await api.batchDeleteOutlookAccounts(selected);
      toast(`已批量删除 ${res.deleted} 个邮箱`);
      void loadData();
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  const handleExport = async (opts: { groupIds?: number[]; ids?: string[] }) => {
    try {
      const res = await api.exportOutlookAccounts(opts);
      if (!res.count || !res.content) {
        toast("没有可导出的 Outlook 邮箱账号");
        return;
      }
      downloadTextFile(res.filename || "outlook_accounts.txt", res.content);
      toast(`已导出 ${res.count} 个 Outlook 邮箱`);
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  const handleDeleteGroup = async (g: OutlookGroup) => {
    if (g.id === 1) return;
    if (!window.confirm(`确定删除分组「${g.name}」？组内邮箱会自动移回默认分组。`)) return;
    try {
      await api.deleteOutlookGroup(g.id);
      toast(`已删除分组「${g.name}」`);
      setActiveGroupId(0);
      void loadData();
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  return (
    <div className="space-y-4">
      {error && <Notice>{error}</Notice>}

      {/* Group Selector Bar */}
      <div className="panel flex flex-wrap items-center justify-between gap-2 px-4 py-3">
        <div className="flex flex-wrap items-center gap-1.5">
          <button
            type="button"
            onClick={() => setActiveGroupId(0)}
            className={`inline-flex h-8 items-center gap-2 rounded-lg border px-3 text-xs font-medium transition-colors ${
              activeGroupId === 0
                ? "border-ink bg-ink text-paper shadow-xs"
                : "border-line bg-white text-ink-soft hover:border-line-strong hover:text-ink"
            }`}
          >
            <span>全部邮箱</span>
            <span className={`rounded-full px-1.5 py-0.5 text-[0.6875rem] tabular-nums ${activeGroupId === 0 ? "bg-white/20 text-white" : "bg-ink/[0.06] text-ink-soft"}`}>
              {accounts?.length ?? 0}
            </span>
          </button>
          {groups.map((g) => {
            const active = activeGroupId === g.id;
            return (
              <button
                key={g.id}
                type="button"
                onClick={() => setActiveGroupId(g.id)}
                title={g.description || g.name}
                className={`inline-flex h-8 items-center gap-2 rounded-lg border px-3 text-xs font-medium transition-colors ${
                  active
                    ? "border-stamp bg-stamp/[0.08] text-stamp shadow-xs"
                    : "border-line bg-white text-ink-soft hover:border-line-strong hover:text-ink"
                }`}
              >
                <span aria-hidden="true" className="size-2 rounded-full" style={{ backgroundColor: g.color || "#666666" }} />
                <span>{g.name}</span>
                <span className="rounded-full bg-ink/[0.06] px-1.5 py-0.5 text-[0.6875rem] text-ink-soft tabular-nums">
                  {g.accountCount}
                </span>
              </button>
            );
          })}
        </div>

        <div className="flex flex-wrap items-center gap-1.5">
          {activeGroup && (
            <>
              <Button variant="ghost" size="sm" onClick={() => setEditingGroup(activeGroup)}>
                编辑分组
              </Button>
              {activeGroup.id !== 1 && (
                <Button variant="ghost" size="sm" className="hover:text-void" onClick={() => void handleDeleteGroup(activeGroup)}>
                  删除分组
                </Button>
              )}
            </>
          )}
          <Button variant="secondary" size="sm" onClick={() => setCreatingGroup(true)}>
            <Icon name="plus" className="size-3.5" />
            新建分组
          </Button>
        </div>
      </div>

      {/* Filter & Actions Toolbar */}
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-3">
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
          <SearchField
            value={search}
            onChange={setSearch}
            placeholder="搜索 Outlook 邮箱地址、备注..."
            label="搜索 Outlook 邮箱"
            className="w-full sm:w-64"
          />
          <Segmented
            label="筛选状态"
            options={[
              ["all", "全部"],
              ["active", "正常"],
              ["disabled", "已停用"],
              ["assigned", "已分配"],
              ["unassigned", "未分配"],
            ]}
            value={filterStatus}
            onChange={setFilterStatus}
          />
        </div>
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void handleExport(activeGroupId > 0 ? { groupIds: [activeGroupId] } : {})}
            disabled={!accounts || accounts.length === 0}
          >
            <Icon name="download" className="size-3.5" />
            {activeGroupId > 0 ? "导出当前分组" : "导出全部 TXT"}
          </Button>
          <Button size="sm" onClick={() => setImporting(true)}>
            <Icon name="plus" className="size-3.5" />
            批量导入 Outlook 邮箱
          </Button>
        </div>
      </div>

      {/* Batch Selection Bar */}
      {selected.length > 0 && (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-stamp/30 bg-stamp/[0.05] px-4 py-2.5">
          <span className="text-[0.8125rem] font-medium text-ink">
            已选中 <strong className="font-semibold text-stamp tabular-nums">{selected.length}</strong> 个 Outlook 邮箱
          </span>
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="secondary" size="sm" onClick={() => setBatchAssigning(true)}>
              <Icon name="users" className="size-3.5" />
              批量分配给用户
            </Button>
            <Button variant="secondary" size="sm" onClick={() => setBatchMovingGroup(true)}>
              移至分组
            </Button>
            <Button variant="secondary" size="sm" onClick={() => void handleExport({ ids: selected })}>
              <Icon name="download" className="size-3.5" />
              导出选中
            </Button>
            <Button variant="danger" size="sm" onClick={() => void handleBatchDelete()}>
              <Icon name="trash" className="size-3.5" />
              批量删除
            </Button>
          </div>
        </div>
      )}

      {/* Accounts Table */}
      {accounts === null ? (
        <div className="panel divide-y divide-line/70" aria-busy="true">
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className="flex items-center gap-4 px-5 py-4">
              <span className="h-3.5 w-1/4 animate-pulse rounded bg-ink/[0.06]" />
              <span className="h-3 w-1/6 animate-pulse rounded bg-ink/[0.05]" />
              <span className="ml-auto h-3 w-16 animate-pulse rounded bg-ink/[0.05]" />
            </div>
          ))}
        </div>
      ) : filtered && filtered.length === 0 ? (
        <EmptyState
          icon="mail"
          title={accounts.length === 0 ? "还没有导入任何 Outlook 邮箱" : "没有匹配的 Outlook 邮箱"}
          description={
            accounts.length === 0
              ? "支持按 邮箱----密码----client_id----refresh_token 格式批量导入 Outlook 邮箱，并分配给指定用户只读查看邮件与验证码。"
              : "请尝试调整分组、筛选条件或搜索关键字。"
          }
          action={
            accounts.length === 0 ? (
              <Button onClick={() => setImporting(true)}>
                <Icon name="plus" className="size-4" />
                批量导入 Outlook 邮箱
              </Button>
            ) : undefined
          }
        />
      ) : (
        <div className="panel overflow-x-auto">
          <table className="data-table min-w-[780px]">
            <thead>
              <tr>
                <th className="w-10">
                  <input
                    type="checkbox"
                    aria-label="全选当前列表"
                    className="size-4 accent-ink"
                    checked={allChecked}
                    onChange={toggleSelectAll}
                  />
                </th>
                <th>Outlook 邮箱</th>
                <th>分组</th>
                <th>状态</th>
                <th>已分配用户</th>
                <th>添加时间</th>
                <th className="text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {filtered?.map((acc) => {
                const isSelected = selected.includes(acc.id);
                const assignedUsers = (acc.userIds || []).map((id) => userMap.get(id)).filter((u): u is User => !!u);
                return (
                  <tr key={acc.id} className={isSelected ? "bg-stamp/[0.04]" : undefined}>
                    <td>
                      <input
                        type="checkbox"
                        aria-label={`选择 ${acc.email}`}
                        className="size-4 accent-ink"
                        checked={isSelected}
                        onChange={() => toggleSelectOne(acc.id)}
                      />
                    </td>
                    <td>
                      <div className="min-w-0">
                        <button
                          type="button"
                          onClick={() => onOpenInbox(acc.email)}
                          className="font-mono text-[0.8125rem] font-semibold text-ink hover:text-stamp hover:underline"
                        >
                          {acc.email}
                        </button>
                        {acc.remark && <p className="mt-0.5 truncate text-xs text-ink-soft max-w-xs">{acc.remark}</p>}
                      </div>
                    </td>
                    <td>
                      <span className="inline-flex items-center gap-1.5 rounded-full border border-line bg-white px-2.5 py-0.5 text-xs font-medium text-ink-soft">
                        <span aria-hidden="true" className="size-2 rounded-full" style={{ backgroundColor: acc.groupColor || "#666666" }} />
                        {acc.groupName || "默认分组"}
                      </span>
                    </td>
                    <td>
                      {acc.status === "active" ? (
                        <Badge tone="seal" dot>
                          正常
                        </Badge>
                      ) : (
                        <Badge tone="void" dot>
                          已停用
                        </Badge>
                      )}
                    </td>
                    <td>
                      {assignedUsers.length > 0 ? (
                        <button
                          type="button"
                          onClick={() => setEditingAccount(acc)}
                          className="group inline-flex items-center gap-2 rounded-lg px-1.5 py-1 text-left transition-colors hover:bg-ink/[0.04]"
                          title={assignedUsers.map((u) => u.name || u.email).join("、")}
                        >
                          <span className="flex -space-x-1.5">
                            {assignedUsers.slice(0, 3).map((u) => (
                              <span key={u.id} className="rounded-full ring-2 ring-white">
                                <Avatar text={u.name || u.email} src={u.avatarUrl} size="sm" tone="plain" />
                              </span>
                            ))}
                          </span>
                          <span className="max-w-[10rem] truncate text-xs font-medium text-ink group-hover:text-stamp">
                            {assignedUsers.map((u) => u.name || u.email).join("、")}
                          </span>
                        </button>
                      ) : (
                        <button
                          type="button"
                          onClick={() => setEditingAccount(acc)}
                          className="text-xs text-ink-faint hover:text-stamp hover:underline"
                        >
                          未分配 · 点击分配
                        </button>
                      )}
                    </td>
                    <td className="whitespace-nowrap text-xs text-ink-soft tabular-nums">{formatDate(acc.createdAt)}</td>
                    <td>
                      <div className="flex items-center justify-end gap-1">
                        <Button variant="ghost" size="sm" onClick={() => onOpenInbox(acc.email)}>
                          <Icon name="inbox" className="size-3.5" />
                          收件箱
                        </Button>
                        <Button variant="ghost" size="sm" onClick={() => setEditingAccount(acc)}>
                          分配 / 编辑
                        </Button>
                        <button
                          type="button"
                          onClick={() => void handleDeleteOne(acc)}
                          className="grid size-8 place-items-center rounded-lg text-ink-faint transition-colors hover:bg-void/[0.06] hover:text-void"
                          title="删除此邮箱"
                          aria-label={`删除 ${acc.email}`}
                        >
                          <Icon name="trash" className="size-3.5" />
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {/* Batch Import Dialog */}
      <Dialog
        open={importing}
        onClose={() => setImporting(false)}
        wide
        title="批量导入 Outlook 邮箱"
        description="按 邮箱----密码----client_id----refresh_token 格式每行粘贴一个账号，可同时选择分组并分配给指定用户。"
      >
        {importing && (
          <ImportOutlookForm
            groups={groups}
            users={users}
            defaultGroupId={activeGroupId > 0 ? activeGroupId : 1}
            onCancel={() => setImporting(false)}
            onSuccess={(msg) => {
              toast(msg);
              setImporting(false);
              void loadData();
            }}
          />
        )}
      </Dialog>

      {/* Edit & Assign Single Account Dialog */}
      <Dialog
        open={editingAccount !== null}
        onClose={() => setEditingAccount(null)}
        wide
        title={editingAccount ? `配置与分配 · ${editingAccount.email}` : "编辑邮箱"}
        description="管理员可修改分组、备注、OAuth2 凭据及允许访问该邮箱的用户。用户无法看到密码或令牌。"
      >
        {editingAccount && (
          <EditOutlookAccountForm
            account={editingAccount}
            groups={groups}
            users={users}
            onCancel={() => setEditingAccount(null)}
            onSuccess={() => {
              toast("已保存 Outlook 邮箱设置");
              setEditingAccount(null);
              void loadData();
            }}
          />
        )}
      </Dialog>

      {/* Create / Edit Group Dialog */}
      <Dialog
        open={creatingGroup || editingGroup !== null}
        onClose={() => {
          setCreatingGroup(false);
          setEditingGroup(null);
        }}
        title={editingGroup ? "编辑分组" : "新建分组"}
        description="使用分组整理不同用途或批次的 Outlook 邮箱账号。"
      >
        {(creatingGroup || editingGroup) && (
          <GroupForm
            group={editingGroup ?? undefined}
            onCancel={() => {
              setCreatingGroup(false);
              setEditingGroup(null);
            }}
            onSuccess={(msg) => {
              toast(msg);
              setCreatingGroup(false);
              setEditingGroup(null);
              void loadData();
            }}
          />
        )}
      </Dialog>

      {/* Batch Assign Dialog */}
      <Dialog
        open={batchAssigning}
        onClose={() => setBatchAssigning(false)}
        wide
        title={`批量分配 ${selected.length} 个 Outlook 邮箱`}
        description="被分配的用户可以在「我的邮箱」中只读查看这些邮箱的收件箱与验证码。"
      >
        {batchAssigning && (
          <BatchAssignForm
            accountIds={selected}
            users={users}
            onCancel={() => setBatchAssigning(false)}
            onSuccess={(n) => {
              toast(`已更新 ${n} 个邮箱的用户分配`);
              setBatchAssigning(false);
              void loadData();
            }}
          />
        )}
      </Dialog>

      {/* Batch Move Group Dialog */}
      <Dialog
        open={batchMovingGroup}
        onClose={() => setBatchMovingGroup(false)}
        title={`移动 ${selected.length} 个邮箱到分组`}
      >
        {batchMovingGroup && (
          <BatchMoveGroupForm
            accountIds={selected}
            groups={groups}
            onCancel={() => setBatchMovingGroup(false)}
            onSuccess={(n) => {
              toast(`已移动 ${n} 个邮箱`);
              setBatchMovingGroup(false);
              void loadData();
            }}
          />
        )}
      </Dialog>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Dialog Forms
// ---------------------------------------------------------------------------

function ImportOutlookForm({
  groups,
  users,
  defaultGroupId,
  onCancel,
  onSuccess,
}: {
  groups: OutlookGroup[];
  users: User[];
  defaultGroupId: number;
  onCancel: () => void;
  onSuccess: (msg: string) => void;
}) {
  const [text, setText] = useState("");
  const [groupId, setGroupId] = useState<number>(defaultGroupId || 1);
  const [remark, setRemark] = useState("");
  const [overwrite, setOverwrite] = useState(true);
  const [userIds, setUserIds] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // Live preview of valid/invalid line counts
  const previewStats = useMemo(() => {
    const lines = text.split(/\r?\n/).map((l) => l.trim()).filter((l) => l !== "" && !l.startsWith("#"));
    let valid = 0;
    let invalid = 0;
    for (const line of lines) {
      const parts = line.split("----");
      if (parts.length >= 3 && parts[0].includes("@") && parts[parts.length - 1].trim() !== "") {
        valid++;
      } else {
        invalid++;
      }
    }
    return { total: lines.length, valid, invalid };
  }, [text]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await api.importOutlookAccounts({
        accountsText: text,
        groupId,
        remark: remark.trim(),
        userIds,
        overwrite,
      });
      const parts = [`新增 ${res.added} 个`];
      if (res.updated > 0) parts.push(`更新 ${res.updated} 个`);
      if (res.skipped > 0) parts.push(`跳过 ${res.skipped} 个`);
      onSuccess(`导入完成：${parts.join("，")}`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4">
      {error && <Notice>{error}</Notice>}

      <TextArea
        label="格式化账号列表（每行一个）"
        rows={6}
        required
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder={"example1@outlook.com----密码----dbc8e03a-b00c-46bd-ae65-b683e7707cb0----M.C559_BAY.0.U.-...\nexample2@hotmail.com----密码----dbc8e03a-b00c-46bd-ae65-b683e7707cb0----M.C559_BAY.0.U.-..."}
        hint={
          previewStats.total > 0 ? (
            <span>
              已识别 <strong className="font-semibold text-seal">{previewStats.valid}</strong> 行有效格式
              {previewStats.invalid > 0 && (
                <span className="ml-2 text-void">（{previewStats.invalid} 行格式不完整将被忽略）</span>
              )}
            </span>
          ) : (
            "标准格式：邮箱----密码----client_id----refresh_token（若无密码可留空：邮箱--------client_id----refresh_token）"
          )
        }
      />

      <div className="grid gap-4 sm:grid-cols-2">
        <div>
          <label className="mb-1.5 block text-[0.8125rem] font-medium text-ink">所属分组</label>
          <select
            value={groupId}
            onChange={(e) => setGroupId(Number(e.target.value))}
            className="field"
          >
            {groups.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </select>
        </div>
        <Field
          label="批次备注（可选）"
          value={remark}
          onChange={(e) => setRemark(e.target.value)}
          placeholder="例如：OpenAI 专用号池"
          maxLength={200}
        />
      </div>

      <Check
        label="覆盖已存在的同名邮箱凭据"
        description="开启后，如果导入的邮箱已存在，将自动更新其密码、client_id 和 refresh_token。"
        checked={overwrite}
        onChange={setOverwrite}
      />

      <div>
        <GroupLabel as="p">立即分配给用户（可选，被分配用户可只读收信）</GroupLabel>
        <UserChecklist users={users} selected={userIds} onChange={setUserIds} />
      </div>

      <div className="flex justify-end gap-2.5 border-t border-line pt-4">
        <Button variant="secondary" onClick={onCancel}>
          取消
        </Button>
        <Button type="submit" busy={busy} disabled={previewStats.valid === 0}>
          导入 {previewStats.valid > 0 ? `${previewStats.valid} 个邮箱` : ""}
        </Button>
      </div>
    </form>
  );
}

function EditOutlookAccountForm({
  account,
  groups,
  users,
  onCancel,
  onSuccess,
}: {
  account: OutlookAccount;
  groups: OutlookGroup[];
  users: User[];
  onCancel: () => void;
  onSuccess: () => void;
}) {
  const [email, setEmail] = useState(account.email);
  const [password, setPassword] = useState("");
  const [clientId, setClientId] = useState(account.clientId);
  const [refreshToken, setRefreshToken] = useState("");
  const [groupId, setGroupId] = useState<number>(account.groupId || 1);
  const [remark, setRemark] = useState(account.remark || "");
  const [status, setStatus] = useState<"active" | "disabled">(account.status);
  const [userIds, setUserIds] = useState<string[]>(account.userIds || []);
  const [showCreds, setShowCreds] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    api
      .outlookAccount(account.id)
      .then((r) => {
        setPassword(r.account.password || "");
        setClientId(r.account.clientId || "");
        setRefreshToken(r.account.refreshToken || "");
      })
      .catch(() => {});
  }, [account.id]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.updateOutlookAccount(account.id, {
        email: email.trim(),
        password,
        clientId: clientId.trim(),
        refreshToken: refreshToken.trim(),
        groupId,
        remark: remark.trim(),
        status,
        userIds,
      });
      onSuccess();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4">
      {error && <Notice>{error}</Notice>}

      <div>
        <GroupLabel as="p">分配给哪些 XAuth 用户（普通用户只读，不能自行添加或删除）</GroupLabel>
        <UserChecklist users={users} selected={userIds} onChange={setUserIds} />
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <div>
          <label className="mb-1.5 block text-[0.8125rem] font-medium text-ink">所属分组</label>
          <select value={groupId} onChange={(e) => setGroupId(Number(e.target.value))} className="field">
            {groups.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label className="mb-1.5 block text-[0.8125rem] font-medium text-ink">状态</label>
          <select value={status} onChange={(e) => setStatus(e.target.value as "active" | "disabled")} className="field">
            <option value="active">正常启用</option>
            <option value="disabled">停用（禁止已分配用户访问）</option>
          </select>
        </div>
      </div>

      <Field
        label="备注说明"
        value={remark}
        onChange={(e) => setRemark(e.target.value)}
        placeholder="选填备注，已分配用户在邮箱卡片上可见"
        maxLength={200}
      />

      <div className="rounded-xl border border-line bg-desk/30 p-3.5">
        <div className="flex items-center justify-between">
          <span className="text-xs font-medium text-ink-soft">OAuth2 凭据与邮箱密码（AES-256-GCM 加密存储）</span>
          <button
            type="button"
            onClick={() => setShowCreds((v) => !v)}
            className="text-xs font-medium text-stamp hover:underline"
          >
            {showCreds ? "收起凭据" : "展开查看 / 修改凭据"}
          </button>
        </div>
        {showCreds && (
          <div className="mt-3.5 space-y-3 border-t border-line pt-3.5">
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="邮箱地址" value={email} onChange={(e) => setEmail(e.target.value)} required />
              <Field label="邮箱密码（选填）" value={password} onChange={(e) => setPassword(e.target.value)} />
            </div>
            <Field label="OAuth2 Client ID" value={clientId} onChange={(e) => setClientId(e.target.value)} required />
            <TextArea
              label="OAuth2 Refresh Token"
              rows={3}
              value={refreshToken}
              onChange={(e) => setRefreshToken(e.target.value)}
              required
            />
          </div>
        )}
      </div>

      <div className="flex justify-end gap-2.5 border-t border-line pt-4">
        <Button variant="secondary" onClick={onCancel}>
          取消
        </Button>
        <Button type="submit" busy={busy}>
          保存更改
        </Button>
      </div>
    </form>
  );
}

function GroupForm({
  group,
  onCancel,
  onSuccess,
}: {
  group?: OutlookGroup;
  onCancel: () => void;
  onSuccess: (msg: string) => void;
}) {
  const [name, setName] = useState(group?.name || "");
  const [description, setDescription] = useState(group?.description || "");
  const [color, setColor] = useState(group?.color || GROUP_COLORS[0]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      if (group) {
        await api.updateOutlookGroup(group.id, { name: name.trim(), description: description.trim(), color });
        onSuccess("分组已更新");
      } else {
        await api.createOutlookGroup({ name: name.trim(), description: description.trim(), color });
        onSuccess("分组已创建");
      }
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4">
      {error && <Notice>{error}</Notice>}
      <Field label="分组名称" value={name} onChange={(e) => setName(e.target.value)} required maxLength={64} />
      <Field label="分组描述（选填）" value={description} onChange={(e) => setDescription(e.target.value)} maxLength={200} />
      <div>
        <GroupLabel as="p">标识颜色</GroupLabel>
        <div className="flex flex-wrap items-center gap-2">
          {GROUP_COLORS.map((c) => (
            <button
              key={c}
              type="button"
              onClick={() => setColor(c)}
              aria-label={`颜色 ${c}`}
              className={`size-7 rounded-full border-2 transition-transform ${color === c ? "scale-110 border-ink shadow-sm" : "border-transparent"}`}
              style={{ backgroundColor: c }}
            />
          ))}
        </div>
      </div>
      <div className="flex justify-end gap-2.5 border-t border-line pt-4">
        <Button variant="secondary" onClick={onCancel}>
          取消
        </Button>
        <Button type="submit" busy={busy}>
          {group ? "保存" : "创建分组"}
        </Button>
      </div>
    </form>
  );
}

function BatchAssignForm({
  accountIds,
  users,
  onCancel,
  onSuccess,
}: {
  accountIds: string[];
  users: User[];
  onCancel: () => void;
  onSuccess: (updated: number) => void;
}) {
  const [userIds, setUserIds] = useState<string[]>([]);
  const [mode, setMode] = useState<"append" | "replace">("append");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await api.batchSetOutlookGrants(accountIds, userIds, mode);
      onSuccess(res.updated);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4">
      {error && <Notice>{error}</Notice>}
      <div className="grid gap-3 sm:grid-cols-2">
        <RadioCard
          name="assign-mode"
          checked={mode === "append"}
          onChange={() => setMode("append")}
          title="追加分配"
          description="保留邮箱原有的已分配用户，并新增勾选的用户。"
        />
        <RadioCard
          name="assign-mode"
          checked={mode === "replace"}
          onChange={() => setMode("replace")}
          title="覆盖分配"
          description="将选中邮箱的访问名单替换为下方勾选的用户（不选则清空分配）。"
        />
      </div>
      <div>
        <GroupLabel as="p">选择目标用户</GroupLabel>
        <UserChecklist users={users} selected={userIds} onChange={setUserIds} />
      </div>
      <div className="flex justify-end gap-2.5 border-t border-line pt-4">
        <Button variant="secondary" onClick={onCancel}>
          取消
        </Button>
        <Button type="submit" busy={busy}>
          确认分配
        </Button>
      </div>
    </form>
  );
}

function BatchMoveGroupForm({
  accountIds,
  groups,
  onCancel,
  onSuccess,
}: {
  accountIds: string[];
  groups: OutlookGroup[];
  onCancel: () => void;
  onSuccess: (moved: number) => void;
}) {
  const [groupId, setGroupId] = useState<number>(groups[0]?.id || 1);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await api.batchMoveOutlookGroup(accountIds, groupId);
      onSuccess(res.moved);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4">
      {error && <Notice>{error}</Notice>}
      <div>
        <label className="mb-1.5 block text-[0.8125rem] font-medium text-ink">目标分组</label>
        <select value={groupId} onChange={(e) => setGroupId(Number(e.target.value))} className="field">
          {groups.map((g) => (
            <option key={g.id} value={g.id}>
              {g.name}
            </option>
          ))}
        </select>
      </div>
      <div className="flex justify-end gap-2.5 border-t border-line pt-4">
        <Button variant="secondary" onClick={onCancel}>
          取消
        </Button>
        <Button type="submit" busy={busy}>
          确认移动
        </Button>
      </div>
    </form>
  );
}
