import { useId, useState, type ButtonHTMLAttributes, type FormEvent, type InputHTMLAttributes, type ReactNode, type TextareaHTMLAttributes } from "react";
import { Icon, type IconName } from "./Icon";

type Variant = "primary" | "secondary" | "ghost" | "danger";
type Size = "sm" | "md";

const variants: Record<Variant, string> = {
  primary: "bg-ink text-paper shadow-[inset_0_1px_0_rgb(255_255_255/0.12),0_1px_2px_rgb(27_35_64/0.28)] hover:bg-[#29335c] disabled:bg-ink-soft",
  secondary: "border border-line-strong bg-white text-ink shadow-xs hover:border-[#a9aea3] hover:bg-[#fafaf7]",
  ghost: "text-ink-soft hover:bg-ink/[0.05] hover:text-ink",
  danger: "border border-void/30 bg-white text-void shadow-xs hover:border-void/55 hover:bg-void/[0.04]",
};

const sizes: Record<Size, string> = {
  sm: "h-8 rounded-lg px-3 text-[0.8125rem]",
  md: "h-10 rounded-[9px] px-4 text-[0.875rem]",
};

export function Button({
  variant = "primary",
  size = "md",
  busy,
  className = "",
  children,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; size?: Size; busy?: boolean }) {
  return (
    <button
      type="button"
      {...rest}
      disabled={rest.disabled || busy}
      aria-busy={busy || undefined}
      className={`inline-flex shrink-0 items-center justify-center gap-2 font-semibold whitespace-nowrap transition-[background-color,border-color,color,box-shadow,transform] duration-150 active:translate-y-px disabled:cursor-not-allowed disabled:opacity-60 disabled:active:translate-y-0 ${sizes[size]} ${variants[variant]} ${className}`}
    >
      {busy && <span className="size-3.5 animate-spin rounded-full border-2 border-current border-r-transparent motion-reduce:animate-none" aria-hidden="true" />}
      {children}
    </button>
  );
}

type FieldProps = InputHTMLAttributes<HTMLInputElement> & { label: string; hint?: ReactNode; error?: string };

export function Field({ label, hint, error, className = "", ...rest }: FieldProps) {
  const id = useId();
  const note = error || hint ? `${id}-note` : undefined;
  return (
    <div className={className}>
      <label htmlFor={id} className="mb-1.5 block text-[0.8125rem] font-medium text-ink">
        {label}
      </label>
      <input id={id} aria-invalid={error ? true : undefined} aria-describedby={note} className="field" {...rest} />
      {note && (
        <p id={note} className={`mt-1.5 flex items-center gap-1.5 text-[0.8125rem] ${error ? "text-void" : "text-ink-soft"}`}>
          {error && <Icon name="alert" className="size-3.5" />}
          {error || hint}
        </p>
      )}
    </div>
  );
}

type AreaProps = TextareaHTMLAttributes<HTMLTextAreaElement> & { label: string; hint?: ReactNode };

export function TextArea({ label, hint, ...rest }: AreaProps) {
  const id = useId();
  return (
    <div>
      <label htmlFor={id} className="mb-1.5 block text-[0.8125rem] font-medium text-ink">
        {label}
      </label>
      <textarea id={id} aria-describedby={hint ? `${id}-hint` : undefined} className="field min-h-24 font-mono text-[0.8125rem] leading-relaxed" {...rest} />
      {hint && (
        <p id={`${id}-hint`} className="mt-1.5 text-[0.8125rem] text-ink-soft">
          {hint}
        </p>
      )}
    </div>
  );
}

type CheckProps = { label: string; description?: string; checked: boolean; onChange: (v: boolean) => void; disabled?: boolean };

export function Check({ label, description, checked, onChange, disabled }: CheckProps) {
  return (
    <label className={`flex gap-3 ${disabled ? "opacity-55" : "cursor-pointer"}`}>
      <input
        type="checkbox"
        className="mt-0.5 size-4 shrink-0 accent-ink"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
      />
      <span>
        <span className="block text-sm font-medium">{label}</span>
        {description && <span className="mt-0.5 block text-[0.8125rem] text-ink-soft">{description}</span>}
      </span>
    </label>
  );
}

