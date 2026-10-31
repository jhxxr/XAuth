import { useState } from "react";
import { api, errorMessage } from "../api";
import { mrz, Pass, Stamp } from "../components/Pass";
import { Centered } from "../components/Shell";
import { Button, Notice } from "../components/ui";
import { Link } from "../router";
import { useSession } from "../session";

/** Sign-out confirmation, also the landing page for RP-initiated logout. */
export function LogoutPage() {
  const { session, setUser } = useSession();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);

  if (!session) return <Centered>{null}</Centered>;
  const host = new URL(session.issuer).host;
  const user = session.user;
  const signedOut = done || !user;

  const signOut = async () => {
    setBusy(true);
    setError("");
    try {
      await api.logout();
      setUser(null);
      setDone(true);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const link = "inline-block text-sm font-semibold underline decoration-line underline-offset-4 hover:decoration-ink";

  return (
    <Centered>
      <Pass
        seed={session.issuer}
        eyebrow="退出"
        title="XAuth"
        mark="X"
        level={1}
        mrzLines={mrz(host, host.replace(/\W/g, ""))}
        stamp={signedOut && <Stamp tone="void" label="已注销" detail="SIGNED OUT" />}
      >
        {signedOut ? (
          <div className="space-y-5">
            <p className="max-w-[30ch] text-sm leading-relaxed text-ink-soft">
              你已退出 XAuth。已经登录的其他应用不受影响，需要在应用内单独退出。
            </p>
            <Link href="/login" className={link}>
              重新登录
            </Link>
          </div>
        ) : (
          <div className="space-y-5">
            {error && <Notice>{error}</Notice>}
            <p className="text-sm leading-relaxed">
              要在这台设备上退出 <span className="font-semibold">{user.email}</span> 吗？
            </p>
            <p className="text-sm leading-relaxed text-ink-soft">退出后，通过 XAuth 登录新应用需要重新输入密码。</p>
            <Button className="w-full" busy={busy} onClick={signOut}>
              退出 XAuth
            </Button>
            <Link href="/account" className={link}>
              留在登录状态
            </Link>
          </div>
        )}
      </Pass>
    </Centered>
  );
}
