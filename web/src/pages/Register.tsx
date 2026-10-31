import { useEffect, useState, type FormEvent } from "react";
import { api, errorMessage } from "../api";
import { mrz, Pass, Stamp } from "../components/Pass";
import { Centered } from "../components/Shell";
import { Button, Field, Notice } from "../components/ui";
import { leave, Link, navigate, useLocation } from "../router";
import { useSession } from "../session";

export function RegisterPage() {
  const { query } = useLocation();
  const flowId = query.get("flow") ?? "";
  const { session, setUser } = useSession();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [issued, setIssued] = useState(false);

  useEffect(() => {
    if (session?.user && !flowId && !issued) navigate("/account", { replace: true });
  }, [session?.user, flowId, issued]);

  if (!session) return <Centered>{null}</Centered>;
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
      const r = await api.register(String(data.get("email")), password, String(data.get("name")), flowId);
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
        {!session.signupEnabled ? (
          <div className="space-y-4">
            <Notice tone="info">当前不开放注册。请联系管理员为你创建账户。</Notice>
            <Link href={loginHref} className="inline-block text-sm font-semibold underline decoration-line underline-offset-4 hover:decoration-ink">
              返回登录
            </Link>
          </div>
        ) : (
          <form onSubmit={submit} className="space-y-4">
            <fieldset disabled={issued} className="min-w-0 space-y-4">
              {error && <Notice>{error}</Notice>}
              <Field label="邮箱" name="email" type="email" autoComplete="username" required autoFocus />
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
