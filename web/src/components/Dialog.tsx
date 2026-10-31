import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { Icon } from "./Icon";

type DialogProps = {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  footer?: ReactNode;
  /** When false, Escape does not close it (used while a secret is shown). */
  dismissible?: boolean;
  /** A short line under the title. */
  description?: string;
  /** Wider sheet for long forms. */
  wide?: boolean;
};

/** A modal built on <dialog>, which handles focus trapping and the top layer. */
export function Dialog({ open, onClose, title, children, footer, dismissible = true, description, wide }: DialogProps) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && d.open) d.close();
  }, [open]);

  return (
    <dialog
      ref={ref}
      className={`sheet ${wide ? "!w-[min(680px,calc(100vw-2rem))]" : ""}`}
      aria-labelledby="dialog-title"
      onCancel={(e) => {
        e.preventDefault();
        if (dismissible) onClose();
      }}
    >
      {open && (
        <div className="flex max-h-[calc(100dvh-2rem)] flex-col">
          <div className="flex items-start justify-between gap-4 border-b border-line px-6 py-4">
            <div className="min-w-0">
              <h2 id="dialog-title" className="truncate text-base leading-7 font-semibold tracking-tight">
                {title}
              </h2>
              {description && <p className="mt-0.5 text-[0.8125rem] text-ink-soft">{description}</p>}
            </div>
            {dismissible && (
              <button
                type="button"
                onClick={onClose}
                aria-label="关闭"
                className="-mr-2 grid size-8 place-items-center rounded-lg text-ink-soft transition-colors hover:bg-ink/[0.06] hover:text-ink"
              >
                <Icon name="x" className="size-4" />
              </button>
            )}
          </div>
          <div className="overflow-y-auto px-6 py-5">{children}</div>
          {footer && <div className="flex flex-wrap justify-end gap-2 rounded-b-[15px] border-t border-line bg-desk/40 px-6 py-3.5">{footer}</div>}
        </div>
      )}
    </dialog>
  );
}

const ToastCtx = createContext<(msg: string) => void>(() => {});

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toast, setToast] = useState<{ msg: string; key: number } | null>(null);
  const show = useCallback((msg: string) => setToast({ msg, key: Date.now() }), []);
  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(null), 3200);
    return () => clearTimeout(t);
  }, [toast]);

  return (
    <ToastCtx.Provider value={show}>
      {children}
      <div aria-live="polite" className="pointer-events-none fixed inset-x-0 bottom-5 z-50 flex justify-center px-4">
        {toast && (
          <p key={toast.key} className="toast-in flex items-center gap-2.5 rounded-xl bg-ink py-2.5 pr-4 pl-3 text-sm font-medium text-paper shadow-[0_12px_32px_-8px_rgb(27_35_64/0.5)]">
            <span aria-hidden="true" className="grid size-5 place-items-center rounded-full bg-white/15">
              <Icon name="check" className="size-3" />
            </span>
            {toast.msg}
          </p>
        )}
      </div>
    </ToastCtx.Provider>
  );
}

export const useToast = () => useContext(ToastCtx);
