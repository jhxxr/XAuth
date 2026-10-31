import { useState, type FormEvent } from "react";
import { api, errorMessage } from "../api";
import { mrz, Pass, Stamp } from "../components/Pass";
import { Centered } from "../components/Shell";
import { Button, Field, Notice } from "../components/ui";
import { Link, navigate, useLocation } from "../router";
import { useSession } from "../session";

export function ResetPasswordPage() {
  const { query } = useLocation();
  const token = query.get("token") ?? "";
  const { session } = useSession();
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState("");

  if (!session) return <Centered>{null}</Centered>;
  const host = new URL(session.issuer).host;

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    const next = String(data.get("password"));
    if (next !== String(data.get("confirm"))) {
      setError("两次输入的密码不一致。");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await api.resetPassword(token, next);
      setDone(true);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Centered>
      <Pass
        seed={session.issuer}
        eyebrow="重置"
        title="设置新密码"
        mark="X"
        level={1}
        mrzLines={mrz(host, host.replace(/\W/g, ""))}
        stamp={done && <Stamp tone="issued" label="已完成" detail="RESET" />}
      >
        {done ? (
          <div className="space-y-4">
            <p className="text-sm leading-relaxed text-ink-soft">密码已更新，其他设备上的登录已退出。这台浏览器已经登录。</p>
            <Button className="w-full" onClick={() => navigate("/account", { replace: true })}>前往我的账户</Button>
          </div>
        ) : token ? (
          <form onSubmit={submit} className="space-y-4">
            {error && <Notice>{error}</Notice>}
            <Field label="新密码" name="password" type="password" autoComplete="new-password" minLength={8} required autoFocus hint="至少 8 个字符。" />
            <Field label="确认新密码" name="confirm" type="password" autoComplete="new-password" minLength={8} required />
            <Button type="submit" className="w-full" busy={busy}>设置新密码</Button>
          </form>
        ) : (
          <div className="space-y-4">
            <Notice>这个链接不完整，请从邮件里重新打开，或重新申请一次。</Notice>
            <Link href="/forgot-password" className="inline-block text-sm font-semibold underline decoration-line underline-offset-4 hover:decoration-ink">重新申请</Link>
          </div>
        )}
      </Pass>
    </Centered>
  );
}
