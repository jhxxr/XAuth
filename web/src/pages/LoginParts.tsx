import { useState, type FormEvent } from "react";
import { errorMessage, type Flow, type User } from "../api";
import { Icon } from "../components/Icon";
import { Avatar, Button, Field, Notice } from "../components/ui";
import { Link } from "../router";
import { displayName } from "../session";

const scopeLabels: Record<string, string> = {
  openid: "你的账户标识",
  profile: "名称",
  email: "邮箱地址",
  offline_access: "在你离开后保持登录",
};

/** The tear-off stub: what the app receives and where the browser goes next. */
export function FlowStub({ flow }: { flow: Flow }) {
  const items = flow.scopes.filter((s) => scopeLabels[s]);
  return (
    <div className="grid gap-4 text-sm sm:grid-cols-[1fr_auto] sm:items-end">
      <div>
        <p className="eyebrow">
          <span className="normal-case">{flow.app.name}</span> 将获得
        </p>
        <ul className="mt-2.5 space-y-1.5">
          {items.map((s) => (
            <li key={s} className="flex items-center gap-2">
              <span aria-hidden="true" className="grid size-4 place-items-center rounded-full bg-seal/10 text-seal">
                <Icon name="check" className="size-2.5" />
              </span>
              {scopeLabels[s]}
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
      <Button className="mt-6 w-full" busy={busy} onClick={onContinue}>
        以此身份继续登录 {appName}
      </Button>
      <button type="button" onClick={onSwitch} className="mt-3 w-full rounded-lg py-2 text-sm font-medium text-ink-soft hover:text-ink">
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
      <Button type="submit" className="w-full" busy={busy}>
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
