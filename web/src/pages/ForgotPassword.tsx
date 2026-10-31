import { useState, type FormEvent } from "react";
import { api, errorMessage } from "../api";
import { mrz, Pass, Stamp } from "../components/Pass";
import { Centered } from "../components/Shell";
import { Button, Field, Notice } from "../components/ui";
import { Link } from "../router";
import { useSession } from "../session";

export function ForgotPasswordPage() {
  const { session } = useSession();
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState("");

  if (!session) return <Centered>{null}</Centered>;
  const host = new URL(session.issuer).host;

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.forgotPassword(String(new FormData(e.currentTarget).get("email")));
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
        title="找回密码"
        mark="X"
        level={1}
        mrzLines={mrz(host, host.replace(/\W/g, ""))}
        stamp={done && <Stamp tone="issued" label="已寄出" detail="CHECK MAIL" />}
      >
        {done ? (
          <div className="space-y-4">
            <p className="text-sm leading-relaxed text-ink-soft">如果这个邮箱在 XAuth 里存在，重置链接已经发出。30 分钟内有效，请检查收件箱和垃圾邮件。</p>
            <Link href="/login" className="inline-block text-sm font-semibold underline decoration-line underline-offset-4 hover:decoration-ink">返回登录</Link>
          </div>
        ) : (
          <form onSubmit={submit} className="space-y-4">
            {error && <Notice>{error}</Notice>}
            <p className="text-sm leading-relaxed text-ink-soft">输入注册邮箱，我们会发一个一次性链接用来设置新密码。</p>
            <Field label="邮箱" name="email" type="email" autoComplete="email" required autoFocus />
            <Button type="submit" className="w-full" busy={busy}>发送重置链接</Button>
            <p className="text-center text-sm text-ink-soft">
              想起来了？<Link href="/login" className="font-medium text-ink underline decoration-line underline-offset-4 hover:decoration-ink">返回登录</Link>
            </p>
          </form>
        )}
      </Pass>
    </Centered>
  );
}
