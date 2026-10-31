import { useState, type FormEvent } from "react";
import { api, errorMessage, type App } from "../api";
import { Dialog, useToast } from "../components/Dialog";
import { Icon } from "../components/Icon";
import { Button, Notice } from "../components/ui";
import { AppFields, Footer, toPayload, useAppForm } from "./AppDialogs";

type Props = {
  app: App | null;
  onClose: () => void;
  onSaved: (a: App) => void;
  onDeleted: (id: string) => void;
  onSecret: (a: App, secret: string) => void;
};

export function EditAppDialog({ app, onClose, ...rest }: Props) {
  return (
    <Dialog open={!!app} onClose={onClose} wide title={app ? `编辑 ${app.name}` : ""} description={app ? `客户端 ID ${app.id}` : undefined} footer={<Footer form="edit-app" label="保存更改" onClose={onClose} />}>
      {app && <EditForm key={app.id} app={app} {...rest} />}
    </Dialog>
  );
}

function EditForm({ app, onSaved, onDeleted, onSecret }: Omit<Props, "app" | "onClose"> & { app: App }) {
  const toast = useToast();
  const { v, set } = useAppForm({
    name: app.name,
    type: app.type,
    redirects: app.redirectUris.join("\n"),
    logouts: app.postLogoutRedirectUris.join("\n"),
    allowRefresh: app.allowRefresh,
    access: app.access,
    allowedUserIds: app.allowedUserIds,
  });
  const [error, setError] = useState("");
  const [confirm, setConfirm] = useState<"secret" | "delete" | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    try {
      const r = await api.updateApp(app.id, toPayload(v));
      onSaved(r.app);
      toast("更改已保存");
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const act = async () => {
    setBusy(true);
    setError("");
    try {
      if (confirm === "secret") {
        const r = await api.rotateSecret(app.id);
        onSecret(app, r.secret);
      } else {
        await api.deleteApp(app.id);
        onDeleted(app.id);
        toast(`已删除 ${app.name}`);
      }
    } catch (err) {
      setError(errorMessage(err));
      setBusy(false);
    }
  };

  const warning =
    confirm === "secret"
      ? "旧密钥会立即失效。在应用里换上新密钥之前，用户无法通过它登录。"
      : "删除后，这个应用签发的所有令牌立即失效，用户无法再通过它登录。此操作不能撤销。";

  return (
    <form id="edit-app" onSubmit={submit} className="space-y-6">
      {error && <Notice>{error}</Notice>}
      <AppFields v={v} set={set} typeLocked />
      <section className="overflow-hidden rounded-xl border border-void/25" aria-labelledby="danger">
        <div className="flex items-center gap-2 border-b border-void/15 bg-void/[0.04] px-4 py-2.5">
          <Icon name="alert" className="size-4 text-void" />
          <h3 id="danger" className="text-[0.8125rem] font-semibold text-void">
            危险操作
          </h3>
        </div>
        {confirm ? (
          <div className="space-y-3 p-4">
            <p className="text-sm leading-6">{warning}</p>
            <div className="flex flex-wrap gap-2">
              <Button variant="danger" size="sm" busy={busy} onClick={act}>
                {confirm === "secret" ? "重新生成密钥" : "删除应用"}
              </Button>
              <Button variant="ghost" size="sm" onClick={() => setConfirm(null)}>
                取消
              </Button>
            </div>
          </div>
        ) : (
          <div className="divide-y divide-line/80">
            {app.hasSecret && (
              <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
                <div className="min-w-0">
                  <p className="text-sm font-medium">重新生成客户端密钥</p>
                  <p className="mt-0.5 text-[0.78125rem] text-ink-soft">旧密钥会立即失效。</p>
                </div>
                <Button variant="danger" size="sm" onClick={() => setConfirm("secret")}>
                  重新生成密钥…
                </Button>
              </div>
            )}
            <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
              <div className="min-w-0">
                <p className="text-sm font-medium">删除这个应用</p>
                <p className="mt-0.5 text-[0.78125rem] text-ink-soft">所有令牌立即失效，不能撤销。</p>
              </div>
              <Button variant="danger" size="sm" onClick={() => setConfirm("delete")}>
                删除应用…
              </Button>
            </div>
          </div>
        )}
      </section>
    </form>
  );
}
