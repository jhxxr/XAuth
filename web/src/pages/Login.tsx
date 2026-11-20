import { useEffect, useState } from "react";
import { ApiError, api, errorMessage, type Flow, type SignInResult } from "../api";
import { mrz, Pass, Stamp } from "../components/Pass";
import { Icon } from "../components/Icon";
import { Centered } from "../components/Shell";
import { Button, Notice } from "../components/ui";
import { leave, Link, navigate, useLocation } from "../router";
import { useSession } from "../session";
import { getPasskey, supportsPasskeys } from "../webauthn";
import { ConsentCard, ContinueCard, FlowStub, LoginForm } from "./LoginParts";

type Mark = { tone: "issued" | "void"; label: string } | null;
type LoginState = "loading" | "ready" | "expired" | "denied" | "error";

const clock = () => new Date().toLocaleTimeString("zh-CN", { hour12: false });

function stateFromRedirectError(code: string | null): LoginState {
  if (code === "flow_expired") return "expired";
  if (code === "access_denied") return "denied";
  if (code) return "error";
  return "ready";
}

function messageFromRedirectError(code: string | null) {
  if (code === "access_denied") return "这次登录授权未完成，该应用没有获得访问权限。";
  if (code === "flow_expired") return "这个登录请求已过期或已使用，请回到原来的应用重新登录。";
  return code ? "无法完成这次登录请求，请返回原来的应用后重试。" : "";
}

export function LoginPage() {
  const { query } = useLocation();
  const flowId = query.get("flow") ?? "";
  const { session, setUser } = useSession();
  const [flow, setFlow] = useState<Flow | null>(null);
  const [state, setState] = useState<LoginState>(() => {
    const redirected = stateFromRedirectError(query.get("error"));
    return redirected === "ready" && flowId ? "loading" : redirected;
  });
  const [mode, setMode] = useState<"continue" | "consent" | "form">("form");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(() => messageFromRedirectError(query.get("error")));
  const [mark, setMark] = useState<Mark>(null);

  useEffect(() => {
    if (!flowId || state !== "loading") return;
    let active = true;
    api
      .flow(flowId)
      .then((f) => {
        if (!active) return;
        setFlow(f);
        setMode(f.canContinue ? (f.requiresConsent ? "consent" : "continue") : "form");
        setError("");
        setState("ready");
      })
      .catch((err) => {
        if (!active) return;
        const expired = err instanceof ApiError && (err.status === 410 || err.code === "flow_expired");
        setError(expired ? "" : errorMessage(err));
        setState(expired ? "expired" : err instanceof ApiError && err.code === "access_denied" ? "denied" : "error");
      });
    return () => { active = false; };
  }, [flowId, state]);

  const retryFlow = () => {
    setError("");
    setFlow(null);
    setState(flowId ? "loading" : "ready");
  };

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
    if (r.flowError === "flow_expired") setState("expired");
    else if (r.flowError === "access_denied") setState("denied");
    else if (r.flowError) {
      setError("登录已完成，但无法继续处理此应用的请求。请重新打开登录请求。");
      setState("error");
    } else if (flowId && r.requiresConsent) setMode("consent");
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
      if (err instanceof ApiError && (err.code === "login_required" || err.status === 401)) setMode("form");
      else if (err instanceof ApiError && err.status === 410) setState("expired");
      else if (err instanceof ApiError && err.code === "access_denied") setState("denied");
      else setError(errorMessage(err));
    }
  };

  if (state === "loading" || !session) {
    return (
      <Centered>
        <div role="status" aria-live="polite" className="pass grid h-80 place-items-center animate-pulse text-sm text-ink-soft motion-reduce:animate-none">
          正在验证登录请求…
        </div>
      </Centered>
    );
  }

  const host = new URL(session.issuer).host;
  const passProps = flow
    ? { seed: flow.app.id, eyebrow: mode === "consent" ? "授权给" : "登录到", title: flow.app.name, mark: flow.app.name.slice(0, 1).toUpperCase(), mrzLines: mrz(flow.app.name, flow.app.id) }
    : { seed: session.issuer, eyebrow: "账户通行证", title: "XAuth", mark: "X", mrzLines: mrz(host, host.replace(/\W/g, "")) };

  if (state === "expired" || state === "denied" || state === "error") {
    const expired = state === "expired";
    const denied = state === "denied";
    const outcome = expired ? "已失效" : denied ? "未授权" : "暂不可用";
    return (
      <Centered>
        <Pass {...passProps} level={1}>
          <p className="mb-2 font-semibold">{outcome}</p>
          <p role="alert" className="max-w-[34ch] text-sm leading-relaxed text-ink-soft">
            {expired ? "这个登录请求已过期或已使用，请回到原来的应用重新登录。" : denied ? "这次登录授权未完成，该应用没有获得访问权限。" : error || "无法加载登录请求，请检查网络后重试。"}
          </p>
          {state === "error" && flowId && (
            <Button variant="secondary" className="mt-5 w-full" onClick={retryFlow}>重试获取登录请求</Button>
          )}
          <Link href={user ? "/account" : "/login"} className="mt-5 inline-block text-sm font-semibold underline decoration-line underline-offset-4 hover:decoration-ink">
            {user ? "前往我的账户" : "直接登录 XAuth"}
          </Link>
        </Pass>
      </Centered>
    );
  }

  const stamp = mark && <Stamp tone={mark.tone} label={mark.label} detail={clock()} />;
  const signup = session.registrationMode !== "closed" ? `/register${flowId ? `?flow=${encodeURIComponent(flowId)}` : ""}` : undefined;

  return (
    <Centered>
      <Pass {...passProps} level={1} stamp={stamp} stub={flow && mode !== "consent" && <FlowStub flow={flow} />}>
        <fieldset disabled={!!mark || busy} className="min-w-0">
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
          ) : flow && mode === "consent" && user ? (
            <ConsentCard
              user={user}
              flow={flow}
              busy={busy}
              onApprove={() => decide(() => api.continueFlow(flow.id), "issued", "已授权")}
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
              className="mt-4 min-h-11 w-full rounded-lg px-3 py-2 text-sm font-medium text-ink-soft hover:bg-void/[0.05] hover:text-void"
            >
              {mode === "consent" ? "拒绝授权并返回" : "取消，返回"} {flow.redirectTo}
            </button>
          )}
        </fieldset>
      </Pass>
    </Centered>
  );
}
