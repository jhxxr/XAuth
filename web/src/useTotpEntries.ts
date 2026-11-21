import { useCallback, useEffect, useRef, useState } from "react";
import { errorMessage, type TotpEntry } from "./api";
import { codeDeadline } from "./totp";

/** One refresh clock for both vaults; expired codes stay expired on failure. */
export function useTotpEntries(fetchEntries: () => Promise<{ entries: TotpEntry[] }>) {
  const [rows, setRows] = useState<TotpEntry[] | null>(null);
  const [error, setError] = useState("");
  const [clockSkewSeconds, setClockSkewSeconds] = useState(0);
  const [refreshAt, setRefreshAt] = useState<number | null>(null);
  const generation = useRef(0);

  const load = useCallback(async () => {
    const current = ++generation.current;
    const startedAt = Date.now();
    try {
      const result = await fetchEntries();
      if (current !== generation.current) return;
      const serverTime = result.entries[0]?.generatedAt;
      setClockSkewSeconds(serverTime === undefined ? 0 : Math.round((serverTime - (startedAt + Date.now()) / 2) / 1000));
      const entries = result.entries.map((entry) => ({ ...entry, validUntil: codeDeadline(entry, startedAt) }));
      setRows(entries);
      setError("");
      // Retry already-expired responses without spinning on a slow connection.
      setRefreshAt(entries.length ? Math.max(Date.now() + 100, Math.min(...entries.map((entry) => entry.validUntil))) : null);
    } catch (cause) {
      if (current !== generation.current) return;
      setError(errorMessage(cause));
      setRefreshAt(Date.now() + 3000);
    }
  }, [fetchEntries]);

  useEffect(() => {
    void load();
    const resume = () => {
      if (document.visibilityState === "visible") void load();
    };
    window.addEventListener("focus", resume);
    window.addEventListener("online", resume);
    document.addEventListener("visibilitychange", resume);
    return () => {
      generation.current++;
      window.removeEventListener("focus", resume);
      window.removeEventListener("online", resume);
      document.removeEventListener("visibilitychange", resume);
    };
  }, [load]);

  useEffect(() => {
    if (refreshAt === null) return;
    const timer = window.setTimeout(() => { void load(); }, Math.max(0, refreshAt - Date.now()));
    return () => window.clearTimeout(timer);
  }, [refreshAt, load]);

  const clockWarning = Math.abs(clockSkewSeconds) >= 10
    ? `服务器与此设备的系统时间相差约 ${Math.abs(clockSkewSeconds)} 秒，可能导致验证码被拒绝。请核对两端时间并开启自动校时。`
    : "";

  return { rows, error, clockWarning, setError, load };
}
