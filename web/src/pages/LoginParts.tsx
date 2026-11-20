import { useState, type FormEvent } from "react";
import { errorMessage, type Flow, type User } from "../api";
import { Icon } from "../components/Icon";
import { Avatar, Button, Field, Notice } from "../components/ui";
import { Link } from "../router";
import { displayName } from "../session";

const scopeLabels: Record<string, string> = {
  openid: "XAuth 账户的唯一标识",
  profile: "名称与用户名（邮箱）",
  email: "邮箱地址及验证状态",
  offline_access: "申请刷新令牌以保持登录",
};

/** The tear-off stub: what the app receives and where the browser goes next. */
export function FlowStub({ flow }: { flow: Flow }) {
  return (
    <div className="grid gap-4 text-sm sm:grid-cols-[1fr_auto] sm:items-end">
      <div>
        <p className="eyebrow">
          <span className="normal-case">{flow.app.name}</span> 请求访问
        </p>
        <ul className="mt-2.5 space-y-1.5">
          {flow.scopes.map((s) => (
            <li key={s} className="flex items-center gap-2">
              <span aria-hidden="true" className="grid size-4 place-items-center rounded-full bg-seal/10 text-seal">
                <Icon name="check" className="size-2.5" />
              </span>
              {scopeLabels[s] ?? s}
            </li>
          ))}
        </ul>
      </div>
      <div className="sm:text-right">
        <p className="eyebrow">完成后前往</p>
        <p className="mt-2 font-mono text-[0.75rem] break-all">{flow.redirectTo}</p>
      </div>
    </div>
  );
}

type ContinueProps = { user: User; appName: string; busy: boolean; onContinue: () => void; onSwitch: () => void };

export function ContinueCard({ user, appName, busy, onContinue, onSwitch }: ContinueProps) {
  return (
    <div>
      <div className="flex items-center gap-3 rounded-xl border border-line bg-white p-3 shadow-xs">
        <Avatar text={displayName(user)} src={user.avatarUrl} size="lg" />
        <div className="min-w-0">
          <p className="truncate font-semibold">{displayName(user)}</p>
          <p className="truncate text-sm text-ink-soft">{user.email}</p>
        </div>
      </div>
      <Button className="mt-6 !h-auto min-h-11 w-full !whitespace-normal py-2" busy={busy} onClick={onContinue}>
        以此身份继续登录 {appName}
      </Button>
      <button type="button" onClick={onSwitch} className="mt-3 w-full rounded-lg py-2 text-sm font-medium text-ink-soft hover:text-ink">
        使用其他账户
      </button>
    </div>
  );
}

type ConsentProps = { user: User; flow: Flow; busy: boolean; onApprove: () => void; onSwitch: () => void };

export function ConsentCard({ user, flow, busy, onApprove, onSwitch }: ConsentProps) {
  return (
    <div>
      <div className="flex items-center gap-3 border-b border-line pb-4">
        <Avatar text={displayName(user)} src={user.avatarUrl} size="lg" />
        <div className="min-w-0">
          <p className="text-[0.8125rem] font-medium text-ink-soft">使用此账户授权</p>
          <p className="truncate font-semibold">{displayName(user)}</p>
          <p className="truncate text-sm text-ink-soft">{user.email}</p>
        </div>
      </div>
      <p className="mt-4 text-sm leading-6 text-ink-soft">
        同意后，{flow.app.name} 可读取你批准的资料。XAuth 不会向应用提供你的密码。
      </p>
      <section aria-label={`授权给 ${flow.app.name} 的内容`} className="mt-4 space-y-3 rounded-xl border border-line bg-desk/35 p-3.5">
        <div>
          <p className="eyebrow">应用</p>
          <p className="mt-1 break-words text-sm font-semibold">{flow.app.name}</p>
        </div>
        <div>
          <p className="eyebrow">请求的权限</p>
          <ul className="mt-1.5 space-y-1.5">
            {flow.scopes.map((scope) => (
              <li key={scope} className="flex items-start gap-2 text-[0.8125rem] leading-5 text-ink-soft">
                <span aria-hidden="true" className="mt-0.5 grid size-4 shrink-0 place-items-center rounded-full bg-seal/10 text-seal">
                  <Icon name="check" className="size-2.5" />
                </span>
                <span>{scopeLabels[scope] ?? scope}</span>
              </li>
            ))}
          </ul>
        </div>
        <div className="border-t border-line pt-2.5">
          <p className="eyebrow">授权后返回</p>
          <p className="mt-1 break-all font-mono text-xs text-ink-soft">{flow.redirectTo}</p>
        </div>
      </section>
      <Button className="mt-5 min-h-11 w-full" busy={busy} onClick={onApprove}>
        同意并继续
      </Button>
      <button
        type="button"
        onClick={onSwitch}
        className="mt-3 min-h-11 w-full rounded-lg px-3 py-2 text-sm font-medium text-ink-soft hover:bg-ink/[0.05] hover:text-ink"
      >
        使用其他账户
      </button>
    </div>
  );
}

type FormProps = {
  hint?: string;
  submitLabel: string;
  signupHref?: string;
  /** SMTP must be configured; the login page hides the link otherwise. */
  showForgot?: boolean;
  onSubmit: (email: string, password: string) => Promise<void>;
};

export function LoginForm({ hint, submitLabel, signupHref, showForgot, onSubmit }: FormProps) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    setBusy(true);
    setError("");
    try {
      await onSubmit(String(data.get("email")), String(data.get("password")));
    } catch (err) {
      setError(errorMessage(err));
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4">
      {error && <Notice>{error}</Notice>}
      <Field label="邮箱" name="email" type="email" autoComplete="username" defaultValue={hint} required autoFocus={!hint} />
      <div>
        <Field label="密码" name="password" type="password" autoComplete="current-password" required autoFocus={!!hint} />
        {showForgot && (
          <div className="mt-2 flex justify-end">
            <Link href="/forgot-password" className="text-[0.8125rem] font-medium text-ink-soft underline-offset-4 hover:text-ink hover:underline">
              忘记密码？
            </Link>
          </div>
        )}
      </div>
      <Button type="submit" className="!h-auto min-h-11 w-full !whitespace-normal py-2" busy={busy}>
        {submitLabel}
      </Button>
      {signupHref && (
        <p className="text-center text-sm text-ink-soft">
          还没有账户？
          <Link href={signupHref} className="font-medium text-ink underline decoration-line underline-offset-4 hover:decoration-ink">
            创建账户
          </Link>
        </p>
      )}
    </form>
  );
}
