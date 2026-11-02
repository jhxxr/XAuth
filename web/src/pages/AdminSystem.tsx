import { useEffect, useState, type FormEvent } from "react";
import { api, errorMessage, type RegistrationMode, type RegistrationSettings, type SMTPSettings, type SystemSettings, type XMailSettings } from "../api";
import { useToast } from "../components/Dialog";
import { AppShell } from "../components/Shell";
import { Button, Check, Field, GroupLabel, Notice, Section, Segmented } from "../components/ui";
import { useSession } from "../session";

const modes: [RegistrationMode, string][] = [
  ["closed", "关闭注册"],
  ["invite", "仅限邀请"],
  ["open", "开放注册"],
];

export function AdminSystemPage() {
  const toast = useToast();
  const { reload } = useSession();
  const [registration, setRegistration] = useState<RegistrationSettings | null>(null);
  const [smtp, setSmtp] = useState<SMTPSettings | null>(null);
  const [xmail, setXMail] = useState<XMailSettings | null>(null);
  const [error, setError] = useState("");
  const [savingReg, setSavingReg] = useState(false);
  const [savingSMTP, setSavingSMTP] = useState(false);
  const [savingXMail, setSavingXMail] = useState(false);

  useEffect(() => {
    api
      .registrationSettings()
      .then((r) => {
        setRegistration(r.registration);
        setSmtp(r.smtp);
        setXMail(r.xmail);
      })
      .catch((e) => setError(errorMessage(e)));
  }, []);

  const saveRegistration = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!registration) return;
    setSavingReg(true);
    setError("");
    try {
      const r = await api.updateRegistrationSettings({
        mode: registration.mode,
        inviteVerifyEmail: registration.inviteVerifyEmail,
      });
      setRegistration(r.registration);
      setSmtp(r.smtp);
      setXMail(r.xmail);
      await reload();
      toast("注册设置已保存");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSavingReg(false);
    }
  };

  return (
    <AppShell
      title="系统设置"
      description="注册方式、发信服务器和 xmail。这里保存的配置立即生效，并盖过环境变量，直到改回为止。"
    >
      {error && <div className="mb-4"><Notice>{error}</Notice></div>}
      <div className="space-y-6">
        <Section
          title="注册"
          description="决定谁能创建账户，以及邀请注册时是否必须验证邮箱。"
          icon="settings"
          onSubmit={saveRegistration}
          footer={<Button type="submit" busy={savingReg}>保存注册设置</Button>}
        >
          {registration ? (
            <div className="space-y-5">
              <div>
                <GroupLabel as="p">新账户来源</GroupLabel>
                <Segmented
                  label="注册模式"
                  options={modes}
                  value={registration.mode}
                  onChange={(mode) => setRegistration({ ...registration, mode })}
                />
                <p className="mt-2 text-[0.8125rem] leading-5 text-ink-soft">
                  {registration.mode === "closed"
                    ? "任何人都不能自行注册，只有管理员能在用户页创建账户。这段时间邀请码不能兑换；改回邀请模式后，未过期的邀请码仍然有效。"
                    : registration.mode === "invite"
                      ? "只有持有邀请码的人可以注册。邀请码在「注册与邀请」页生成。"
                      : "任何人都可以通过登录页的注册入口创建账户，不需要邀请码。"}
                </p>
              </div>
              <div className="rounded-xl border border-line bg-white/70 px-3.5 py-3">
                <Check
                  label="邀请注册需要邮箱验证"
                  description={
                    !registration.mailEnabled && !registration.inviteVerifyEmail
                      ? "还没有可用的发信服务器，先在下面配置 SMTP 才能打开。"
                      : "关闭时，只有绑定了邮箱的邀请码会把该地址直接标为已确认。未绑定邮箱的邀请码不会自动确认。"
                  }
                  checked={registration.inviteVerifyEmail}
                  onChange={(inviteVerifyEmail) => setRegistration({ ...registration, inviteVerifyEmail })}
                  disabled={!registration.mailEnabled && !registration.inviteVerifyEmail}
                />
              </div>
            </div>
          ) : (
            <p className="text-sm text-ink-soft">正在加载设置…</p>
          )}
        </Section>

        {smtp && (
          <SMTPForm
            smtp={smtp}
            busy={savingSMTP}
            onError={setError}
            onSaved={async (r) => {
              setRegistration(r.registration);
              setSmtp(r.smtp);
              setXMail(r.xmail);
              await reload();
            }}
            onBusy={setSavingSMTP}
          />
        )}
        {xmail && (
          <XMailForm
            xmail={xmail}
            busy={savingXMail}
            onError={setError}
            onSaved={async (r) => {
              setRegistration(r.registration);
              setSmtp(r.smtp);
              setXMail(r.xmail);
              await reload();
            }}
            onBusy={setSavingXMail}
          />
        )}
      </div>
    </AppShell>
  );
}

