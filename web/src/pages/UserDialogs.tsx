import { useState, type FormEvent, type ReactNode } from "react";
import { api, errorMessage, type User } from "../api";
import { Dialog, useToast } from "../components/Dialog";
import { Button, Check, Field, GroupLabel, Notice } from "../components/ui";

type CreateProps = { open: boolean; onClose: () => void; onSaved: (u: User) => void };

export function CreateUserDialog({ open, onClose, onSaved }: CreateProps) {
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [isAdmin, setIsAdmin] = useState(false);
  const [verified, setVerified] = useState(false);

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const d = new FormData(e.currentTarget);
    setBusy(true);
    setError("");
    try {
      const r = await api.createUser({
        email: String(d.get("email")),
        name: String(d.get("name")),
        password: String(d.get("password")),
        isAdmin,
        emailVerified: verified,
      });
      onSaved(r.user);
      toast(`已添加 ${r.user.email}`);
      setIsAdmin(false);
      setVerified(false);
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
      title="添加用户"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button type="submit" form="create-user" busy={busy}>
            添加用户
          </Button>
        </>
      }
    >
      <form id="create-user" onSubmit={submit} className="space-y-4">
        {error && <Notice>{error}</Notice>}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="邮箱" name="email" type="email" required autoFocus autoComplete="off" />
          <Field label="名称" name="name" maxLength={64} autoComplete="off" placeholder="选填" />
        </div>
        <Field label="初始密码" name="password" type="password" minLength={8} required autoComplete="new-password" hint="至少 8 个字符。请通过安全的方式告诉对方，并提醒对方登录后更改。" />
        <ToggleList>
          <Check label="管理员" description="可以管理用户和应用。" checked={isAdmin} onChange={setIsAdmin} />
          <Check label="邮箱已确认" description="应用会收到 email_verified = true。" checked={verified} onChange={setVerified} />
        </ToggleList>
      </form>
    </Dialog>
  );
}

type EditProps = { user: User | null; self: boolean; onClose: () => void; onSaved: (u: User) => void };

export function EditUserDialog({ user, self, onClose, onSaved }: EditProps) {
  return (
    <Dialog open={!!user} onClose={onClose} title={user ? `编辑 ${user.name || user.email}` : ""} description={user?.name ? user.email : undefined} footer={<EditFooter onClose={onClose} />}>
      {user && <EditForm key={user.id} user={user} self={self} onSaved={onSaved} />}
    </Dialog>
  );
}

function EditFooter({ onClose }: { onClose: () => void }) {
  return (
    <>
      <Button variant="ghost" onClick={onClose}>
        取消
      </Button>
      <Button type="submit" form="edit-user">
        保存更改
      </Button>
    </>
  );
}

function EditForm({ user, self, onSaved }: { user: User; self: boolean; onSaved: (u: User) => void }) {
  const toast = useToast();
  const [isAdmin, setIsAdmin] = useState(user.isAdmin);
  const [verified, setVerified] = useState(user.emailVerified);
  const [disabled, setDisabled] = useState(user.disabled);
  const [error, setError] = useState("");

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const d = new FormData(e.currentTarget);
    const password = String(d.get("password"));
    setError("");
    try {
      const r = await api.updateUser(user.id, {
        name: String(d.get("name")),
        isAdmin,
        emailVerified: verified,
        disabled,
        ...(password ? { password } : {}),
      });
      onSaved(r.user);
      toast("更改已保存");
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  return (
    <form id="edit-user" onSubmit={submit} className="space-y-4">
      {error && <Notice>{error}</Notice>}
      <Field label="名称" name="name" defaultValue={user.name} maxLength={64} autoComplete="off" />
      <div>
        <GroupLabel as="p">权限与状态</GroupLabel>
        <ToggleList>
          <Check label="管理员" description={self ? "不能取消自己的管理员权限。" : "可以管理用户和应用。"} checked={isAdmin} onChange={setIsAdmin} disabled={self} />
          <Check label="邮箱已确认" description="应用会收到 email_verified = true。" checked={verified} onChange={setVerified} />
          <Check
            label="停用账户"
            description={self ? "不能停用自己。" : "停用后立即退出所有设备，已签发的令牌失效。"}
            checked={disabled}
            onChange={setDisabled}
            disabled={self}
          />
        </ToggleList>
      </div>
      <Field label="设置新密码" name="password" type="password" minLength={8} autoComplete="new-password" hint="留空则不更改。设置后对方会在所有设备上退出登录。" />
    </form>
  );
}

/** Checkboxes stacked in a bordered settings list. */
function ToggleList({ children }: { children: ReactNode }) {
  return <div className="divide-y divide-line/80 rounded-xl border border-line bg-white/70 [&>label]:px-3.5 [&>label]:py-3">{children}</div>;
}
