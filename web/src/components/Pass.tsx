import type { ReactNode } from "react";
import { GuillocheBand, Rosette, seedFrom } from "./Guilloche";

/** Two machine-readable-zone lines, as printed at the foot of a passport. */
export function mrz(name: string, id: string): [string, string] {
  const clean = (s: string) =>
    s
      .normalize("NFKD")
      .toUpperCase()
      .replace(/[^A-Z0-9]+/g, "<")
      .replace(/^<+|<+$/g, "");
  const pad = (s: string) => (s + "<".repeat(36)).slice(0, 36);
  const digits = id.toUpperCase().replace(/[^A-Z0-9]/g, "");
  const value = (ch: string) => (/\d/.test(ch) ? Number(ch) : ch.charCodeAt(0) - 55);
  const check = [...digits.slice(0, 12)].reduce((sum, ch, i) => sum + value(ch) * [7, 3, 1][i % 3], 0) % 10;
  return [pad(`P<XAU${clean(name) || "APP"}<<`), pad(`${digits.slice(0, 12)}${check}<<OIDC<PKCE<S256`)];
}

type PassProps = {
  seed: string;
  eyebrow: string;
  title: string;
  mark: string;
  children: ReactNode;
  stub?: ReactNode;
  mrzLines?: [string, string];
  stamp?: ReactNode;
  /** Heading level of the title; the sign-in pages use the pass as their h1. */
  level?: 1 | 2;
};

/** The credential card the sign-in pages are printed on. */
export function Pass({ seed, eyebrow, title, mark, children, stub, mrzLines, stamp, level = 2 }: PassProps) {
  const s = seedFrom(seed);
  const Title = level === 1 ? "h1" : "h2";
  return (
    <article className="pass overflow-hidden">
      <header className="relative bg-[linear-gradient(180deg,#fff_0%,rgb(90_63_192/0.035)_100%)] px-6 pt-6 pb-5 sm:px-8">
        <GuillocheBand seed={s} className="pointer-events-none absolute inset-x-0 top-0 h-full w-full" />
        <div className="relative flex items-center justify-between gap-4">
          <div className="min-w-0">
            <p className="eyebrow">{eyebrow}</p>
            <Title className="display mt-1.5 truncate text-[1.375rem] leading-tight" title={title}>
              {title}
            </Title>
          </div>
          <Rosette seed={s} mark={mark} />
        </div>
      </header>
      <div className="relative border-t border-line px-6 py-6 sm:px-8">
        {children}
        {stamp && <div className="pointer-events-none absolute right-5 bottom-4">{stamp}</div>}
      </div>
      {stub && (
        <>
          <div className="perforation" aria-hidden="true" />
          <div className="px-6 py-5 sm:px-8">{stub}</div>
        </>
      )}
      {mrzLines && (
        <div
          aria-hidden="true"
          className="overflow-hidden border-t border-dashed border-line-strong bg-[#f4f5f1] px-6 py-3 font-mono text-[0.625rem] leading-[1.7] tracking-[0.12em] whitespace-pre text-ink-soft/80 [font-stretch:75%] sm:px-8"
        >
          {mrzLines[0]}
          {"\n"}
          {mrzLines[1]}
        </div>
      )}
    </article>
  );
}

type StampProps = { tone: "issued" | "void"; label: string; detail?: string };

/** An ink stamp pressed onto the pass when a sign-in is decided. */
export function Stamp({ tone, label, detail }: StampProps) {
  const color = tone === "issued" ? "border-stamp text-stamp" : "border-void text-void";
  return (
    <div role="status" className={`stamp-in rounded-md border-2 p-[3px] mix-blend-multiply ${color}`}>
      <div className="rounded-[3px] border border-current px-3 py-1 text-center">
        <p className="display text-[0.9375rem] leading-none tracking-[0.12em]">{label}</p>
        {detail && <p className="mt-1 font-mono text-[0.5625rem] leading-none tracking-wider">{detail}</p>}
      </div>
    </div>
  );
}
