// Typed client for XAuth's JSON API. Every request is same-origin JSON,
// which the server requires for anything that changes state.

export type User = {
  id: string;
  email: string;
  name: string;
  /** Cache-busting URL of the user's picture, or null when they have none. */
  avatarUrl: string | null;
  emailVerified: boolean;
  isAdmin: boolean;
  disabled: boolean;
  createdAt: string;
  lastLoginAt: string | null;
};

export type Session = { user: User | null; signupEnabled: boolean; issuer: string; mailEnabled: boolean };

export type Flow = {
  id: string;
  app: { id: string; name: string };
  scopes: string[];
  redirectTo: string;
  loginHint?: string;
  canContinue: boolean;
  expiresAt: string;
};

export type AppType = "web" | "spa" | "native";

export type App = {
  id: string;
  name: string;
  type: AppType;
  redirectUris: string[];
  postLogoutRedirectUris: string[];
  allowRefresh: boolean;
  access: "all" | "admins" | "selected";
  allowedUserIds: string[];
  hasSecret: boolean;
  createdAt: string;
  updatedAt: string;
};

export type Endpoints = {
  issuer: string;
  discovery: string;
  authorization: string;
  token: string;
  userinfo: string;
  jwks: string;
  endSession: string;
  revocation: string;
  introspection: string;
};

export type SignInResult = { user: User; redirect?: string; flowError?: string };

export class ApiError extends Error {
  constructor(
    readonly code: string,
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      method,
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: method === "GET" ? undefined : JSON.stringify(body ?? {}),
    });
  } catch {
    throw new ApiError("network", "无法连接到 XAuth，请检查网络后重试。", 0);
  }
  const data = await res.json().catch(() => null);
  if (!res.ok) {
    throw new ApiError(data?.error ?? "internal", data?.message ?? "服务器出错，请稍后再试。", res.status);
  }
  return data as T;
}

