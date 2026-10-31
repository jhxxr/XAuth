import { useEffect, useState, type FormEvent } from "react";
import { api, errorMessage, type DeviceSession, type Grant, type Passkey } from "../api";
import { useToast } from "../components/Dialog";
import { Icon } from "../components/Icon";
import { AppShell } from "../components/Shell";
import { Badge, Button, Empty, Field, ListRow, Notice, Section } from "../components/ui";
import { formatDate, useSession } from "../session";
import { createPasskey, defaultPasskeyName, supportsPasskeys } from "../webauthn";

function ProfileForm({ user }: { user: { name: string; email: string } }) {
  const { setUser } = useSession();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const r = await api.updateAccount(String(new FormData(e.currentTarget).get("name")));
      setUser(r.user);
      toast("名称已保存");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Section
      icon="user"
      title="个人资料"
      description="登录其他应用时，它们会看到这个名称。"
      onSubmit={submit}
      footer={
        <>
          <p className="mr-auto text-[0.8125rem] text-ink-soft">最多 64 个字符。</p>
          <Button type="submit" size="sm" busy={busy}>
            保存
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        {error && <Notice>{error}</Notice>}
        <Field label="名称" name="name" defaultValue={user.name} maxLength={64} autoComplete="name" placeholder={user.email} className="max-w-md" />
      </div>
    </Section>
  );
}