export function Notice({ tone = "error", children }: { tone?: "error" | "info"; children: ReactNode }) {
  const style = tone === "error" ? "border-void/25 bg-void/[0.05] text-void" : "border-line bg-desk/60 text-ink";
  return (
    <div role={tone === "error" ? "alert" : "status"} className={`flex items-start gap-2.5 rounded-[10px] border px-3.5 py-2.5 text-sm leading-5 ${style}`}>
      <Icon name={tone === "error" ? "alert" : "info"} className={`mt-0.5 size-4 ${tone === "error" ? "" : "text-ink-soft"}`} />
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}

/** A value with a copy button, for client IDs, secrets and endpoint URLs. */
export function Copyable({ value, label, wrap }: { value: string; label: string; wrap?: boolean }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    } catch {
      setCopied(false);
    }
  };
  return (
    <div className="flex min-w-0 items-center gap-1 rounded-lg border border-line bg-white/80 py-1 pr-1 pl-2.5">
      <code className={`min-w-0 flex-1 font-mono text-[0.71875rem] text-ink ${wrap ? "break-all" : "truncate"}`} title={value}>
        {value}
      </code>
      <button
        type="button"
        onClick={copy}
        aria-label={`复制${label}`}
        title={copied ? "已复制" : "复制"}
        className={`grid size-7 shrink-0 place-items-center rounded-md transition-colors ${copied ? "bg-seal/10 text-seal" : "text-ink-soft hover:bg-ink/[0.06] hover:text-ink"}`}
      >
        <Icon name={copied ? "check" : "copy"} className="size-3.5" />
        <span aria-live="polite" className="sr-only">
          {copied ? "已复制" : ""}
        </span>
      </button>
    </div>
  );
}

/** A small rounded square holding an icon, used to anchor section headers and list rows. */
export function IconTile({ name, tone = "plain" }: { name: IconName; tone?: "plain" | "stamp" | "seal" }) {
  const style = {
    plain: "border-line bg-white text-ink-soft",
    stamp: "border-stamp/20 bg-stamp/[0.06] text-stamp",
    seal: "border-seal/20 bg-seal/[0.07] text-seal",
  }[tone];
  return (
    <span aria-hidden="true" className={`grid size-9 shrink-0 place-items-center rounded-[10px] border shadow-xs ${style}`}>
      <Icon name={name} className="size-[1.0625rem]" />
    </span>
  );
}

type BadgeTone = "plain" | "ink" | "seal" | "void" | "stamp";