function SMTPForm({
  smtp,
  busy,
  onError,
  onSaved,
  onBusy,
}: {
  smtp: SMTPSettings;
  busy: boolean;
  onError: (msg: string) => void;
  onSaved: (r: SystemSettings) => Promise<void>;
  onBusy: (v: boolean) => void;
}) {
  const toast = useToast();
  const [host, setHost] = useState(smtp.host);
  const [port, setPort] = useState(String(smtp.port || 587));
  const [user, setUser] = useState(smtp.user);
  const [password, setPassword] = useState("");
  const [clearPassword, setClearPassword] = useState(false);
  const [from, setFrom] = useState(smtp.from);
  const [tls, setTls] = useState(smtp.tls);

  useEffect(() => {
    setHost(smtp.host);
    setPort(String(smtp.port || 587));
    setUser(smtp.user);
    setFrom(smtp.from);
    setTls(smtp.tls);
    setPassword("");
    setClearPassword(false);
  }, [smtp]);

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    onBusy(true);
    onError("");
    try {
      const r = await api.updateSMTP({
        host: host.trim(),
        port: Number(port),
        user: user.trim(),
        password,
        clearPassword,
        from: from.trim(),
        tls,
      });
      await onSaved(r);
      toast(r.smtp.enabled ? "发信设置已保存" : "发信已停用");
    } catch (err) {
      onError(errorMessage(err));
    } finally {
      onBusy(false);
    }
  };

  const reset = async () => {
    if (!window.confirm("放弃后台保存的发信配置，改回服务器环境变量里的值？")) return;
    onBusy(true);
    onError("");
    try {
      const r = await api.resetSMTP();
      await onSaved(r);
      toast("已改回环境变量中的发信配置");
    } catch (err) {
      onError(errorMessage(err));
    } finally {
      onBusy(false);
    }
  };

  const disable = async () => {
    if (!window.confirm("停用后，邮箱验证、找回密码和邀请邮件都不会再发送。")) return;
    onBusy(true);
    onError("");
    try {
      const r = await api.updateSMTP({
        host: "",
        port: 587,
        user: "",
        password: "",
        clearPassword: true,
        from: "",
        tls: true,
      });
      await onSaved(r);
      toast("发信已停用");
    } catch (err) {
      onError(errorMessage(err));
    } finally {
      onBusy(false);
    }
  };

  return (
    <Section
      title="发信"
      description="用于邮箱验证、找回密码和邀请邮件。密码保存后不再显示。"
      icon="mail"
      onSubmit={submit}
      footer={
        <>
          {smtp.source === "database" && (
            <Button type="button" variant="ghost" onClick={reset} disabled={busy}>
              改回环境变量
            </Button>
          )}
          {smtp.enabled && (
            <Button type="button" variant="ghost" onClick={disable} disabled={busy}>
              停用发信
            </Button>
          )}
          <Button type="submit" busy={busy}>保存发信设置</Button>
        </>
      }
    >
      <div className="space-y-4">
        <p className="text-[0.8125rem] leading-5 text-ink-soft">
          {smtp.source === "database"
            ? "当前使用后台保存的配置，优先于环境变量。"
            : smtp.enabled
              ? "当前使用环境变量。在这里保存后，改为使用后台配置，重启也不会丢。"
              : "还没有发信服务器。填写后保存即可，不必改环境变量或重启。"}
        </p>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="主机" value={host} onChange={(e) => setHost(e.target.value)} autoComplete="off" placeholder="smtp.example.com" />
          <Field label="端口" type="number" min={1} max={65535} value={port} onChange={(e) => setPort(e.target.value)} required />
          <Field label="用户名" value={user} onChange={(e) => setUser(e.target.value)} autoComplete="off" placeholder="不需要认证就留空" />
          <Field
            label="密码"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            placeholder={smtp.passwordSet ? "已设置，留空则不修改" : "选填"}
          />
        </div>
        <Field label="发件地址" type="email" value={from} onChange={(e) => setFrom(e.target.value)} autoComplete="off" placeholder="no-reply@example.com" />
        <div className="space-y-3">
          <div className="rounded-xl border border-line bg-white/70 px-3.5 py-3">
            <Check label="使用 STARTTLS" description="只在可信的内网 SMTP 上关闭。" checked={tls} onChange={setTls} />
          </div>
          {smtp.passwordSet && (
            <div className="rounded-xl border border-line bg-white/70 px-3.5 py-3">
              <Check
                label="清除已保存的密码"
                description="下次保存时去掉密码。如果还留着用户名，保存会被拒绝。"
                checked={clearPassword}
                onChange={setClearPassword}
              />
            </div>
          )}
        </div>
      </div>
    </Section>
  );
}

