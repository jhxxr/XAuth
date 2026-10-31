import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { api, errorMessage, type App, type AppType, type User } from "../api";
import { Dialog } from "../components/Dialog";
import type { IconName } from "../components/Icon";
import { Button, Check, Copyable, Field, GroupLabel, Notice, RadioCard, TextArea, UserChecklist } from "../components/ui";

export const appTypes: Record<AppType, { label: string; description: string; hint: string }> = {
  web: {
    label: "Web 应用",
    description: "在服务器上运行，可以保存客户端密钥。",
    hint: "例如 https://grafana.example.com/login/generic_oauth",
  },
  spa: {
    label: "单页应用",
    description: "在浏览器里运行，不保存密钥，只用 PKCE。",
    hint: "必须使用 https；开发模式下可以用 http://localhost。",
  },
  native: {
    label: "原生或命令行应用",
    description: "桌面、手机或 CLI 程序，不保存密钥，只用 PKCE。",
    hint: "例如 http://127.0.0.1/callback（端口可变）或 com.example.app:/callback",
  },
};

const lines = (s: string) => s.split("\n").map((l) => l.trim()).filter(Boolean);

type FormValues = { name: string; type: AppType; redirects: string; logouts: string; allowRefresh: boolean; access: "all" | "admins" | "selected"; allowedUserIds: string[] };

const accessOptions = [
  ["all", "所有用户", "任何已登录用户都可以使用。"],
  ["admins", "仅管理员", "只有管理员可以登录这个应用。"],
  ["selected", "指定用户", "只有下面勾选的用户可以登录。"],
] as const;

const typeIcons: Record<AppType, IconName> = { web: "globe", spa: "apps", native: "monitor" };
const accessIcons: Record<FormValues["access"], IconName> = { all: "users", admins: "shield", selected: "user" };

/** A titled block inside a long dialog form. */
function FormBlock({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-4 border-t border-line pt-5 first:border-0 first:pt-0">
      <h3 className="eyebrow">{title}</h3>
      {children}
    </section>
  );
}

function AppFields({ v, set, typeLocked }: { v: FormValues; set: (p: Partial<FormValues>) => void; typeLocked?: boolean }) {
  return (
    <div className="space-y-6">
      <FormBlock title="基本信息">
        <Field label="名称" value={v.name} onChange={(e) => set({ name: e.target.value })} maxLength={64} required autoComplete="off" hint="用户登录时会在页面上看到这个名称。" />
        <fieldset>
          <GroupLabel>类型{typeLocked && <span className="font-normal text-ink-soft">（创建后不能更改）</span>}</GroupLabel>
          <div className="grid gap-2 sm:grid-cols-3">
            {(Object.keys(appTypes) as AppType[]).map((t) => (
              <RadioCard key={t} name="type" icon={typeIcons[t]} checked={v.type === t} disabled={typeLocked} onChange={() => set({ type: t })} title={appTypes[t].label} description={appTypes[t].description} />
            ))}
          </div>
        </fieldset>
      </FormBlock>
      <FormBlock title="回调">
        <TextArea label="回调地址" value={v.redirects} onChange={(e) => set({ redirects: e.target.value })} required spellCheck={false} hint={`每行一个。${appTypes[v.type].hint}`} />
        <TextArea label="退出后跳转地址" value={v.logouts} onChange={(e) => set({ logouts: e.target.value })} spellCheck={false} hint="选填，每行一个。应用调用退出接口后，XAuth 只会跳回这些地址。" />
      </FormBlock>
      <FormBlock title="访问控制">
        <div className="rounded-xl border border-line bg-white/60 p-3.5">
          <Check label="允许刷新令牌" description="应用请求 offline_access 时签发刷新令牌，用户无需反复登录。" checked={v.allowRefresh} onChange={(allowRefresh) => set({ allowRefresh })} />
        </div>
        <fieldset>
          <GroupLabel>谁能登录</GroupLabel>
          <div className="grid gap-2 sm:grid-cols-3">
            {accessOptions.map(([value, label, description]) => (
              <RadioCard key={value} name="access" icon={accessIcons[value]} checked={v.access === value} onChange={() => set({ access: value })} title={label} description={description} />
            ))}
          </div>
        </fieldset>
        {v.access === "selected" && <UserPicker selected={v.allowedUserIds} onChange={(allowedUserIds) => set({ allowedUserIds })} />}
      </FormBlock>
    </div>
  );
}

function UserPicker({ selected, onChange }: { selected: string[]; onChange: (ids: string[]) => void }) {
  const [users, setUsers] = useState<User[]>([]);
  useEffect(() => { void api.users().then((r) => setUsers(r.users)); }, []);
  return <UserChecklist users={users} selected={selected} onChange={onChange} />;
}

function useAppForm(initial: FormValues) {
  const [v, setV] = useState(initial);
  return { v, set: (p: Partial<FormValues>) => setV((cur) => ({ ...cur, ...p })) };
}

const toPayload = (v: FormValues) => ({ name: v.name, redirectUris: lines(v.redirects), postLogoutRedirectUris: lines(v.logouts), allowRefresh: v.allowRefresh, access: v.access, allowedUserIds: v.allowedUserIds });

export function CreateAppDialog({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (a: App, secret?: string) => void }) {
  return (
    <Dialog open={open} onClose={onClose} wide title="添加应用" description="注册一个 OIDC 客户端，让它通过 XAuth 登录用户。" footer={<Footer form="create-app" label="添加应用" onClose={onClose} />}>
      <CreateForm onCreated={onCreated} />
    </Dialog>
  );
}

function Footer({ form, label, onClose }: { form: string; label: string; onClose: () => void }) {
  return (
    <>
      <Button variant="ghost" onClick={onClose}>
        取消
      </Button>
      <Button type="submit" form={form}>
        {label}
      </Button>
    </>
  );
}

function CreateForm({ onCreated }: { onCreated: (a: App, secret?: string) => void }) {
  const { v, set } = useAppForm({ name: "", type: "web", redirects: "", logouts: "", allowRefresh: true, access: "all", allowedUserIds: [] });
  const [error, setError] = useState("");
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    try {
      const r = await api.createApp({ ...toPayload(v), type: v.type });
      onCreated(r.app, r.secret);
    } catch (err) {
      setError(errorMessage(err));
    }
  };
  return (
    <form id="create-app" onSubmit={submit} className="space-y-4">
      {error && <Notice>{error}</Notice>}
      <AppFields v={v} set={set} />
    </form>
  );
}

export function SecretDialog({ app, secret, onClose }: { app: App | null; secret: string; onClose: () => void }) {
  return (
    <Dialog
      open={!!app && !!secret}
      onClose={onClose}
      dismissible={false}
      title="保存客户端密钥"
      footer={<Button onClick={onClose}>我已保存密钥</Button>}
    >
      <div className="space-y-4">
        <Notice tone="info">密钥只显示这一次。关闭后无法再查看，只能重新生成。</Notice>
        <div className="space-y-4 rounded-xl border border-line bg-white p-4 shadow-xs">
          <div>
            <p className="eyebrow mb-1.5">客户端 ID</p>
            <Copyable value={app?.id ?? ""} label="客户端 ID" wrap />
          </div>
          <div>
            <p className="eyebrow mb-1.5">客户端密钥</p>
            <Copyable value={secret} label="客户端密钥" wrap />
          </div>
        </div>
      </div>
    </Dialog>
  );
}

export { AppFields, toPayload, useAppForm, Footer };