export const api = {
  session: () => request<Session>("GET", "/api/session"),
  login: (email: string, password: string, flow?: string) =>
    request<SignInResult>("POST", "/api/login", { email, password, flow: flow ?? "" }),
  register: (email: string, password: string, name: string, flow?: string) =>
    request<SignInResult>("POST", "/api/register", { email, password, name, flow: flow ?? "" }),
  logout: () => request<{ ok: boolean }>("POST", "/api/logout"),
  updateAccount: (name: string) => request<{ user: User }>("PATCH", "/api/account", { name }),
  setAvatar: (image: string) => request<{ user: User }>("PUT", "/api/account/avatar", { image }),
  removeAvatar: () => request<{ user: User }>("DELETE", "/api/account/avatar"),
  changePassword: (current: string, next: string) =>
    request<{ ok: boolean }>("POST", "/api/account/password", { current, new: next }),
  requestVerifyEmail: () => request<{ ok: boolean }>("POST", "/api/account/verify-email"),
  verifyEmail: (token: string) => request<{ ok: boolean }>("POST", "/api/verify", { token }),
  forgotPassword: (email: string) => request<{ ok: boolean }>("POST", "/api/forgot-password", { email }),
  resetPassword: (token: string, password: string) =>
    request<{ user: User }>("POST", "/api/reset-password", { token, password }),

  passkeys: () => request<{ passkeys: Passkey[]; hasPassword: boolean }>("GET", "/api/account/passkeys"),
  beginPasskeyRegister: (name: string) =>
    request<{ challengeId: string; options: Record<string, unknown> }>("POST", "/api/account/passkeys/register/begin", { name }),
  finishPasskeyRegister: (challengeId: string, name: string, credential: unknown) =>
    request<{ passkey: Passkey }>(
      "POST",
      `/api/account/passkeys/register/finish/${encodeURIComponent(challengeId)}?name=${encodeURIComponent(name)}`,
      credential,
    ),
  deletePasskey: (id: string) => request<{ ok: boolean }>("DELETE", `/api/account/passkeys/${id}`),
  beginPasskeyLogin: () =>
    request<{ challengeId: string; options: Record<string, unknown> }>("POST", "/api/auth/passkey/begin"),
  finishPasskeyLogin: (challengeId: string, credential: unknown, flow = "") =>
    request<SignInResult>(
      "POST",
      `/api/auth/passkey/finish/${encodeURIComponent(challengeId)}${flow ? `?flow=${encodeURIComponent(flow)}` : ""}`,
      credential,
    ),

  flow: (id: string) => request<Flow>("GET", `/api/flow/${encodeURIComponent(id)}`),
  continueFlow: (id: string) => request<{ redirect: string }>("POST", `/api/flow/${encodeURIComponent(id)}/continue`),
  denyFlow: (id: string) => request<{ redirect: string }>("POST", `/api/flow/${encodeURIComponent(id)}/deny`),

  users: () => request<{ users: User[] }>("GET", "/api/admin/users"),
  createUser: (u: { email: string; name: string; password: string; isAdmin: boolean; emailVerified: boolean }) =>
    request<{ user: User }>("POST", "/api/admin/users", u),
  updateUser: (id: string, patch: Partial<Pick<User, "name" | "isAdmin" | "disabled" | "emailVerified">> & { password?: string }) =>
    request<{ user: User }>("PATCH", `/api/admin/users/${id}`, patch),

  apps: () => request<{ apps: App[]; endpoints: Endpoints }>("GET", "/api/admin/apps"),
  createApp: (a: { name: string; type: AppType; redirectUris: string[]; postLogoutRedirectUris: string[]; allowRefresh: boolean; access: App["access"]; allowedUserIds: string[] }) =>
    request<{ app: App; secret?: string }>("POST", "/api/admin/apps", a),
  updateApp: (id: string, patch: { name: string; redirectUris: string[]; postLogoutRedirectUris: string[]; allowRefresh: boolean; access: App["access"]; allowedUserIds: string[] }) =>
    request<{ app: App }>("PATCH", `/api/admin/apps/${id}`, patch),
  rotateSecret: (id: string) => request<{ secret: string }>("POST", `/api/admin/apps/${id}/secret`),
  deleteApp: (id: string) => request<{ ok: boolean }>("DELETE", `/api/admin/apps/${id}`),

  sessions: () => request<{ sessions: DeviceSession[] }>("GET", "/api/sessions"),
  deleteSession: (id: string) => request<{ ok: boolean }>("DELETE", `/api/sessions/${id}`),
  revokeOtherSessions: () => request<{ revoked: number }>("POST", "/api/sessions/revoke-others"),
  grants: () => request<{ grants: Grant[] }>("GET", "/api/grants"),
  revokeGrant: (clientId: string) => request<{ ok: boolean }>("DELETE", `/api/grants/${clientId}`),

  events: (query: string) => request<{ events: AuditEvent[] }>("GET", `/api/admin/events${query}`),
  keys: () => request<{ keys: SigningKey[] }>("GET", "/api/admin/keys"),
  rotateKey: () => request<{ key: SigningKey }>("POST", "/api/admin/keys"),
  retireKey: (id: string) => request<{ ok: boolean }>("POST", `/api/admin/keys/${id}/retire`),
  forceSignOut: (id: string) => request<{ ok: boolean }>("POST", `/api/admin/users/${id}/signout`),

  myTotp: () => request<{ entries: TotpEntry[] }>("GET", "/api/totp"),
  createMyTotp: (input: TotpWrite) => request<{ entry: TotpEntry }>("POST", "/api/totp", input),
  updateMyTotp: (id: string, input: TotpWrite) => request<{ entry: TotpEntry }>("PATCH", `/api/totp/${id}`, input),
  deleteMyTotp: (id: string) => request<{ ok: boolean }>("DELETE", `/api/totp/${id}`),
  adminTotp: () => request<{ entries: TotpEntry[] }>("GET", "/api/admin/totp"),
  createAdminTotp: (input: TotpWrite & { userIds: string[] }) => request<{ entry: TotpEntry }>("POST", "/api/admin/totp", input),
  updateAdminTotp: (id: string, input: TotpWrite & { userIds: string[] }) => request<{ entry: TotpEntry }>("PATCH", `/api/admin/totp/${id}`, input),
  deleteAdminTotp: (id: string) => request<{ ok: boolean }>("DELETE", `/api/admin/totp/${id}`),
  assignTotp: (id: string, userIds: string[]) => request<{ entry: TotpEntry }>("PUT", `/api/admin/totp/${id}/grants`, { userIds }),

  // Mail - User
  mailStatus: () => request<{ configured: boolean }>("GET", "/api/mail/status"),
  myMailboxes: () => request<{ mailboxes: MyMailbox[] }>("GET", "/api/mail/mailboxes"),
  myMailbox: (address: string) =>
    request<{
      mailbox: { address: string; expiresAt: number | null; isActive: boolean };
      stats: { total: number; unread: number };
      services: { name: string; loginUrl: string; note: string | null; expiresAt: number | null }[];
    }>("GET", `/api/mail/mailboxes/${encodeURIComponent(address)}`),
  myEmails: (address: string, query = "") =>
    request<{ items: EmailItem[]; total?: number }>("GET", `/api/mail/mailboxes/${encodeURIComponent(address)}/emails${query}`),
  myCode: (address: string, seconds = 600) =>
    request<VerificationCodeResult>("GET", `/api/mail/mailboxes/${encodeURIComponent(address)}/verification-code?seconds=${seconds}`),
  myEmail: (id: string) => request<{ email: EmailDetail }>("GET", `/api/mail/emails/${encodeURIComponent(id)}`),
  markEmailRead: (id: string) => request<{ email: EmailItem }>("POST", `/api/mail/emails/${encodeURIComponent(id)}/read`),
  toggleEmailStar: (id: string, isStarred?: boolean) =>
    request<{ email: EmailItem }>("POST", `/api/mail/emails/${encodeURIComponent(id)}/star`, isStarred !== undefined ? { isStarred } : {}),

  // Mail - Admin Grants & Proxy
  mailGrants: (check = false) => request<{ grants: MailGrant[] }>("GET", `/api/admin/mail-grants${check ? "?check=1" : ""}`),
  setMailGrants: (address: string, userIds: string[]) =>
    request<MailGrant>("PUT", `/api/admin/mail-grants/${encodeURIComponent(address)}`, { userIds }),
  deleteMailGrants: (address: string) =>
    request<{ removed: number }>("DELETE", `/api/admin/mail-grants/${encodeURIComponent(address)}`),
  adminMail: <T>(method: string, subpath: string, body?: unknown) =>
    request<T>(method, `/api/admin/mail${subpath.startsWith("/") ? "" : "/"}${subpath}`, body),
};

