import { useEffect, useState } from "react";
import { api, errorMessage, type AuditEvent } from "../api";
import { AppShell } from "../components/Shell";
import { Icon } from "../components/Icon";
import { Button, Notice, Segmented } from "../components/ui";
import { eventTypeLabels } from "./AdminOverview";

/** A dot colour per kind of event: failures and replays in red, admin changes in violet. */
function tone(type: string) {
  if (/failure|denied|reuse/.test(type)) return "bg-void";
  if (/delete|retire|signout|revoke|unassign|remove/.test(type)) return "bg-[#c47a1a]";
  if (type.startsWith("admin.") || type.startsWith("totp.") || type.startsWith("mail.") || type.startsWith("outlook.")) return "bg-stamp";
  if (/success|signin|register|verify/.test(type)) return "bg-seal";
  return "bg-ink-faint";
}

const groups = [
  ["", "全部"],
  ["signin", "登录"],
  ["apps", "应用"],
  ["mail", "邮件"],
  ["admin", "管理"],
  ["security", "安全"],
] as const;

const when = new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });

export function EventsPage() {
  const [group, setGroup] = useState("");
  const [rows, setRows] = useState<AuditEvent[] | null>(null);
  const [error, setError] = useState("");

  const load = (before = "") => {
    const query = `?group=${group}&limit=50${before ? `&before=${before}` : ""}`;
    return api.events(query).then((r) => setRows((prev) => (before && prev ? [...prev, ...r.events] : r.events))).catch((e) => setError(errorMessage(e)));
  };
  useEffect(() => { void load(); }, [group]);

  return (
    <AppShell title="操作记录" description="登录、应用授权与管理操作的审计日志，按时间倒序排列。">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <Segmented label="筛选记录" options={groups.map(([v, l]) => [v, l] as [string, string])} value={group} onChange={setGroup} />
        {rows && <p className="text-[0.8125rem] text-ink-soft tabular-nums">已加载 {rows.length} 条</p>}
      </div>
      {error && <div className="mb-4"><Notice>{error}</Notice></div>}
      <div className="panel overflow-x-auto">
        <table className="data-table min-w-[720px]">
          <thead><tr>{["时间", "操作", "操作人", "对象", "地址"].map((h) => <th key={h}>{h}</th>)}</tr></thead>
          <tbody>
            {rows?.map((e) => (
              <tr key={e.id}>
                <td className="whitespace-nowrap text-[0.8125rem] text-ink-soft tabular-nums">{when.format(new Date(e.at))}</td>
                <td>
                  <span className="inline-flex items-center gap-2 font-medium whitespace-nowrap">
                    <span aria-hidden="true" className={`size-1.5 shrink-0 rounded-full ${tone(e.type)}`} />
                    {eventTypeLabels[e.type] ?? e.type}
                  </span>
                </td>
                <td className="max-w-[14rem] truncate">{e.actorEmail || <span className="text-ink-faint">—</span>}</td>
                <td className="max-w-[14rem] truncate">{e.subjectName || <span className="text-ink-faint">—</span>}</td>
                <td className="text-[0.8125rem] text-ink-soft tabular-nums">{e.ip || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {rows?.length === 0 && (
          <div className="flex flex-col items-center px-6 py-14 text-center">
            <Icon name="history" className="size-6 text-ink-faint" />
            <p className="mt-3 text-sm text-ink-soft">这个范围内还没有记录。</p>
          </div>
        )}
      </div>
      {(rows?.length ?? 0) >= 50 && (
        <div className="mt-5 flex justify-center">
          <Button variant="secondary" onClick={() => load(String(rows![rows!.length - 1].id))}>加载更早的记录</Button>
        </div>
      )}
    </AppShell>
  );
}
