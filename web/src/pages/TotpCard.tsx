import { useEffect, useState, type ReactNode } from "react";
import type { TotpEntry } from "../api";
import { Icon } from "../components/Icon";
import { Badge } from "../components/ui";

/** Formats 6–8 digits as groups of three so a code can be read aloud. */
export function formatCode(code: string) {
  return code.replace(/(.{3})(?=.)/g, "$1 ").trim();
}

/** Seconds left drawn as a ring that empties, turning red in the last five seconds. */
function Countdown({ left, period }: { left: number; period: number }) {
  const low = left <= 5;
  const r = 15;
  const c = 2 * Math.PI * r;
  return (
    <div className="relative grid size-11 shrink-0 place-items-center" aria-live="polite" aria-label={`${left} 秒后刷新`}>
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
  const [left, setLeft] = useState(entry.remaining ?? period);
  const [copied, setCopied] = useState(false);
  useEffect(() => setLeft(entry.remaining ?? period), [entry.remaining, entry.code, period]);
  useEffect(() => {
    const timer = window.setInterval(() => setLeft((n) => (n > 1 ? n - 1 : period)), 1000);
    return () => window.clearInterval(timer);
  }, [period]);

  const copy = async () => {
    if (!entry.code) return;
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
          disabled={!entry.code}
          aria-label={`复制验证码 ${entry.code ?? ""}`}
          className="group/code -ml-2 flex min-w-0 items-center gap-2.5 rounded-xl px-2 py-1 text-left transition-colors hover:bg-ink/[0.04] disabled:cursor-default disabled:hover:bg-transparent"
        >
          <span className={`font-mono text-[1.875rem] leading-none font-medium tracking-[0.12em] tabular-nums transition-colors ${low ? "text-void" : "text-ink"}`}>
            {entry.code ? formatCode(entry.code) : "··· ···"}
          </span>
          {entry.code && (
            <span className={`flex items-center gap-1 text-[0.75rem] font-medium transition-opacity ${copied ? "text-seal opacity-100" : "text-ink-soft opacity-0 group-hover/code:opacity-100 group-focus-visible/code:opacity-100"}`}>
              <Icon name={copied ? "check" : "copy"} className="size-3.5" />
              <span aria-live="polite">{copied ? "已复制" : "复制"}</span>
            </span>
          )}
        </button>
        <Countdown left={left} period={period} />
      </div>

      {(entry.note || footer) && (
        <div className="mt-auto space-y-1 rounded-b-[13px] border-t border-line bg-desk/35 px-5 py-3 text-[0.8125rem] text-ink-soft">
          {entry.note && <p className="truncate" title={entry.note}>{entry.note}</p>}
          {footer}
        </div>
      )}
    </article>
  );
}
