import { useEffect, useState } from "react";
import { api, errorMessage, type SigningKey } from "../api";
import { useToast } from "../components/Dialog";
import { AppShell } from "../components/Shell";
import { Icon, type IconName } from "../components/Icon";
import { Badge, Button, IconTile, Notice } from "../components/ui";
import { formatDate } from "../session";

export function KeysPage() {
  const toast = useToast();
  const [rows, setRows] = useState<SigningKey[] | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const active = rows?.filter((k) => !k.retiredAt).length ?? 0;

  const load = () => api.keys().then((r) => setRows(r.keys)).catch((e) => setError(errorMessage(e)));
  useEffect(() => { void load(); }, []);

  const rotate = async () => {
    setBusy(true);
    try { await api.rotateKey(); toast("新签名密钥已生成，之后签发的令牌会使用它"); await load(); }
    catch (e) { setError(errorMessage(e)); } finally { setBusy(false); }
  };
  const retire = async (key: SigningKey) => {
    if (!window.confirm("停用后，这把密钥签发的访问令牌会立即失效。")) return;
    try { await api.retireKey(key.id); toast("签名密钥已停用"); await load(); } catch (e) { setError(errorMessage(e)); }
  };

  return (
    <AppShell
      title="签名密钥"
      description="新令牌使用最新的密钥签名。旧密钥在停用前仍能验证它签发过的令牌。至少要保留一把可用密钥。"
      actions={<Button busy={busy} onClick={rotate}>{!busy && <Icon name="refresh" className="size-4" />}生成新密钥</Button>}
    >
      {error && <div className="mb-4"><Notice>{error}</Notice></div>}
      <div className="mb-5 grid gap-3 sm:grid-cols-3">
        {[
          ["可用密钥", active, "key"],
          ["已停用", (rows?.length ?? 0) - active, "history"],
          ["算法", "RS256", "shield"],
        ].map(([label, value, icon]) => (
          <div key={label as string} className="panel flex items-center gap-3.5 p-4">
            <IconTile name={icon as IconName} />
            <div>
              <p className="eyebrow">{label}</p>
              <p className="mt-0.5 text-lg leading-6 font-semibold tabular-nums">{value}</p>
            </div>
          </div>
        ))}
      </div>
      <div className="panel overflow-hidden">
        <div className="divide-y divide-line/80">
          {rows?.map((key) => (
            <div key={key.id} className={`flex flex-wrap items-center gap-x-4 gap-y-3 px-5 py-4 ${key.retiredAt ? "bg-desk/30" : ""}`}>
              <IconTile name="key" tone={key.current ? "stamp" : "plain"} />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <p className={`truncate font-mono text-[0.8125rem] ${key.retiredAt ? "text-ink-soft line-through decoration-line-strong" : ""}`}>{key.id}</p>
                  {key.retiredAt ? <Badge tone="void">已停用</Badge> : key.current ? <Badge tone="seal" dot>当前签名密钥</Badge> : <Badge>仍可验证旧令牌</Badge>}
                </div>
                <p className="mt-1 text-[0.8125rem] text-ink-soft">创建于 {formatDate(key.createdAt)}{key.retiredAt ? ` · 停用于 ${formatDate(key.retiredAt)}` : ""}</p>
              </div>
              {!key.retiredAt && <Button variant="danger" size="sm" disabled={active < 2} title={active < 2 ? "至少要保留一把可用的签名密钥" : undefined} onClick={() => retire(key)}>停用</Button>}
            </div>
          ))}
        </div>
      </div>
    </AppShell>
  );
}
