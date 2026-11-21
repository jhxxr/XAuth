import { useEffect, useMemo, useState, type ReactNode } from "react";
import type { TotpEntry } from "../api";
import { Icon } from "../components/Icon";
import { Badge } from "../components/ui";
import { codeDeadline, codeSecondsLeft } from "../totp";

/** Keep 6–8 digit codes in two balanced groups. */
export function formatCode(code: string) {
  const middle = Math.floor(code.length / 2);
  return `${code.slice(0, middle)} ${code.slice(middle)}`.trim();
}

/** Seconds left drawn as a ring that empties, turning red in the last five seconds. */
function Countdown({ left, period }: { left: number; period: number }) {
  const low = left <= 5;
  const r = 15;
  const c = 2 * Math.PI * r;
  return (
    <div className="relative grid size-11 shrink-0 place-items-center" aria-label={left > 0 ? `${left} 秒后刷新` : "验证码已过期，等待刷新"}>
      <svg viewBox="0 0 36 36" className="absolute inset-0 -rotate-90" aria-hidden="true">
        <circle cx="18" cy="18" r={r} fill="none" className="stroke-line" strokeWidth="2.5" />
        <circle
          cx="18"
          cy="18"
          r={r}
          fill="none"
          className={`transition-[stroke-dashoffset] duration-1000 ease-linear ${low ? "stroke-void" : "stroke-stamp"}`}
          strokeWidth="2.5"
          strokeLinecap="round"
          strokeDasharray={c}
          strokeDashoffset={c * (1 - left / period)}
        />
      </svg>
      <span className={`text-[0.75rem] font-semibold tabular-nums ${low ? "text-void" : "text-ink"}`}>{left}</span>
    </div>
  );
}

export function TotpCard({
  entry,
  onEdit,
  onDelete,
  footer,
}: {
  entry: TotpEntry;
  onEdit?: () => void;
  onDelete?: () => void;
  /** Extra line at the bottom, e.g. who the entry is assigned to. */
  footer?: ReactNode;
}) {
  const period = entry.period || 30;
  const deadline = useMemo(() => entry.validUntil ?? codeDeadline(entry, Date.now()), [entry]);
  const [now, setNow] = useState(Date.now);
  const left = Math.min(period, codeSecondsLeft(deadline, now));
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 200);
    return () => window.clearInterval(timer);
  }, []);

  const copy = async () => {
    if (!entry.code || codeSecondsLeft(deadline, Date.now()) === 0) return;
    try {
      await navigator.clipboard.writeText(entry.code);
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    } catch {
      setCopied(false);
    }
  };

  const low = left <= 5;
  const meta = [entry.issuer, entry.accountName].filter(Boolean).join(" · ");
  return (
    <article className="panel group flex flex-col">
      <div className="flex items-start gap-3 px-5 pt-5">
        <span aria-hidden="true" className="display grid size-9 shrink-0 place-items-center rounded-[10px] border border-line bg-white text-[0.8125rem] text-ink shadow-xs">
          {(entry.issuer || entry.name).slice(0, 1).toUpperCase()}
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <h2 className="truncate text-[0.9375rem] font-semibold">{entry.name}</h2>
            <Badge tone={entry.shared ? "stamp" : "plain"}>{entry.shared ? "共享" : "私人"}</Badge>
          </div>
          <p className="mt-0.5 truncate text-[0.8125rem] text-ink-soft">{meta || (entry.shared ? "共享验证器" : "私人验证器")}</p>
        </div>
        {(onEdit || onDelete) && (
          <div className="-mt-1 -mr-2 flex shrink-0 gap-0.5 opacity-100 transition-opacity sm:opacity-0 sm:group-focus-within:opacity-100 sm:group-hover:opacity-100">
            {onEdit && (
              <button type="button" onClick={onEdit} aria-label={`编辑 ${entry.name}`} title="编辑" className="grid size-8 place-items-center rounded-lg text-ink-soft hover:bg-ink/[0.06] hover:text-ink">
                <Icon name="edit" className="size-4" />
              </button>
            )}
            {onDelete && (
              <button type="button" onClick={onDelete} aria-label={`删除 ${entry.name}`} title="删除" className="grid size-8 place-items-center rounded-lg text-ink-soft hover:bg-void/[0.07] hover:text-void">
                <Icon name="trash" className="size-4" />
              </button>
            )}
          </div>
        )}
      </div>

      <div className="flex items-center justify-between gap-3 px-5 pt-4 pb-5">
        <button
          type="button"
          onClick={copy}
          disabled={!entry.code || left === 0}
          aria-label={left > 0 ? `复制验证码 ${entry.code ?? ""}` : "验证码已过期，等待刷新"}
          className="group/code -ml-2 flex min-w-0 items-center gap-2.5 rounded-xl px-2 py-1 text-left transition-colors hover:bg-ink/[0.04] disabled:cursor-default disabled:hover:bg-transparent"
        >
          <span className={`font-mono leading-none font-medium whitespace-nowrap tabular-nums transition-colors ${entry.digits > 6 ? "text-[1.375rem] tracking-[0.06em] sm:text-[1.5rem]" : "text-[1.875rem] tracking-[0.12em]"} ${low ? "text-void" : "text-ink"}`}>
            {entry.code && left > 0 ? formatCode(entry.code) : "··· ···"}
          </span>
          {entry.code && left > 0 && (
            <span className={`flex items-center gap-1 text-[0.75rem] font-medium transition-opacity ${copied ? "text-seal opacity-100" : "text-ink-soft opacity-0 group-hover/code:opacity-100 group-focus-visible/code:opacity-100"}`}>
              <Icon name={copied ? "check" : "copy"} className="size-3.5" />
              <span aria-live="polite" className="sr-only sm:not-sr-only">{copied ? "已复制" : "复制"}</span>
            </span>
          )}
        </button>
        <Countdown left={left} period={period} />
      </div>

      {left === 0 && <p role="status" className="px-5 pb-3 text-[0.8125rem] text-ink-soft">验证码已过期，正在刷新…</p>}

      {(entry.note || footer) && (
        <div className="mt-auto space-y-1 rounded-b-[13px] border-t border-line bg-desk/35 px-5 py-3 text-[0.8125rem] text-ink-soft">
          {entry.note && <p className="truncate" title={entry.note}>{entry.note}</p>}
          {footer}
        </div>
      )}
    </article>
  );
}
