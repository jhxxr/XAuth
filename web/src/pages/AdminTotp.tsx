import { useEffect, useState } from "react";
import { api, errorMessage, type TotpEntry, type User } from "../api";
import { Dialog, useToast } from "../components/Dialog";
import { AppShell } from "../components/Shell";
import { Icon } from "../components/Icon";
import { Avatar, Button, EmptyState, Notice } from "../components/ui";
import { TotpCard } from "./TotpCard";
import { TotpForm, type TotpDraft } from "./TotpForm";

export function AdminTotpPage() {
  const toast = useToast();
  const [rows, setRows] = useState<TotpEntry[] | null>(null);
  const [users, setUsers] = useState<User[]>([]);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<TotpEntry | null>(null);
  const [creating, setCreating] = useState(false);

  const load = () => api.adminTotp().then((r) => setRows(r.entries)).catch((e) => setError(errorMessage(e)));
  useEffect(() => { void load(); }, []);
  useEffect(() => {
    api.users().then((r) => setUsers(r.users.filter((u) => !u.disabled))).catch(() => setUsers([]));
  }, []);
  useEffect(() => {
    const soonest = rows?.reduce((min, e) => Math.min(min, e.remaining || e.period || 30), 30) ?? 0;
    if (!soonest) return;
    const timer = window.setTimeout(() => { void load(); }, soonest * 1000);
    return () => window.clearTimeout(timer);
  }, [rows]);

  const save = async (draft: TotpDraft) => {
    const body = { ...draft, userIds: draft.userIds ?? [] };
    if (editing) await api.updateAdminTotp(editing.id, body);
    else await api.createAdminTotp(body);
    toast(editing ? "验证器已保存" : "验证器已添加");
    setEditing(null);
    setCreating(false);
    await load();
  };
  const remove = async (entry: TotpEntry) => {
    if (!window.confirm(`删除“${entry.name}”后，已分配的用户也不能再生成验证码。`)) return;
    try { await api.deleteAdminTotp(entry.id); toast("验证器已删除"); await load(); } catch (e) { setError(errorMessage(e)); }
  };

  return (
    <AppShell
      title="共享验证器"
      description="密钥加密保存在服务器上，分配给哪些用户，哪些用户就能看到当前验证码。用户自己添加的验证器不会出现在这里。"
      actions={<Button onClick={() => setCreating(true)}><Icon name="plus" className="size-4" />添加验证器</Button>}
    >
      {error && <div className="mb-4"><Notice>{error}</Notice></div>}
      {rows?.length === 0 && (
        <EmptyState
          icon="timer"
          title="还没有共享验证器"
          description="添加一个 TOTP 密钥，再把它分配给需要的成员，他们就能在自己的账户里看到验证码。"
          action={<Button variant="secondary" onClick={() => setCreating(true)}><Icon name="plus" className="size-4" />添加验证器</Button>}
        />
      )}
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {rows?.map((entry) => {
          const assigned = users.filter((u) => entry.userIds?.includes(u.id));
          return (
            <TotpCard
              key={entry.id}
              entry={entry}
              onEdit={() => setEditing(entry)}
              onDelete={() => remove(entry)}
              footer={
                <div className="flex items-center gap-2">
                  {assigned.length > 0 && (
                    <span className="flex -space-x-1.5">
                      {assigned.slice(0, 4).map((u) => (
                        <span key={u.id} className="rounded-full ring-2 ring-[#f1f2ee]">
                          <Avatar text={u.name || u.email} size="sm" tone="plain" />
                        </span>
                      ))}
                    </span>
                  )}
                  <span className="min-w-0 truncate">
                    {assigned.length ? `分配给 ${assigned.map((u) => u.name || u.email).join("、")}` : "还没有分配给任何人"}
                  </span>
                </div>
              }
            />
          );
        })}
      </div>
      <Dialog open={creating || editing !== null} onClose={() => { setCreating(false); setEditing(null); }} wide title={editing ? "编辑验证器" : "添加验证器"} description="共享验证器的密钥只保存在服务器上。">
        {(creating || editing) && <TotpForm entry={editing ?? undefined} users={users} onSubmit={save} onCancel={() => { setCreating(false); setEditing(null); }} />}
      </Dialog>
    </AppShell>
  );
}
