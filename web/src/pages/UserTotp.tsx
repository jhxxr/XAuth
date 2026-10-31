import { useEffect, useState } from "react";
import { api, errorMessage, type TotpEntry } from "../api";
import { Dialog, useToast } from "../components/Dialog";
import { AppShell } from "../components/Shell";
import { Icon } from "../components/Icon";
import { Button, EmptyState, Notice } from "../components/ui";
import { TotpCard } from "./TotpCard";
import { TotpForm, type TotpDraft } from "./TotpForm";

export function UserTotpPage() {
  const toast = useToast();
  const [rows, setRows] = useState<TotpEntry[] | null>(null);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<TotpEntry | null>(null);
  const [creating, setCreating] = useState(false);

  const load = () => api.myTotp().then((r) => setRows(r.entries)).catch((e) => setError(errorMessage(e)));
  useEffect(() => { void load(); }, []);
  useEffect(() => {
    const soonest = rows?.reduce((min, e) => Math.min(min, e.remaining || e.period || 30), 30) ?? 0;
    if (!soonest) return;
    const timer = window.setTimeout(() => { void load(); }, soonest * 1000);
    return () => window.clearTimeout(timer);
  }, [rows]);

  const save = async (draft: TotpDraft) => {
    if (editing) await api.updateMyTotp(editing.id, draft);
    else await api.createMyTotp(draft);
    toast(editing ? "验证器已保存" : "验证器已添加");
    setEditing(null);
    setCreating(false);
    await load();
  };
  const remove = async (entry: TotpEntry) => {
    if (!window.confirm(`删除“${entry.name}”？删除后无法恢复。`)) return;
    try { await api.deleteMyTotp(entry.id); toast("验证器已删除"); await load(); } catch (e) { setError(errorMessage(e)); }
  };

  const shared = rows?.filter((e) => e.shared) ?? [];
  const mine = rows?.filter((e) => !e.shared) ?? [];
  const grid = (list: TotpEntry[]) => (
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
      {list.map((entry) => (
        <TotpCard
          key={entry.id}
          entry={entry}
          onEdit={entry.shared ? undefined : () => setEditing(entry)}
          onDelete={entry.shared ? undefined : () => remove(entry)}
        />
      ))}
    </div>
  );

  return (
    <AppShell
      title="验证器"
      description="共享验证器由管理员分配。私人验证器只有你自己能看到，管理员也不能分配给别人。"
      actions={<Button onClick={() => setCreating(true)}><Icon name="plus" className="size-4" />添加私人验证器</Button>}
    >
      {error && <div className="mb-4"><Notice>{error}</Notice></div>}
      {rows?.length === 0 && (
        <EmptyState
          icon="timer"
          title="还没有验证器"
          description="还没有分配给你的验证器。你可以添加一个只有自己能用的。"
          action={<Button variant="secondary" onClick={() => setCreating(true)}><Icon name="plus" className="size-4" />添加私人验证器</Button>}
        />
      )}
      <div className="space-y-8">
        {shared.length > 0 && (
          <section>
            <h2 className="eyebrow mb-3">管理员分配 · {shared.length}</h2>
            {grid(shared)}
          </section>
        )}
        {mine.length > 0 && (
          <section>
            <h2 className="eyebrow mb-3">私人 · {mine.length}</h2>
            {grid(mine)}
          </section>
        )}
      </div>
      <Dialog open={creating || editing !== null} onClose={() => { setCreating(false); setEditing(null); }} wide title={editing ? "编辑私人验证器" : "添加私人验证器"} description="只有你自己能看到这个验证器。">
        {(creating || editing) && <TotpForm entry={editing ?? undefined} onSubmit={save} onCancel={() => { setCreating(false); setEditing(null); }} />}
      </Dialog>
    </AppShell>
  );
}
