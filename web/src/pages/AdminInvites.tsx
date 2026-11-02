import { useEffect, useState, type FormEvent } from "react";
import { api, errorMessage, type Invite } from "../api";
import { Dialog, useToast } from "../components/Dialog";
import { AppShell } from "../components/Shell";
import { Icon } from "../components/Icon";
import { Badge, Button, Check, Copyable, Field, GroupLabel, Notice } from "../components/ui";
import { Link } from "../router";
import { formatDate } from "../session";

const statusBadge: Record<Invite["status"], { tone: "seal" | "plain" | "void"; label: string }> = {
  pending: { tone: "seal", label: "待使用" },
  used: { tone: "plain", label: "已使用" },
  expired: { tone: "void", label: "已过期" },
  revoked: { tone: "void", label: "已撤销" },
};

type Created = { code: string; link: string; emailed: boolean; email: string; sendFailed: boolean };

export function AdminInvitesPage() {
  const toast = useToast();
  const [mailEnabled, setMailEnabled] = useState(false);
  const [invites, setInvites] = useState<Invite[] | null>(null);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<Created | null>(null);
  const [defaults, setDefaults] = useState({ expiresInDays: 7, maxExpiresInDays: 90 });

  const loadInvites = () =>
    api
      .invites()
      .then((r) => setInvites(r.invites))
      .catch((e) => setError(errorMessage(e)));

  useEffect(() => {
    api
      .registrationSettings()
      .then((r) => {
        setMailEnabled(r.registration.mailEnabled);
        setDefaults(r.inviteDefaults);
      })
      .catch((e) => setError(errorMessage(e)));
    void loadInvites();
  }, []);

  const revoke = async (inv: Invite) => {
    if (!window.confirm("撤销后这个邀请码将无法再使用。")) return;
    try {
      await api.revokeInvite(inv.id);
      setError("");
      toast("邀请码已撤销");
      await loadInvites();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  return (
    <AppShell
      title="注册与邀请"
      description="生成邀请码、查看谁用过。谁可以注册、要不要验证邮箱，在系统设置里调整。"
      actions={<Button onClick={() => setCreating(true)}><Icon name="plus" className="size-4" />生成邀请码</Button>}
    >
      {error && <div className="mb-4"><Notice>{error}</Notice></div>}

      <div className="mb-4 text-sm text-ink-soft">
        <Link href="/admin/settings" className="font-medium text-ink underline decoration-line underline-offset-4 hover:decoration-ink">
          打开系统设置
        </Link>
      </div>
      <div className="space-y-6">
        <section className="panel overflow-hidden">
          <header className="flex items-start gap-3.5 px-5 pt-5 pb-4 sm:px-6 sm:pt-6">
            <div className="min-w-0 flex-1">
              <h2 className="text-[0.9375rem] leading-6 font-semibold">邀请码</h2>
              <p className="mt-0.5 text-[0.8125rem] leading-5 text-ink-soft">
                每个邀请码只能使用一次，创建后只显示一次，请及时复制或发送给对方。这里只显示最近 200 条。
              </p>
            </div>
          </header>
          <div className="overflow-x-auto border-t border-line">
            <table className="data-table min-w-[720px]">
              <thead>
                <tr>
                  {["状态", "邮箱", "留言", "创建", "过期", "使用者", ""].map((h, i) => (
                    <th key={i} scope="col">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {invites?.map((inv) => {
                  const badge = statusBadge[inv.status];
                  return (
                    <tr key={inv.id}>
                      <td><Badge tone={badge.tone} dot={inv.status === "pending"}>{badge.label}</Badge></td>
                      <td className="whitespace-nowrap">{inv.email || <span className="text-ink-faint">不限邮箱</span>}</td>
                      <td className="max-w-[16rem] truncate text-ink-soft" title={inv.note}>{inv.note || "—"}</td>
                      <td className="whitespace-nowrap text-ink-soft">{formatDate(inv.createdAt)}</td>
                      <td className="whitespace-nowrap text-ink-soft">{formatDate(inv.expiresAt)}</td>
                      <td className="whitespace-nowrap text-ink-soft">
                        {inv.status === "used" ? inv.usedByEmail || "已使用" : "—"}
                      </td>
                      <td className="text-right whitespace-nowrap">
                        {inv.status === "pending" && (
                          <Button variant="ghost" size="sm" onClick={() => revoke(inv)}>撤销</Button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            {invites && invites.length === 0 && (
              <p className="p-8 text-center text-sm text-ink-soft">还没有邀请码。点击右上角生成一个。</p>
            )}
          </div>
        </section>
      </div>

      <CreateInviteDialog
        open={creating}
        mailEnabled={mailEnabled}
        defaultDays={defaults.expiresInDays}
        maxDays={defaults.maxExpiresInDays}
        onClose={() => setCreating(false)}
        onCreated={(c) => {
          setCreating(false);
          setCreated(c);
          void loadInvites();
        }}
      />

      <Dialog
        open={!!created}
        onClose={() => setCreated(null)}
        title="邀请码已生成"
        description="关闭后不会再次显示邀请码。"
        footer={<Button onClick={() => setCreated(null)}>我已保存</Button>}
      >
        {created && (
          <div className="space-y-4">
            {created.emailed && <Notice tone="info">邀请邮件已发送至 {created.email}。</Notice>}
            {created.sendFailed && <Notice>邀请邮件没有发出。请把下面的邀请码或链接手动发给对方。</Notice>}
            <div>
              <GroupLabel as="p">邀请码</GroupLabel>
              <Copyable value={created.code} label="邀请码" />
            </div>
            <div>
              <GroupLabel as="p">注册链接</GroupLabel>
              <Copyable value={created.link} label="注册链接" wrap />
            </div>
            <p className="text-[0.8125rem] leading-5 text-ink-soft">
              把邀请码或链接通过安全的方式发给对方。对方打开链接后可以直接完成注册。
            </p>
          </div>
        )}
      </Dialog>
    </AppShell>
  );
}

function CreateInviteDialog({
  open,
  mailEnabled,
  defaultDays,
  maxDays,
  onClose,
  onCreated,
}: {
  open: boolean;
  mailEnabled: boolean;
  defaultDays: number;
  maxDays: number;
  onClose: () => void;
  onCreated: (c: Created) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [email, setEmail] = useState("");
  const [sendEmail, setSendEmail] = useState(true);

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const d = new FormData(e.currentTarget);
    setBusy(true);
    setError("");
    try {
      const to = String(d.get("email") ?? "").trim();
      const wantMail = mailEnabled && !!to && sendEmail;
      const r = await api.createInvite({
        email: to,
        note: String(d.get("note") ?? "").trim(),
        expiresInDays: Number(d.get("expiresInDays")),
        sendEmail: wantMail,
      });
      onCreated({ code: r.code, link: r.link, emailed: r.emailed, email: to, sendFailed: wantMail && !r.emailed });
      setEmail("");
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
      title="生成邀请码"
      description="可选绑定邮箱；绑定后只有该邮箱能使用这个邀请码。"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>取消</Button>
          <Button type="submit" form="create-invite" busy={busy}>生成邀请码</Button>
        </>
      }
    >
      <form id="create-invite" onSubmit={submit} className="space-y-4">
        {error && <Notice>{error}</Notice>}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label="受邀邮箱"
            name="email"
            type="email"
            autoComplete="off"
            placeholder="选填"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoFocus
          />
          <Field label="有效期（天）" name="expiresInDays" type="number" min={1} max={maxDays} defaultValue={defaultDays} required />
        </div>
        <Field label="留言" name="note" maxLength={200} autoComplete="off" hint="选填。会写入邀请邮件，方便对方确认来源。" />
        <div className="rounded-xl border border-line bg-white/70 px-3.5 py-3">
          <Check
            label="发送邀请邮件"
            description={
              mailEnabled
                ? email
                  ? "通过已配置的 SMTP 把邀请码和链接发送到上面的邮箱。"
                  : "先填写受邀邮箱才能发送邮件。"
                : "这个站点没有配置 SMTP，无法自动发送。请手动复制邀请码。"
            }
            checked={mailEnabled && !!email && sendEmail}
            onChange={setSendEmail}
            disabled={!mailEnabled || !email}
          />
        </div>
      </form>
    </Dialog>
  );
}
