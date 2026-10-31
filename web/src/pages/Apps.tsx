import { useEffect, useState } from "react";
import { api, errorMessage, type App, type Endpoints } from "../api";
import { useToast } from "../components/Dialog";
import { AppShell } from "../components/Shell";
import { Icon } from "../components/Icon";
import { Badge, Button, Copyable, IconTile, Notice } from "../components/ui";
import { appTypes, CreateAppDialog, SecretDialog } from "./AppDialogs";
import { EditAppDialog } from "./EditApp";

function EndpointsPanel({ e }: { e: Endpoints }) {
  const rows: [string, string][] = [
    ["Issuer", e.issuer],
    ["Discovery", e.discovery],
    ["Authorization", e.authorization],
    ["Token", e.token],
    ["Userinfo", e.userinfo],
    ["JWKS", e.jwks],
    ["End session", e.endSession],
    ["Revocation", e.revocation],
  ];
  return (
    <section className="panel p-5 sm:p-6" aria-labelledby="endpoints">
      <div className="flex items-start gap-3.5">
        <IconTile name="shield" tone="stamp" />
        <div className="min-w-0">
          <h2 id="endpoints" className="text-[0.9375rem] leading-6 font-semibold">
            接入信息
          </h2>
          <p className="mt-0.5 text-[0.8125rem] leading-5 text-ink-soft">大多数应用只需要填写 Issuer 或 Discovery 地址。所有应用都必须使用 PKCE（S256）。</p>
        </div>
      </div>
      <dl className="mt-5 space-y-3">
        {rows.map(([k, v]) => (
          <div key={k} className="space-y-1">
            <dt className="eyebrow">{k}</dt>
            <dd className="min-w-0">
              <Copyable value={v} label={k} />
            </dd>
          </div>
        ))}
      </dl>
    </section>
  );
}

export function AppsPage() {
  const toast = useToast();
  const [apps, setApps] = useState<App[] | null>(null);
  const [endpoints, setEndpoints] = useState<Endpoints | null>(null);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<App | null>(null);
  const [secret, setSecret] = useState<{ app: App; value: string } | null>(null);

  useEffect(() => {
    api
      .apps()
      .then((r) => {
        setApps(r.apps);
        setEndpoints(r.endpoints);
      })
      .catch((e) => setError(errorMessage(e)));
  }, []);

  const upsert = (a: App) => setApps((prev) => {
    const list = prev ?? [];
    return list.some((x) => x.id === a.id) ? list.map((x) => (x.id === a.id ? a : x)) : [...list, a];
  });

  return (
    <AppShell
      title="应用"
      description="接入 XAuth 的 OIDC 客户端。每个应用都有独立的客户端 ID 与回调地址。"
      actions={<Button onClick={() => setCreating(true)}><Icon name="plus" className="size-4" />添加应用</Button>}
    >
      {error && <div className="mb-4"><Notice>{error}</Notice></div>}
      <div className="grid gap-6 lg:grid-cols-[1fr_minmax(0,380px)] lg:items-start">
        <div className="space-y-3">
          {apps?.length === 0 && (
            <div className="panel flex flex-col items-center p-10 text-center">
              <IconTile name="apps" />
              <p className="mt-3 font-medium">还没有应用</p>
              <p className="mt-1 text-sm text-ink-soft">添加一个应用，它就能通过 XAuth 让用户登录。</p>
            </div>
          )}
          {apps?.map((a) => (
            <article key={a.id} className="panel flex items-start gap-4 p-4 transition-[border-color,box-shadow] hover:border-line-strong sm:p-5">
              <span aria-hidden="true" className="display grid size-10 shrink-0 place-items-center rounded-xl bg-gradient-to-br from-[#2b3664] to-ink text-sm text-paper shadow-xs">
                {a.name.slice(0, 1).toUpperCase()}
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <h2 className="font-semibold">{a.name}</h2>
                  <Badge>{appTypes[a.type].label}</Badge>
                  <Badge tone={a.access === "all" ? "plain" : "stamp"}>{a.access === "admins" ? "仅管理员" : a.access === "selected" ? "指定用户" : "所有用户"}</Badge>
                </div>
                <div className="mt-2.5 max-w-md">
                  <Copyable value={a.id} label={`${a.name} 的客户端 ID`} />
                </div>
                <p className="mt-2 truncate font-mono text-[0.71875rem] text-ink-soft" title={a.redirectUris.join("\n")}>
                  <span className="text-ink-faint">↳ </span>
                  {a.redirectUris[0]}
                  {a.redirectUris.length > 1 && ` 等 ${a.redirectUris.length} 个回调地址`}
                </p>
              </div>
              <Button variant="secondary" size="sm" onClick={() => setEditing(a)} aria-label={`编辑 ${a.name}`}>
                编辑
              </Button>
            </article>
          ))}
        </div>
        {endpoints && <EndpointsPanel e={endpoints} />}
      </div>

      <CreateAppDialog
        open={creating}
        onClose={() => setCreating(false)}
        onCreated={(a, s) => {
          upsert(a);
          setCreating(false);
          if (s) setSecret({ app: a, value: s });
          else toast(`已添加 ${a.name}`);
        }}
      />
      <EditAppDialog
        app={editing}
        onClose={() => setEditing(null)}
        onSaved={(a) => {
          upsert(a);
          setEditing(null);
        }}
        onDeleted={(id) => {
          setApps((prev) => (prev ?? []).filter((x) => x.id !== id));
          setEditing(null);
        }}
        onSecret={(a, s) => {
          setEditing(null);
          setSecret({ app: a, value: s });
        }}
      />
      <SecretDialog app={secret?.app ?? null} secret={secret?.value ?? ""} onClose={() => setSecret(null)} />
    </AppShell>
  );
}
