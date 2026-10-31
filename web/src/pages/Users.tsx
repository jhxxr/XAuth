import { useEffect, useState } from "react";
import { api, errorMessage, type User } from "../api";
import { useToast } from "../components/Dialog";
import { AppShell } from "../components/Shell";
import { Icon } from "../components/Icon";
import { Avatar, Badge, Button, Notice, SearchField } from "../components/ui";
import { formatDate, useSession } from "../session";
import { CreateUserDialog, EditUserDialog } from "./UserDialogs";

export function UsersPage() {
  const { session } = useSession();
  const [users, setUsers] = useState<User[] | null>(null);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<User | null>(null);
  const [query, setQuery] = useState("");
  const toast = useToast();

  useEffect(() => {
    api
      .users()
      .then((r) => setUsers(r.users))
      .catch((e) => setError(errorMessage(e)));
  }, []);

  const saved = (u: User) => {
    setUsers((prev) => {
      const list = prev ?? [];
      return list.some((x) => x.id === u.id) ? list.map((x) => (x.id === u.id ? u : x)) : [u, ...list];
    });
    setCreating(false);
    setEditing(null);
  };

  const shown = users?.filter((u) => `${u.email} ${u.name}`.toLowerCase().includes(query.trim().toLowerCase())) ?? [];

  return (
    <AppShell
      title="用户"
      description={users ? `共 ${users.length} 位用户，${users.filter((u) => u.isAdmin).length} 位管理员。` : undefined}
      actions={<Button onClick={() => setCreating(true)}><Icon name="plus" className="size-4" />添加用户</Button>}
    >
      {error && <div className="mb-4"><Notice>{error}</Notice></div>}
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <SearchField value={query} onChange={setQuery} placeholder="搜索邮箱或名称" label="搜索用户" className="w-full max-w-xs" />
        {users && query && <p className="text-[0.8125rem] text-ink-soft tabular-nums">找到 {shown.length} 位</p>}
      </div>
      {users && (
        <div className="panel overflow-x-auto">
          <table className="data-table min-w-[640px]">
            <thead>
              <tr>
                {["用户", "身份", "状态", "最近登录", ""].map((h, i) => (
                  <th key={i} scope="col">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {shown.map((u) => (
                <tr key={u.id}>
                  <td>
                    <div className="flex items-center gap-3">
                      <Avatar text={u.name || u.email} src={u.avatarUrl} tone={u.isAdmin ? "ink" : "plain"} />
                      <div className="min-w-0">
                        <p className="truncate font-medium">{u.name || u.email}</p>
                        {u.name && <p className="truncate text-[0.8125rem] text-ink-soft">{u.email}</p>}
                      </div>
                    </div>
                  </td>
                  <td>{u.isAdmin ? <Badge tone="ink">管理员</Badge> : <Badge>成员</Badge>}</td>
                  <td>
                    <div className="flex flex-wrap gap-1.5">
                      {u.disabled ? <Badge tone="void" dot>已停用</Badge> : <Badge tone="seal" dot>启用</Badge>}
                      {u.emailVerified && <Badge>邮箱已确认</Badge>}
                    </div>
                  </td>
                  <td className="whitespace-nowrap text-ink-soft">{u.lastLoginAt ? formatDate(u.lastLoginAt) : <span className="text-ink-faint">从未登录</span>}</td>
                  <td className="text-right whitespace-nowrap">
                    {u.id !== session?.user?.id && (
                      <Button variant="ghost" size="sm" onClick={async () => {
                        if (!window.confirm(`退出 ${u.email} 的所有设备并使其令牌失效？`)) return;
                        try { await api.forceSignOut(u.id); toast("已退出该用户的所有设备"); } catch (e) { setError(errorMessage(e)); }
                      }}>退出所有设备</Button>
                    )}
                    <Button variant="ghost" size="sm" onClick={() => setEditing(u)} aria-label={`编辑 ${u.email}`}>
                      编辑
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {shown.length === 0 && <p className="p-8 text-center text-sm text-ink-soft">没有匹配“{query.trim()}”的用户。</p>}
        </div>
      )}
      <CreateUserDialog open={creating} onClose={() => setCreating(false)} onSaved={saved} />
      <EditUserDialog user={editing} self={editing?.id === session?.user?.id} onClose={() => setEditing(null)} onSaved={saved} />
    </AppShell>
  );
}
