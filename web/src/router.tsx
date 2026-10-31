import { useSyncExternalStore, type AnchorHTMLAttributes, type MouseEvent } from "react";

// A minimal history router: the app has a handful of flat routes.

const listeners = new Set<() => void>();
const notify = () => listeners.forEach((l) => l());

if (typeof window !== "undefined") {
  window.addEventListener("popstate", notify);
}

export function navigate(to: string, opts: { replace?: boolean } = {}) {
  if (opts.replace) history.replaceState(null, "", to);
  else history.pushState(null, "", to);
  window.scrollTo(0, 0);
  notify();
}

function subscribe(cb: () => void) {
  listeners.add(cb);
  return () => listeners.delete(cb);
}

const snapshot = () => location.pathname + location.search;

export function useLocation() {
  const href = useSyncExternalStore(subscribe, snapshot);
  const url = new URL(href, location.origin);
  return { path: url.pathname, query: url.searchParams };
}

/** Full-page navigation, used when leaving the web app (e.g. back to an OIDC client). */
export function leave(to: string) {
  window.location.assign(to);
}

export function Link({ href, onClick, ...rest }: AnchorHTMLAttributes<HTMLAnchorElement> & { href: string }) {
  const handle = (e: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(e);
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    navigate(href);
  };
  return <a href={href} onClick={handle} {...rest} />;
}