export type MyMailbox = {
  address: string;
  assignedAt: string;
};

export type EmailItem = {
  id: string;
  mailbox: string;
  from: string;
  fromName: string | null;
  to: string;
  subject: string | null;
  isRead: boolean;
  isStarred: boolean;
  createdAt: number;
  preview: string;
  verificationCode: string | null;
};

export type EmailDetail = EmailItem & {
  text: string | null;
  hasHtml: boolean;
};

export type VerificationCodeResult = {
  code: string | null;
  subject?: string;
  from?: string;
  fromName?: string;
  receivedAt?: number;
  emailId?: string;
};

export type MailGrant = {
  address: string;
  userIds: string[];
  status?: "ok" | "trashed" | "missing" | "unknown";
};

export type DeviceSession = {
  id: string;
  userAgent: string;
  ip: string;
  createdAt: string;
  lastSeen: string;
  current: boolean;
};

export type Grant = {
  clientId: string;
  clientName: string;
  scopes: string[];
  lastUsed: string;
  refresh: boolean;
};

export type AuditEvent = {
  id: number;
  at: string;
  type: string;
  actorId?: string;
  actorEmail?: string;
  subjectType?: string;
  subjectId?: string;
  subjectName?: string;
  ip?: string;
  detail?: Record<string, unknown>;
};

export type Passkey = {
  id: string;
  name: string;
  createdAt: string;
  lastUsedAt: string | null;
};

export type TotpEntry = {
  id: string;
  name: string;
  issuer: string;
  accountName: string;
  digits: number;
  period: number;
  algorithm: string;
  note: string;
  shared: boolean;
  userIds?: string[];
  code?: string;
  remaining?: number;
  createdAt: string;
  updatedAt: string;
};

export type TotpWrite = {
  input?: string;
  name?: string;
  issuer?: string;
  accountName?: string;
  secret?: string;
  digits?: number;
  period?: number;
  algorithm?: string;
  note?: string;
};

export type SigningKey = {
  id: string;
  createdAt: string;
  current: boolean;
  retiredAt: string | null;
};

export function errorMessage(err: unknown): string {
  return err instanceof ApiError ? err.message : "出现未知错误，请刷新页面后重试。";
}
