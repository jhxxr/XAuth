import { useEffect } from "react";
import { AppShell, Centered } from "./components/Shell";
import { EmptyState, Notice } from "./components/ui";
import { AccountPage } from "./pages/Account";
import { AppsPage } from "./pages/Apps";
import { AdminInvitesPage } from "./pages/AdminInvites";
import { AdminMailPage } from "./pages/AdminMail";
import { AdminTotpPage } from "./pages/AdminTotp";
import { EventsPage } from "./pages/Events";
import { ForgotPasswordPage } from "./pages/ForgotPassword";
import { KeysPage } from "./pages/Keys";
import { ResetPasswordPage } from "./pages/ResetPassword";
import { SettingsPage } from "./pages/Settings";
import { VerifyEmailPage } from "./pages/VerifyEmail";
import { LoginPage } from "./pages/Login";
import { LogoutPage } from "./pages/Logout";
import { RegisterPage } from "./pages/Register";
import { UserMailPage } from "./pages/UserMail";
import { UserTotpPage } from "./pages/UserTotp";
import { UsersPage } from "./pages/Users";
import { Link, navigate, useLocation } from "./router";
import { useSession } from "./session";

function Redirect({ to }: { to: string }) {
  useEffect(() => navigate(to, { replace: true }), [to]);
  return null;
}

export function App() {
  const { session, error } = useSession();
  const { path, query } = useLocation();

  if (error) {
    return (
      <Centered>
        <Notice>无法连接到 XAuth：{error}</Notice>
      </Centered>
    );
  }
  if (!session) return null;
  const user = session.user;

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
    case "/admin/users":
    case "/admin/invites":
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
      if (path === "/admin/users") return <UsersPage />;
      if (path === "/admin/invites") return <AdminInvitesPage />;
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
}
