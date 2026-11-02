import { useEffect, useState, type FormEvent } from "react";
import { api, errorMessage, type InviteInfo } from "../api";
import { mrz, Pass, Stamp } from "../components/Pass";
import { Centered } from "../components/Shell";
import { Button, Field, Notice } from "../components/ui";
import { leave, Link, navigate, useLocation } from "../router";
import { useSession } from "../session";

export function RegisterPage() {
  const { query } = useLocation();
  const flowId = query.get("flow") ?? "";
  const initialCode = query.get("invite") ?? query.get("code") ?? "";
  const { session, setUser } = useSession();
  const [code, setCode] = useState(initialCode);
  const [invite, setInvite] = useState<InviteInfo | null>(null);
  const [checking, setChecking] = useState(initialCode !== "");
  const [attempt, setAttempt] = useState(0);
  const [codeError, setCodeError] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [issued, setIssued] = useState(false);

  useEffect(() => {
    if (session?.user && !flowId && !issued) navigate("/account", { replace: true });
  }, [session?.user, flowId, issued]);

  // Check a code as soon as one is present (from the invite link or the form).
  useEffect(() => {
    if (!code) return;
    let cancelled = false;
    setChecking(true);
    setCodeError("");
    api
      .inviteInfo(code)
      .then((r) => {
        if (!cancelled) setInvite(r.invite);
      })
      .catch((e) => {
        if (!cancelled) {
          setInvite(null);
          setCodeError(errorMessage(e));
        }
      })
      .finally(() => {
        if (!cancelled) setChecking(false);
      });
    return () => {
      cancelled = true;
    };
  }, [code, attempt]);

  if (!session) return <Centered>{null}</Centered>;
  const mode = session.registrationMode;
  const host = new URL(session.issuer).host;
  const loginHref = `/login${flowId ? `?flow=${encodeURIComponent(flowId)}` : ""}`;

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    const password = String(data.get("password"));
    if (password !== String(data.get("confirm"))) {
      setError("两次输入的密码不一致。");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const r = await api.register(String(data.get("email")), password, String(data.get("name")), flowId, code);
      setIssued(true);
      if (r.redirect) {
        leave(r.redirect);
        return;
      }
      setUser(r.user);
      navigate(r.flowError ? "/login?error=flow_expired" : "/account", { replace: true });
    } catch (err) {
      setError(errorMessage(err));
      setBusy(false);
    }
  };

  const enterCode = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const value = String(new FormData(e.currentTarget).get("code") ?? "").trim();
    setCodeError("");
    setInvite(null);
    setChecking(true);
    setCode(value.toUpperCase());
    setAttempt((n) => n + 1);
  };

  const clearCode = () => {
    setCode("");
    setCodeError("");
    setInvite(null);
    setChecking(false);
  };

  // With no invite in hand, invite-only and closed sites explain themselves.
  const blocked = mode === "closed";
  const needsCode = mode === "invite" && !code;

  return (
    <Centered>
      <Pass
        seed={session.issuer}
        eyebrow="申请"
        title="XAuth 账户"
        mark="X"
        level={1}
        mrzLines={mrz(host, host.replace(/\W/g, ""))}
        stamp={issued && <Stamp tone="issued" label="已签发" detail="NEW ACCOUNT" />}
      >
        {blocked ? (
          <div className="space-y-4">
            <Notice tone="info">当前不开放注册。请联系管理员为你创建账户。</Notice>
            <Link href={loginHref} className="inline-block text-sm font-semibold underline decoration-line underline-offset-4 hover:decoration-ink">
              返回登录
            </Link>
          </div>
        ) : needsCode ? (
          <form onSubmit={enterCode} className="space-y-4">
            <p className="text-sm leading-6 text-ink-soft">这个站点仅限邀请注册。请输入管理员发给你的邀请码。</p>
            <Field label="邀请码" name="code" autoComplete="off" autoFocus required placeholder="K7QF-9M2X-PW3D" />
            <Button type="submit" className="w-full">继续</Button>
            <p className="text-center text-sm text-ink-soft">
              已有账户？
              <Link href={loginHref} className="font-medium text-ink underline decoration-line underline-offset-4 hover:decoration-ink">
                登录
              </Link>
            </p>
          </form>
        ) : checking ? (
          <p className="text-sm text-ink-soft">正在确认邀请码…</p>
        ) : codeError ? (
          <div className="space-y-4">
            <Notice>{codeError}</Notice>
            <form onSubmit={enterCode} className="space-y-4">
              <Field label="邀请码" name="code" autoComplete="off" autoFocus required defaultValue={code} placeholder="K7QF-9M2X-PW3D" />
              <Button type="submit" className="w-full">重试</Button>
            </form>
            {mode === "open" && (
              <Button variant="ghost" className="w-full" onClick={clearCode}>
                不使用邀请码，直接注册
              </Button>
            )}
            <Link href={loginHref} className="inline-block text-sm font-semibold underline decoration-line underline-offset-4 hover:decoration-ink">
              返回登录
            </Link>
          </div>
        ) : (
          <form onSubmit={submit} className="space-y-4">
            <fieldset disabled={issued} className="min-w-0 space-y-4">
              {error && <Notice>{error}</Notice>}
              {code && (
                <p className="rounded-[10px] border border-line bg-desk/50 px-3.5 py-2.5 text-[0.8125rem] leading-5 text-ink-soft">
                  邀请码 <span className="font-mono text-ink">{code}</span>
                  {invite?.email ? ` · 已为你保留 ${invite.email}` : " · 未绑定邮箱，不会自动标为已确认"}
                  {session.inviteVerifyEmail ? " · 注册后需要验证邮箱" : ""}
                </p>
              )}
              <Field
                label="邮箱"
                name="email"
                type="email"
                autoComplete="username"
                required
                autoFocus
                readOnly={!!invite?.email}
                defaultValue={invite?.email ?? ""}
              />
              <Field label="名称" name="name" autoComplete="name" maxLength={64} hint="选填。登录其他应用时显示这个名称。" />
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="密码" name="password" type="password" autoComplete="new-password" minLength={8} required hint="至少 8 个字符。" />
                <Field label="确认密码" name="confirm" type="password" autoComplete="new-password" minLength={8} required />
              </div>
              <Button type="submit" className="w-full" busy={busy}>
                创建账户
              </Button>
            </fieldset>
            <p className="text-center text-sm text-ink-soft">
              已有账户？
              <Link href={loginHref} className="font-medium text-ink underline decoration-line underline-offset-4 hover:decoration-ink">
                登录
              </Link>
            </p>
          </form>
        )}
      </Pass>
    </Centered>
  );
}