function VerifyEmail() {
  const { session } = useSession();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const user = session?.user;
  if (!user || user.emailVerified || !session?.mailEnabled) return null;

  const send = async () => {
    setBusy(true);
    setError("");
    try {
      await api.requestVerifyEmail();
      toast("验证邮件已发送，请检查收件箱");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Section
      icon="mail"
      title="确认邮箱"
      description="确认后，应用会收到 email_verified = true。"
      action={
        <Button variant="secondary" size="sm" busy={busy} onClick={send}>
          发送验证邮件
        </Button>
      }
    >
      <div className="space-y-3">
        {error && <Notice>{error}</Notice>}
        <Notice tone="info">
          你的邮箱还没有确认。我们会把一次性链接发到 <span className="font-medium">{user.email}</span>，30 分钟内有效。
        </Notice>
      </div>
    </Section>
  );
}

function PasswordForm() {
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const data = new FormData(form);
    const next = String(data.get("new"));
    if (next !== String(data.get("confirm"))) {
      setError("两次输入的新密码不一致。");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await api.changePassword(String(data.get("current")), next);
      form.reset();
      toast("密码已更改，其他设备已退出登录");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Section
      icon="lock"
      title="更改密码"
      description="更改后，其他设备和应用需要重新登录。"
      onSubmit={submit}
      footer={
        <Button type="submit" size="sm" busy={busy}>
          更改密码
        </Button>
      }
    >
      <div className="space-y-4">
        {error && <Notice>{error}</Notice>}
        <Field label="当前密码" name="current" type="password" autoComplete="current-password" required className="sm:max-w-[calc(50%-0.5rem)]" />
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="新密码" name="new" type="password" autoComplete="new-password" minLength={8} required hint="至少 8 个字符。" />
          <Field label="确认新密码" name="confirm" type="password" autoComplete="new-password" minLength={8} required />
        </div>
      </div>
    </Section>
  );
}

function Passkeys() {
  const toast = useToast();
  const [rows, setRows] = useState<Passkey[] | null>(null);
  const [hasPassword, setHasPassword] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [supported] = useState(supportsPasskeys);

  const load = () => api.passkeys()
    .then((r) => { setRows(r.passkeys); setHasPassword(r.hasPassword); })
    .catch((e) => setError(errorMessage(e)));
  useEffect(() => { if (supported) void load(); }, [supported]);

  const add = async () => {
    setBusy(true);
    setError("");
    try {
      const name = defaultPasskeyName();
      const begin = await api.beginPasskeyRegister(name);
      const credential = await createPasskey(begin.options);
      await api.finishPasskeyRegister(begin.challengeId, name, credential);
      toast("通行密钥已添加");
      await load();
    } catch (err) {
      if (err instanceof DOMException && err.name === "NotAllowedError") setError("操作已取消，或这个设备不支持通行密钥。");
      else setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (row: Passkey) => {
    if (!window.confirm(`删除“${row.name}”？删除后这台设备将无法用它登录。`)) return;
    try {
      await api.deletePasskey(row.id);
      toast("通行密钥已删除");
      await load();
    } catch (e) { setError(errorMessage(e)); }
  };

  if (!supported) return null;
  return (
    <Section
      icon="fingerprint"
      title="通行密钥"
      description="用指纹、面容或设备 PIN 登录，不用输密码。"
      action={
        <Button variant="secondary" size="sm" busy={busy} onClick={add}>
          {!busy && <Icon name="plus" className="size-3.5" />}
          添加通行密钥
        </Button>
      }
    >
      {error && <div className="mb-3"><Notice>{error}</Notice></div>}
      <div className="divide-y divide-line/80">
        {rows?.map((row) => (
          <ListRow
            key={row.id}
            icon="key"
            title={row.name}
            meta={<>添加于 {formatDate(row.createdAt)}{row.lastUsedAt ? ` · 最近使用 ${formatDate(row.lastUsedAt)}` : ""}</>}
            action={<Button variant="ghost" size="sm" className="hover:text-void" disabled={rows.length <= 1 && !hasPassword} onClick={() => remove(row)}>删除</Button>}
          />
        ))}
      </div>
      {rows?.length === 0 && <Empty icon="fingerprint">还没有通行密钥。添加后，登录页会多出一个“使用通行密钥”按钮。</Empty>}
      {rows?.length === 1 && !hasPassword && <p className="mt-3 text-[0.8125rem] text-ink-soft">这是你唯一的登录方式，不能删除。请先设置密码。</p>}
    </Section>
  );
}

function deviceName(userAgent: string) {
  const browser = /Edg\//.test(userAgent) ? "Edge" : /Chrome\//.test(userAgent) ? "Chrome" : /Firefox\//.test(userAgent) ? "Firefox" : /Safari\//.test(userAgent) ? "Safari" : "浏览器";
  const os = /Windows/.test(userAgent) ? "Windows" : /Mac OS/.test(userAgent) ? "macOS" : /Android/.test(userAgent) ? "Android" : /iPhone|iPad/.test(userAgent) ? "iOS" : "未知系统";
  return `${browser} · ${os}`;
}

function Devices({ onCurrentEnded }: { onCurrentEnded: () => void }) {
  const toast = useToast();
  const [rows, setRows] = useState<DeviceSession[] | null>(null);
  const [error, setError] = useState("");
  const load = () => api.sessions().then((r) => setRows(r.sessions)).catch((e) => setError(errorMessage(e)));
  useEffect(() => { void load(); }, []);

  const remove = async (row: DeviceSession) => {
    if (!window.confirm(row.current ? "退出这台设备后需要重新登录。" : `退出 ${deviceName(row.userAgent)}？`)) return;
    try {
      await api.deleteSession(row.id);
      if (row.current) onCurrentEnded();
      else { toast("已退出该设备"); await load(); }
    } catch (e) { setError(errorMessage(e)); }
  };

  return (
    <Section
      icon="monitor"
      title="登录设备"
      description="退出某台设备后，那台设备上的浏览器需要重新登录。"
      action={(rows?.length ?? 0) > 1 && (
        <Button variant="secondary" size="sm" onClick={async () => {
          const r = await api.revokeOtherSessions();
          toast(`已退出其余 ${r.revoked} 台设备`);
          await load();
        }}>退出其余全部设备</Button>
      )}
    >
      {error && <div className="mb-3"><Notice>{error}</Notice></div>}
      <div className="divide-y divide-line/80">
        {rows?.map((row) => (
          <ListRow
            key={row.id}
            icon="monitor"
            title={deviceName(row.userAgent)}
            badge={row.current && <Badge tone="seal" dot>当前设备</Badge>}
            meta={<><span className="tabular-nums">{row.ip || "未知地址"}</span> · 最近使用 {formatDate(row.lastSeen)}</>}
            action={<Button variant="ghost" size="sm" className="hover:text-void" onClick={() => remove(row)}>退出</Button>}
          />
        ))}
      </div>
    </Section>
  );
}

function Grants() {
  const toast = useToast();
  const [rows, setRows] = useState<Grant[] | null>(null);
  const [error, setError] = useState("");
  const load = () => api.grants().then((r) => setRows(r.grants)).catch((e) => setError(errorMessage(e)));
  useEffect(() => { void load(); }, []);

  return (
    <Section icon="apps" title="已授权应用" description="撤销后，应用保存的登录会在下次使用时失效。">
      {error && <div className="mb-3"><Notice>{error}</Notice></div>}
      {rows?.length === 0 && <Empty icon="apps">还没有应用通过 XAuth 保持登录。</Empty>}
      <div className="divide-y divide-line/80">
        {rows?.map((row) => (
          <ListRow
            key={row.clientId}
            icon="apps"
            title={row.clientName}
            badge={row.refresh && <Badge tone="stamp">会保持登录</Badge>}
            meta={<>最近使用 {formatDate(row.lastUsed)}</>}
            action={<Button variant="ghost" size="sm" className="hover:text-void" onClick={async () => {
              if (!window.confirm(`撤销 ${row.clientName} 的登录？`)) return;
              try { await api.revokeGrant(row.clientId); toast("已撤销授权"); await load(); } catch (e) { setError(errorMessage(e)); }
            }}>撤销</Button>}
          />
        ))}
      </div>
    </Section>
  );
}

export function SettingsPage() {
  const { session } = useSession();
  const user = session?.user;
  if (!user) return null;
  return (
    <AppShell title="用户设置" description="管理你的资料、登录方式和已授权的应用。">
      <div className="mx-auto max-w-3xl space-y-5">
        <ProfileForm user={user} />
        <VerifyEmail />
        <PasswordForm />
        <Passkeys />
        <Devices onCurrentEnded={() => { window.location.assign("/login"); }} />
        <Grants />
      </div>
    </AppShell>
  );
}