function XMailForm({
  xmail,
  busy,
  onError,
  onSaved,
  onBusy,
}: {
  xmail: XMailSettings;
  busy: boolean;
  onError: (msg: string) => void;
  onSaved: (r: SystemSettings) => Promise<void>;
  onBusy: (v: boolean) => void;
}) {
  const toast = useToast();
  const [baseUrl, setBaseUrl] = useState(xmail.baseUrl);
  const [apiKey, setApiKey] = useState("");
  const [clearKey, setClearKey] = useState(false);

  useEffect(() => {
    setBaseUrl(xmail.baseUrl);
    setApiKey("");
    setClearKey(false);
  }, [xmail]);

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    onBusy(true);
    onError("");
    try {
      const r = await api.updateXMail({ baseUrl: baseUrl.trim(), apiKey, clearKey });
      await onSaved(r);
      toast(r.xmail.enabled ? "xmail 已保存" : "xmail 已停用");
    } catch (err) {
      onError(errorMessage(err));
    } finally {
      onBusy(false);
    }
  };

  const reset = async () => {
    if (!window.confirm("放弃后台保存的 xmail 配置，改回服务器环境变量里的值？")) return;
    onBusy(true);
    onError("");
    try {
      const r = await api.resetXMail();
      await onSaved(r);
      toast("已改回环境变量中的 xmail 配置");
    } catch (err) {
      onError(errorMessage(err));
    } finally {
      onBusy(false);
    }
  };

  const disable = async () => {
    if (!window.confirm("停用后，邮件中台和用户的收信都会关闭，直到重新填写。")) return;
    onBusy(true);
    onError("");
    try {
      const r = await api.updateXMail({ baseUrl: "", apiKey: "", clearKey: true });
      await onSaved(r);
      toast("xmail 已停用");
    } catch (err) {
      onError(errorMessage(err));
    } finally {
      onBusy(false);
    }
  };

  return (
    <Section
      title="xmail"
      description="收信用的 xmail 地址和 API Key。Key 保存后不再显示，也不会出现在操作记录里。"
      icon="inbox"
      onSubmit={submit}
      footer={
        <>
          {xmail.source === "database" && (
            <Button type="button" variant="ghost" onClick={reset} disabled={busy}>
              改回环境变量
            </Button>
          )}
          {xmail.enabled && (
            <Button type="button" variant="ghost" onClick={disable} disabled={busy}>
              停用 xmail
            </Button>
          )}
          <Button type="submit" busy={busy}>保存 xmail</Button>
        </>
      }
    >
      <div className="space-y-4">
        <p className="text-[0.8125rem] leading-5 text-ink-soft">
          {xmail.source === "database"
            ? "当前使用后台保存的配置，优先于环境变量。"
            : xmail.enabled
              ? "当前使用环境变量。在这里保存后，改为使用后台配置，重启也不会丢。"
              : "还没有连接 xmail。填写地址和 API Key 后保存即可，不必改环境变量或重启。"}
        </p>
        <Field
          label="地址"
          value={baseUrl}
          onChange={(e) => setBaseUrl(e.target.value)}
          autoComplete="off"
          placeholder="https://mail.example.com"
        />
        <Field
          label="API Key"
          type="password"
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          autoComplete="new-password"
          placeholder={xmail.keySet ? "已设置，留空则不修改" : "sk_live_..."}
        />
        {xmail.keySet && (
          <div className="rounded-xl border border-line bg-white/70 px-3.5 py-3">
            <Check
              label="清除已保存的 API Key"
              description="下次保存时去掉 Key。如果还留着地址，保存会被拒绝。"
              checked={clearKey}
              onChange={setClearKey}
            />
          </div>
        )}
      </div>
    </Section>
  );
}