export function Badge({ children, tone = "plain", dot }: { children: ReactNode; tone?: BadgeTone; dot?: boolean }) {
  const style = {
    plain: "border-line bg-white text-ink-soft",
    ink: "border-ink/15 bg-ink/[0.06] text-ink",
    seal: "border-seal/20 bg-seal/[0.07] text-seal",
    void: "border-void/20 bg-void/[0.06] text-void",
    stamp: "border-stamp/20 bg-stamp/[0.07] text-stamp",
  }[tone];
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-[0.1875rem] text-[0.71875rem] leading-none font-medium whitespace-nowrap ${style}`}>
      {dot && <span aria-hidden="true" className="size-1.5 rounded-full bg-current" />}
      {children}
    </span>
  );
}

type ListRowProps = { icon?: IconName; title: ReactNode; meta?: ReactNode; badge?: ReactNode; action?: ReactNode };

/** One entry in a settings list: icon, title with optional badge, a muted meta line, and an action. */
export function ListRow({ icon, title, meta, badge, action }: ListRowProps) {
  return (
    <div className="flex items-center gap-3.5 py-3 first:pt-0 last:pb-0">
      {icon && <IconTile name={icon} />}
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <p className="truncate text-sm font-medium">{title}</p>
          {badge}
        </div>
        {meta && <p className="mt-0.5 truncate text-[0.8125rem] text-ink-soft">{meta}</p>}
      </div>
      {action && <div className="flex shrink-0 items-center gap-1">{action}</div>}
    </div>
  );
}

/** A quiet placeholder for an empty list. */
export function Empty({ icon = "info", children }: { icon?: IconName; children: ReactNode }) {
  return (
    <div className="flex items-center gap-3 rounded-xl border border-dashed border-line-strong bg-desk/35 px-4 py-3.5 text-[0.8125rem] leading-5 text-ink-soft">
      <Icon name={icon} className="size-4 text-ink-faint" />
      <div className="min-w-0">{children}</div>
    </div>
  );
}

/** A page-level empty state: a large panel with an icon, a title, a line of help and an optional action. */
export function EmptyState({ icon, title, description, action }: { icon: IconName; title: string; description?: ReactNode; action?: ReactNode }) {
  return (
    <div className="panel flex flex-col items-center px-6 py-14 text-center">
      <span aria-hidden="true" className="relative grid size-14 place-items-center">
        <span className="absolute inset-0 rounded-2xl border border-line bg-white shadow-xs" />
        <span className="absolute -inset-2 rounded-[1.25rem] border border-dashed border-line" />
        <Icon name={icon} className="relative size-6 text-ink-soft" />
      </span>
      <p className="mt-6 text-[0.9375rem] font-semibold">{title}</p>
      {description && <p className="mt-1.5 max-w-sm text-[0.8125rem] leading-5 text-ink-soft">{description}</p>}
      {action && <div className="mt-5">{action}</div>}
    </div>
  );
}

type SegmentedProps<T extends string> = { options: [T, string][]; value: T; onChange: (v: T) => void; label: string };

/** A pill switcher for filters and views. */
export function Segmented<T extends string>({ options, value, onChange, label }: SegmentedProps<T>) {
  return (
    <div role="group" aria-label={label} className="inline-flex max-w-full gap-0.5 overflow-x-auto rounded-[11px] border border-line bg-desk/70 p-[3px] [scrollbar-width:none]">
      {options.map(([v, text]) => (
        <button
          key={v}
          type="button"
          aria-pressed={value === v}
          onClick={() => onChange(v)}
          className="h-7 shrink-0 rounded-lg px-3 text-[0.8125rem] font-medium whitespace-nowrap text-ink-soft transition-colors hover:text-ink aria-pressed:bg-white aria-pressed:text-ink aria-pressed:shadow-[0_0_0_1px_var(--color-line),0_1px_2px_rgb(27_35_64/0.08)]"
        >
          {text}
        </button>
      ))}
    </div>
  );
}

/** A search input with a leading icon. */
export function SearchField({ value, onChange, placeholder, label, className = "" }: { value: string; onChange: (v: string) => void; placeholder: string; label: string; className?: string }) {
  return (
    <div className={`relative ${className}`}>
      <Icon name="search" className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-ink-faint" />
      <input type="search" value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} aria-label={label} className="field !h-9 !pl-9 text-sm" />
    </div>
  );
}

type SectionProps = {
  title: string;
  description?: string;
  icon?: IconName;
  /** Rendered at the right of the header, e.g. a primary action for the section. */
  action?: ReactNode;
  /** Rendered in a tinted strip at the bottom; holds the form's submit button. */
  footer?: ReactNode;
  /** When set, body and footer are wrapped in a <form> so the footer button can submit it. */
  onSubmit?: (e: FormEvent<HTMLFormElement>) => void;
  children: ReactNode;
};

export function Section({ title, description, icon, action, footer, onSubmit, children }: SectionProps) {
  const body = (
    <>
      <div className="px-5 pt-4 pb-5 sm:px-6 sm:pb-6">{children}</div>
      {footer && <div className="flex flex-wrap items-center justify-end gap-3 rounded-b-[13px] border-t border-line bg-desk/40 px-5 py-3 sm:px-6">{footer}</div>}
    </>
  );
  return (
    <section className="panel">
      <header className="flex items-start gap-3.5 px-5 pt-5 sm:px-6 sm:pt-6">
        {icon && <IconTile name={icon} />}
        <div className="min-w-0 flex-1">
          <h2 className="text-[0.9375rem] leading-6 font-semibold">{title}</h2>
          {description && <p className="mt-0.5 text-[0.8125rem] leading-5 text-ink-soft">{description}</p>}
        </div>
        {action && <div className="shrink-0">{action}</div>}
      </header>
      {onSubmit ? (
        <form onSubmit={onSubmit}>{body}</form>
      ) : (
        body
      )}
    </section>
  );
}

/** A round monogram for a person or an app. */
export function Avatar({ text, size = "md", tone = "ink" }: { text: string; size?: "sm" | "md" | "lg"; tone?: "ink" | "plain" }) {
  const s = { sm: "size-7 text-[0.6875rem]", md: "size-8 text-[0.75rem]", lg: "size-10 text-sm" }[size];
  const t = tone === "ink" ? "bg-gradient-to-br from-[#2e3a6b] to-ink text-paper" : "border border-line bg-white text-ink-soft";
  return (
    <span aria-hidden="true" className={`display grid shrink-0 place-items-center rounded-full ${s} ${t}`}>
      {text.slice(0, 1).toUpperCase()}
    </span>
  );
}

/** The label above a group of radio cards or checkboxes. */
export function GroupLabel({ children, as: As = "legend" }: { children: ReactNode; as?: "legend" | "p" }) {
  return <As className="mb-2 block text-[0.8125rem] font-medium text-ink">{children}</As>;
}

type RadioCardProps = { name: string; checked: boolean; onChange: () => void; title: string; description?: string; icon?: IconName; disabled?: boolean };

/** A selectable card backed by a radio input. */
export function RadioCard({ name, checked, onChange, title, description, icon, disabled }: RadioCardProps) {
  return (
    <label
      className={`relative flex gap-3 rounded-xl border p-3.5 transition-[border-color,box-shadow,background-color] has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-stamp ${
        checked ? "border-stamp/50 bg-white shadow-[0_0_0_3px_rgb(90_63_192/0.1)]" : "border-line bg-white/60 hover:border-line-strong hover:bg-white"
      } ${disabled ? "cursor-not-allowed opacity-60" : "cursor-pointer"}`}
    >
      <input type="radio" name={name} className="sr-only" checked={checked} disabled={disabled} onChange={onChange} />
      <span aria-hidden="true" className={`mt-0.5 grid size-4 shrink-0 place-items-center rounded-full border transition-colors ${checked ? "border-stamp bg-stamp" : "border-line-strong bg-white"}`}>
        {checked && <span className="size-1.5 rounded-full bg-white" />}
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-1.5 text-sm font-medium">
          {icon && <Icon name={icon} className={`size-3.5 ${checked ? "text-stamp" : "text-ink-faint"}`} />}
          {title}
        </span>
        {description && <span className="mt-0.5 block text-[0.78125rem] leading-[1.125rem] text-ink-soft">{description}</span>}
      </span>
    </label>
  );
}

type ChecklistUser = { id: string; name: string; email: string };

/** A scrollable, filterable list of people with checkboxes. */
export function UserChecklist({ users, selected, onChange }: { users: ChecklistUser[]; selected: string[]; onChange: (ids: string[]) => void }) {
  const [q, setQ] = useState("");
  const shown = users.filter((u) => `${u.name} ${u.email}`.toLowerCase().includes(q.trim().toLowerCase()));
  const all = users.length > 0 && users.every((u) => selected.includes(u.id));
  return (
    <div className="overflow-hidden rounded-xl border border-line-strong bg-white shadow-xs">
      <div className="flex items-center gap-2 border-b border-line bg-desk/40 px-2.5 py-2">
        <div className="relative min-w-0 flex-1">
          <Icon name="search" className="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-ink-faint" />
          <input
            type="search"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="筛选用户"
            aria-label="筛选用户"
            className="h-7 w-full rounded-md bg-transparent pr-2 pl-7 text-[0.8125rem] outline-none placeholder:text-ink-faint focus:bg-white"
          />
        </div>
        <span className="shrink-0 text-[0.75rem] text-ink-soft tabular-nums">
          已选 {selected.length}/{users.length}
        </span>
        <button
          type="button"
          onClick={() => onChange(all ? [] : users.map((u) => u.id))}
          className="shrink-0 rounded-md px-2 py-1 text-[0.75rem] font-medium text-stamp hover:bg-stamp/[0.07]"
        >
          {all ? "清空" : "全选"}
        </button>
      </div>
      <div className="max-h-52 divide-y divide-line/70 overflow-y-auto">
        {users.length === 0 && <p className="px-3 py-4 text-center text-[0.8125rem] text-ink-soft">还没有可分配的用户。</p>}
        {users.length > 0 && shown.length === 0 && <p className="px-3 py-4 text-center text-[0.8125rem] text-ink-soft">没有匹配的用户。</p>}
        {shown.map((u) => {
          const on = selected.includes(u.id);
          return (
            <label key={u.id} className={`flex cursor-pointer items-center gap-3 px-3 py-2 transition-colors ${on ? "bg-stamp/[0.04]" : "hover:bg-desk/40"}`}>
              <input
                type="checkbox"
                className="size-4 shrink-0 accent-ink"
                checked={on}
                onChange={(e) => onChange(e.target.checked ? [...selected, u.id] : selected.filter((id) => id !== u.id))}
              />
              <Avatar text={u.name || u.email} size="sm" tone="plain" />
              <span className="min-w-0 flex-1 leading-tight">
                <span className="block truncate text-[0.8125rem] font-medium">{u.name || u.email}</span>
                {u.name && <span className="block truncate text-[0.75rem] text-ink-soft">{u.email}</span>}
              </span>
            </label>
          );
        })}
      </div>
    </div>
  );
}
