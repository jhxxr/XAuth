import { useMemo } from "react";

// Security-printing patterns generated from a seed (an app's client ID or a
// user ID). The same app always gets the same pattern, so a user who signs
// in to it often learns what its pass looks like.

/** FNV-1a hash of a string. */
export function seedFrom(text: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

function mulberry32(seed: number) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const TAU = Math.PI * 2;
const intIn = (rnd: () => number, lo: number, hi: number) => lo + Math.floor(rnd() * (hi - lo + 1));

type Line = { d: string; tone: 0 | 1 };

function path(points: [number, number][], close = false) {
  const d = points.map(([x, y], i) => `${i ? "L" : "M"}${x.toFixed(1)} ${y.toFixed(1)}`).join("");
  return close ? d + "Z" : d;
}

/** Interlaced sine ribbons whose amplitude breathes along the band. */
function band(seed: number, w: number, h: number): Line[] {
  const rnd = mulberry32(seed);
  const cy = h / 2;
  const families = [
    { n: intIn(rnd, 12, 16), f: intIn(rnd, 4, 7), env: intIn(rnd, 1, 3), a: h * 0.34, wobble: h * 0.1, tone: 0 as const },
    { n: intIn(rnd, 8, 11), f: intIn(rnd, 9, 14), env: intIn(rnd, 2, 4), a: h * 0.2, wobble: h * 0.08, tone: 1 as const },
  ];
  const lines: Line[] = [];
  for (const fam of families) {
    const psi = rnd() * TAU;
    for (let k = 0; k < fam.n; k++) {
      const phi = (k / fam.n) * TAU;
      const pts: [number, number][] = [];
      for (let x = 0; x <= w; x += 3) {
        const t = x / w;
        const amp = fam.a + fam.wobble * Math.sin(TAU * fam.env * t + psi);
        pts.push([x, cy + amp * Math.sin(TAU * fam.f * t + phi)]);
      }
      lines.push({ d: path(pts), tone: fam.tone });
    }
  }
  return lines;
}

/** Rotated copies of lobed rings, like the seal on a banknote. */
function rosette(seed: number, size: number): Line[] {
  const rnd = mulberry32(seed ^ 0x9e3779b9);
  const c = size / 2;
  const rings = [
    { R: size * 0.42, a: size * 0.055, k: intIn(rnd, 9, 14), copies: 5, tone: 0 as const },
    { R: size * 0.3, a: size * 0.045, k: intIn(rnd, 6, 9), copies: 4, tone: 1 as const },
  ];
  const lines: Line[] = [];
  for (const ring of rings) {
    for (let j = 0; j < ring.copies; j++) {
      const rot = (j * TAU) / (ring.k * ring.copies);
      const pts: [number, number][] = [];
      for (let i = 0; i < 240; i++) {
        const th = (i / 240) * TAU;
        const r = ring.R + ring.a * Math.cos(ring.k * th);
        pts.push([c + r * Math.cos(th + rot), c + r * Math.sin(th + rot)]);
      }
      lines.push({ d: path(pts, true), tone: ring.tone });
    }
  }
  return lines;
}

function Strokes({ lines, width, opacity }: { lines: Line[]; width: number; opacity: [number, number] }) {
  return (
    <>
      {lines.map((l, i) => (
        <path
          key={i}
          d={l.d}
          pathLength={1}
          className={l.tone ? "stroke-stamp" : "stroke-ink"}
          strokeOpacity={opacity[l.tone]}
          strokeWidth={width}
          vectorEffect="non-scaling-stroke"
          style={{ animationDelay: `${i * 35}ms` }}
        />
      ))}
    </>
  );
}

export function GuillocheBand({ seed, className = "" }: { seed: number; className?: string }) {
  const lines = useMemo(() => band(seed, 480, 72), [seed]);
  return (
    <svg
      viewBox="0 0 480 72"
      preserveAspectRatio="none"
      fill="none"
      aria-hidden="true"
      className={`engrave ${className}`}
      // Fade the engraving out under the title so the name stays crisp.
      style={{ maskImage: "linear-gradient(90deg, rgb(0 0 0 / 0.18) 0%, rgb(0 0 0 / 0.45) 35%, #000 75%)" }}
    >
      <Strokes lines={lines} width={0.6} opacity={[0.26, 0.3]} />
    </svg>
  );
}

export function Rosette({ seed, mark, className = "size-16" }: { seed: number; mark: string; className?: string }) {
  const lines = useMemo(() => rosette(seed, 100), [seed]);
  return (
    <div className={`relative shrink-0 ${className}`} aria-hidden="true">
      <svg viewBox="0 0 100 100" fill="none" className="engrave absolute inset-0 size-full">
        <circle cx="50" cy="50" r="49" className="fill-paper" fillOpacity={0.85} />
        <Strokes lines={lines} width={0.6} opacity={[0.42, 0.38]} />
        <circle cx="50" cy="50" r="18" className="fill-paper stroke-ink" strokeOpacity={0.3} strokeWidth={0.8} />
        <circle cx="50" cy="50" r="15.5" className="stroke-ink" fill="none" strokeOpacity={0.12} strokeWidth={0.6} />
      </svg>
      <span className="display absolute inset-0 grid place-items-center text-[0.875rem]">{mark}</span>
    </div>
  );
}
