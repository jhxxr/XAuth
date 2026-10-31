import { useState, type FormEvent } from "react";
import { api, errorMessage, type TotpEntry, type TotpWrite, type User } from "../api";
import { Button, Field, GroupLabel, Notice, TextArea, UserChecklist } from "../components/ui";

export type TotpDraft = TotpWrite & { userIds?: string[] };

/** The add/edit form shared by the admin vault and a user's own entries. */
export function TotpForm({
  entry,
  users,
  onSubmit,
  onCancel,
}: {
  entry?: TotpEntry;
  users?: User[];
  onSubmit: (draft: TotpDraft) => Promise<void>;
  onCancel: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [userIds, setUserIds] = useState<string[]>(entry?.userIds ?? []);

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    const digits = Number(data.get("digits"));
    const period = Number(data.get("period"));
    setBusy(true);
    setError("");
    try {
      const text = (key: string) => String(data.get(key) ?? "").trim();
      const optional = (key: "issuer" | "accountName" | "note") => (entry || text(key) ? { [key]: text(key) } : {});
      await onSubmit({
        input: text("input"),
        name: text("name"),
        ...optional("issuer"),
        ...optional("accountName"),
        digits: Number.isFinite(digits) ? digits : undefined,
        period: Number.isFinite(period) ? period : undefined,
        algorithm: text("algorithm"),
        ...optional("note"),
        userIds,
      });
    } catch (err) {
      setError(errorMessage(err));
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-5">
      {error && <Notice>{error}</Notice>}
      <TextArea
        label={entry ? "更换密钥" : "密钥"}
        name="input"
        required={!entry}
        spellCheck={false}
        placeholder="Base32 密钥，或 otpauth://totp/..."
        hint={entry ? "留空则保持原来的密钥。粘贴 otpauth 链接会同时更新参数。" : "支持 Base32 密钥和 otpauth://totp 链接。密钥只保存在服务器上。"}
      />
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="名称" name="name" defaultValue={entry?.name ?? ""} maxLength={128} placeholder="留空则按发行方生成" />
        <Field label="发行方" name="issuer" defaultValue={entry?.issuer ?? ""} maxLength={128} />
        <Field label="账号" name="accountName" defaultValue={entry?.accountName ?? ""} maxLength={128} />
        <Field label="备注" name="note" defaultValue={entry?.note ?? ""} maxLength={128} />
      </div>
      <fieldset className="rounded-xl border border-line bg-desk/30 p-4">
        <legend className="eyebrow -ml-1 px-1">参数</legend>
        <div className="grid gap-4 sm:grid-cols-3">
          <Field label="位数" name="digits" type="number" min={6} max={8} defaultValue={entry?.digits ?? 6} />
          <Field label="周期（秒）" name="period" type="number" min={15} max={120} defaultValue={entry?.period ?? 30} />
          <label className="block text-[0.8125rem] font-medium">
            算法
            <select name="algorithm" defaultValue={entry?.algorithm ?? "SHA1"} className="field mt-1.5">
              <option value="SHA1">SHA1</option>
              <option value="SHA256">SHA256</option>
              <option value="SHA512">SHA512</option>
            </select>
          </label>
        </div>
      </fieldset>
      {users && (
        <fieldset>
          <GroupLabel>分配给</GroupLabel>
          <UserChecklist users={users} selected={userIds} onChange={setUserIds} />
        </fieldset>
      )}
      <div className="-mx-6 -mb-5 flex justify-end gap-2 border-t border-line bg-desk/40 px-6 py-3.5">
        <Button variant="ghost" onClick={onCancel}>取消</Button>
        <Button type="submit" busy={busy}>{entry ? "保存" : "添加"}</Button>
      </div>
    </form>
  );
}

export async function loadUsers() {
  const r = await api.users();
  return r.users.filter((u) => !u.disabled);
}
