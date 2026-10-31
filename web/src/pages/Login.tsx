import { useEffect, useState } from "react";
import { ApiError, api, errorMessage, type Flow, type SignInResult } from "../api";
import { mrz, Pass, Stamp } from "../components/Pass";
import { Icon } from "../components/Icon";
import { Centered } from "../components/Shell";
import { Button, Notice } from "../components/ui";
import { leave, Link, navigate, useLocation } from "../router";
import { useSession } from "../session";
import { getPasskey, supportsPasskeys } from "../webauthn";
import { ContinueCard, FlowStub, LoginForm } from "./LoginParts";

type Mark = { tone: "issued" | "void"; label: string } | null;

const clock = () => new Date().toLocaleTimeString("zh-CN", { hour12: false });

export function LoginPage() {
  const { query } = useLocation();
  const flowId = query.get("flow") ?? "";
  const { session, setUser } = useSession();
  const [flow, setFlow] = useState<Flow | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "expired">(
    query.get("error") === "flow_expired" ? "expired" : flowId ? "loading" : "ready",
  );
  const [mode, setMode] = useState<"continue" | "form">("form");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [mark, setMark] = useState<Mark>(null);

  useEffect(() => {
    if (!flowId || state !== "loading") return;
    api
      .flow(flowId)
      .then((f) => {
        setFlow(f);
        setMode(f.canContinue ? "continue" : "form");
        setState("ready");
      })
      .catch(() => setState("expired"));
  }, [flowId, state]);

  // A signed-in visitor with nothing to approve belongs on the account page.
  const user = session?.user;
  useEffect(() => {
    if (user && !flowId && state === "ready" && !mark) navigate("/account", { replace: true });
  }, [user, flowId, state, mark]);

  const finish = (r: SignInResult) => {
    if (r.redirect) {
      setMark({ tone: "issued", label: "已签发" });
      leave(r.redirect);
      return;
    }
    setUser(r.user);
    if (r.flowError) setState("expired");
    else navigate("/account", { replace: true });
  };

  const decide = async (call: () => Promise<{ redirect: string }>, tone: "issued" | "void", label: string) => {
    setBusy(true);
    setError("");
    try {
      const r = await call();
      setMark({ tone, label });
      leave(r.redirect);
    } catch (err) {
      setBusy(false);
      if (err instanceof ApiError && err.code === "login_required") setMode("form");
      else if (err instanceof ApiError && err.status === 410) setState("expired");
      else setError(errorMessage(err));
    }
  };

  if (state === "loading" || !session) {
    return <Centered>{<div className="pass h-80 animate-pulse motion-reduce:animate-none" aria-label="正在加载" />}</Centered>;
  }

  const host = new URL(session.issuer).host;
  const passProps = flow
    ? { seed: flow.app.id, eyebrow: "登录到", title: flow.app.name, mark: flow.app.name.slice(0, 1).toUpperCase(), mrzLines: mrz(flow.app.name, flow.app.id) }
    : { seed: session.issuer, eyebrow: "账户通行证", title: "XAuth", mark: "X", mrzLines: mrz(host, host.replace(/\W/g, "")) };

  if (state === "expired") {
    return (
      <Centered>
        <Pass {...passProps} level={1} stamp={<Stamp tone="void" label="已失效" detail="EXPIRED" />}>
          <p className="max-w-[30ch] text-sm leading-relaxed text-ink-soft">
            这个登录链接已过期或已经使用过。请回到原来的应用重新点击登录。
          </p>
          <Link href={user ? "/account" : "/login"} className="mt-5 inline-block text-sm font-semibold underline decoration-line underline-offset-4 hover:decoration-ink">
            {user ? "前往我的账户" : "直接登录 XAuth"}
          </Link>
        </Pass>
      </Centered>
    );
  }

  const stamp = mark && <Stamp tone={mark.tone} label={mark.label} detail={clock()} />;
  const signup = session.signupEnabled ? `/register${flowId ? `?flow=${encodeURIComponent(flowId)}` : ""}` : undefined;

  return (
    <Centered>
      <Pass {...passProps} level={1} stamp={stamp} stub={flow && <FlowStub flow={flow} />}>
        <fieldset disabled={!!mark} className="min-w-0">
          {error && (
            <div className="mb-4">
              <Notice>{error}</Notice>
            </div>
          )}
          {flow && mode === "continue" && user ? (
            <ContinueCard
              user={user}
              appName={flow.app.name}
              busy={busy}
              onContinue={() => decide(() => api.continueFlow(flow.id), "issued", "已签发")}
              onSwitch={() => setMode("form")}
            />
          ) : (
            <LoginForm
              hint={flow?.loginHint}
              submitLabel={flow ? `登录并返回 ${flow.app.name}` : "登录"}
              signupHref={signup}
              showForgot={session.mailEnabled}
              onSubmit={async (email, password) => finish(await api.login(email, password, flowId))}
            />
          )}
          {(!flow || mode === "form") && !mark && supportsPasskeys() && (
            <div className="mt-5">
              <div className="mb-4 flex items-center gap-3 text-[0.75rem] text-ink-faint" aria-hidden="true">
                <span className="h-px flex-1 bg-line" />或<span className="h-px flex-1 bg-line" />
              </div>
              <Button
                variant="secondary"
                className="w-full"
                busy={busy}
                onClick={async () => {
                  setBusy(true);
                  setError("");
                  try {
                    const begin = await api.beginPasskeyLogin();
                    const credential = await getPasskey(begin.options);
                    finish(await api.finishPasskeyLogin(begin.challengeId, credential, flowId));
                  } catch (err) {
                    if (err instanceof DOMException && err.name === "NotAllowedError") setError("没有选择通行密钥，或操作已取消。");
                    else setError(errorMessage(err));
                  } finally {
                    setBusy(false);
                  }
                }}
              >
                <Icon name="fingerprint" className="size-4" />
                使用通行密钥
              </Button>
              <p className="mt-2 text-center text-[0.8125rem] text-ink-soft">浏览器会让你选择这台设备上保存的通行密钥。</p>
            </div>
          )}
          {flow && (
            <button
              type="button"
              onClick={() => decide(() => api.denyFlow(flow.id), "void", "已取消")}
              className="mt-4 w-full rounded-lg py-2 text-sm font-medium text-ink-soft hover:text-void"
            >
              取消，返回 {flow.redirectTo}
            </button>
          )}
        </fieldset>
      </Pass>
    </Centered>
  );
}
