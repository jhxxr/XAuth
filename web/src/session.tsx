import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { api, type Session, type User } from "./api";

type SessionState = {
  session: Session | null;
  error: string | null;
  setUser: (u: User | null) => void;
  reload: () => Promise<void>;
};

const Ctx = createContext<SessionState | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<Session | null>(null);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(async () => {
    try {
      setSession(await api.session());
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  const setUser = useCallback((user: User | null) => {
    setSession((s) => (s ? { ...s, user } : s));
  }, []);

  return <Ctx.Provider value={{ session, error, setUser, reload }}>{children}</Ctx.Provider>;
}

export function useSession() {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useSession outside SessionProvider");
  return ctx;
}

export const displayName = (u: User) => u.name || u.email;

const dateFmt = new Intl.DateTimeFormat("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit" });
export const formatDate = (iso: string) => dateFmt.format(new Date(iso));
