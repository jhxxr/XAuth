import { useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import { mrz, Pass, Stamp } from "../components/Pass";
import { Centered } from "../components/Shell";
import { Button, Notice } from "../components/ui";
import { Link, navigate, useLocation } from "../router";
import { useSession } from "../session";

export function VerifyEmailPage() {
  const { query } = useLocation();
  const token = query.get("token") ?? "";
  const { session, setUser } = useSession();
  const [state, setState] = useState<"working" | "ok" | "failed">("working");
  const [error, setError] = useState("");

  useEffect(() => {
    if (!session || !token) return;
    let live = true;
    api.verifyEmail(token)
      .then(async () => {
        const fresh = await api.session();
        if (!live) return;
        setUser(fresh.user);
        setState("ok");
      })
      .catch((err) => {
        if (!live) return;
        setError(errorMessage(err));
        setState("failed");
      });
    return () => { live = false; };
  }, [session, token, setUser]);

  if (!session) return <Centered>{null}</Centered>;
  const host = new URL(session.issuer).host;
  return (
    <Centered>
      <Pass
        seed={session.issuer}
        eyebrow="确认"
        title="邮箱验证"
        mark="X"
        level={1}
        mrzLines={mrz(host, host.replace(/\W/g, ""))}
        stamp={state === "ok" ? <Stamp tone="issued" label="已确认" detail="VERIFIED" /> : state === "failed" ? <Stamp tone="void" label="已失效" detail="EXPIRED" /> : undefined}
      >
        {!token ? (
          <div className="space-y-4">
            <Notice>这个链接不完整，请从邮件里重新打开。</Notice>
            <Link href="/account" className="inline-block text-sm font-semibold underline decoration-line underline-offset-4 hover:decoration-ink">返回我的账户</Link>
          </div>
        ) : state === "working" ? (
          <p className="text-sm text-ink-soft">正在确认…</p>
        ) : state === "ok" ? (
          <div className="space-y-4">
            <p className="text-sm leading-relaxed text-ink-soft">邮箱已确认。应用现在会收到 email_verified = true。</p>
            <Button className="w-full" onClick={() => navigate("/account", { replace: true })}>前往我的账户</Button>
          </div>
        ) : (
          <div className="space-y-4">
            <Notice>{error}</Notice>
            <Link href="/account" className="inline-block text-sm font-semibold underline decoration-line underline-offset-4 hover:decoration-ink">返回我的账户重新发送</Link>
          </div>
        )}
      </Pass>
    </Centered>
  );
}
