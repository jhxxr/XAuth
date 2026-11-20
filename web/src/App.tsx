import { Component, lazy, Suspense, useEffect, type ReactNode } from "react";
import { AppShell, Centered } from "./components/Shell";
import { Button, EmptyState, Notice } from "./components/ui";
import { Link, navigate, useLocation } from "./router";
import { useSession } from "./session";

const AccountPage = lazy(() => import("./pages/Account").then((page) => ({ default: page.AccountPage })));
const AppsPage = lazy(() => import("./pages/Apps").then((page) => ({ default: page.AppsPage })));
const AdminInvitesPage = lazy(() => import("./pages/AdminInvites").then((page) => ({ default: page.AdminInvitesPage })));
const AdminOverviewPage = lazy(() => import("./pages/AdminOverview").then((page) => ({ default: page.AdminOverviewPage })));
const AdminSystemPage = lazy(() => import("./pages/AdminSystem").then((page) => ({ default: page.AdminSystemPage })));
const AdminMailPage = lazy(() => import("./pages/AdminMail").then((page) => ({ default: page.AdminMailPage })));
const AdminTotpPage = lazy(() => import("./pages/AdminTotp").then((page) => ({ default: page.AdminTotpPage })));
const EventsPage = lazy(() => import("./pages/Events").then((page) => ({ default: page.EventsPage })));
const ForgotPasswordPage = lazy(() => import("./pages/ForgotPassword").then((page) => ({ default: page.ForgotPasswordPage })));
const KeysPage = lazy(() => import("./pages/Keys").then((page) => ({ default: page.KeysPage })));
const ResetPasswordPage = lazy(() => import("./pages/ResetPassword").then((page) => ({ default: page.ResetPasswordPage })));
const SettingsPage = lazy(() => import("./pages/Settings").then((page) => ({ default: page.SettingsPage })));
const VerifyEmailPage = lazy(() => import("./pages/VerifyEmail").then((page) => ({ default: page.VerifyEmailPage })));
const LoginPage = lazy(() => import("./pages/Login").then((page) => ({ default: page.LoginPage })));
const LogoutPage = lazy(() => import("./pages/Logout").then((page) => ({ default: page.LogoutPage })));
const RegisterPage = lazy(() => import("./pages/Register").then((page) => ({ default: page.RegisterPage })));
const UserMailPage = lazy(() => import("./pages/UserMail").then((page) => ({ default: page.UserMailPage })));
const UserTotpPage = lazy(() => import("./pages/UserTotp").then((page) => ({ default: page.UserTotpPage })));
const UsersPage = lazy(() => import("./pages/Users").then((page) => ({ default: page.UsersPage })));

function PageLoading() {
  return (
    <Centered>
      <div role="status" aria-live="polite" className="pass grid min-h-80 place-items-center text-sm text-ink-soft">
        正在加载页面…
      </div>
    </Centered>
  );
}

class PageErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state: { failed: boolean } = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  render() {
    if (!this.state.failed) return this.props.children;
    return (
      <Centered>
        <div className="w-full max-w-md space-y-4">
          <Notice>页面加载失败，请检查网络后重新加载页面。</Notice>
          <Button className="w-full" onClick={() => window.location.reload()}>重新加载页面</Button>
        </div>
      </Centered>
    );
  }
}

function Redirect({ to }: { to: string }) {
  useEffect(() => navigate(to, { replace: true }), [to]);
  return null;
}

export function App() {
  const { session, error, reload } = useSession();
  const { path, query } = useLocation();

  if (error) {
    return (
      <Centered>
        <div className="w-full max-w-md space-y-4">
          <Notice>无法连接到 XAuth：{error}</Notice>
          <Button className="w-full" onClick={() => void reload()}>重新连接</Button>
        </div>
      </Centered>
    );
  }
  if (!session) {
    return <Centered><div role="status" aria-live="polite" className="text-sm text-ink-soft">正在连接 XAuth…</div></Centered>;
  }
  const user = session.user;

  const route = () => {
    switch (path) {
      case "/login":
        // Re-mount when the query changes, e.g. from an expired link to a fresh login.
        return <LoginPage key={query.toString()} />;
      case "/register":
        return <RegisterPage key={query.toString()} />;
      case "/logout":
        return <LogoutPage />;
      case "/forgot-password":
        return <ForgotPasswordPage />;
      case "/reset":
        return <ResetPasswordPage />;
      case "/verify":
        return <VerifyEmailPage />;
      case "/":
        return <Redirect to={user ? "/account" : "/login"} />;
    }

    if (!user) return <Redirect to="/login" />;

    switch (path) {
      case "/account":
        return <AccountPage />;
      case "/settings":
        return <SettingsPage />;
      case "/mail":
        return <UserMailPage />;
      case "/totp":
        return <UserTotpPage />;
      case "/admin":
      case "/admin/users":
      case "/admin/invites":
      case "/admin/settings":
      case "/admin/apps":
      case "/admin/mail":
      case "/admin/events":
      case "/admin/totp":
      case "/admin/keys":
        if (!user.isAdmin) {
          return (
            <AppShell title="需要管理员权限">
              <EmptyState icon="shield" title="没有访问权限" description="只有管理员可以管理用户和应用。" action={<Link href="/account" className="text-sm font-medium text-stamp hover:underline">返回我的账户</Link>} />
            </AppShell>
          );
        }
        if (path === "/admin") return <AdminOverviewPage />;
        if (path === "/admin/users") return <UsersPage />;
        if (path === "/admin/invites") return <AdminInvitesPage />;
        if (path === "/admin/settings") return <AdminSystemPage />;
        if (path === "/admin/apps") return <AppsPage />;
        if (path === "/admin/mail") return <AdminMailPage />;
        if (path === "/admin/events") return <EventsPage />;
        if (path === "/admin/totp") return <AdminTotpPage />;
        return <KeysPage />;
    }

    return (
      <AppShell title="找不到页面">
        <EmptyState
          icon="link"
          title="这个地址不存在"
          description="链接可能已失效，或者页面已经移动。"
          action={
            <Link href="/account" className="text-sm font-medium text-stamp hover:underline">
              返回我的账户
            </Link>
          }
        />
      </AppShell>
    );
  };

  return <PageErrorBoundary key={path}><Suspense fallback={<PageLoading />}>{route()}</Suspense></PageErrorBoundary>;
}
