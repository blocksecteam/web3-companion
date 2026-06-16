// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import { Suspense, lazy, useCallback, useEffect, useRef, useState } from "react";
import { Toast } from "@/components/Toast";
import { LandingPage } from "@/pages/LandingPage";
import {
  normalizeRoute,
  pathForRoute,
  readRoute,
  hasResetAuthRequest,
  shouldWriteHistory,
  type AppRoute,
} from "@/lib/routes";
import { authStatus } from "@/lib/auth-state";
import { logoutSession } from "@/lib/auth-client";
import { hydrateSession, needsSessionHydration } from "@/lib/auth-session";
import i18n from "@/i18n";
import {
  getCurrentUser,
  logout,
  onCurrentUserChanged,
  resetLocalAuthState,
  setCurrentUser,
} from "@/lib/current-user";
import type { Page, User } from "@/types/chat";
import { fetchSetupStatus, type SetupChain } from "@/lib/setup-api";

const AuthPage = lazy(() => import("@/pages/AuthPage").then((module) => ({ default: module.AuthPage })));
const AuditPage = lazy(() => import("@/pages/AuditPage").then((module) => ({ default: module.AuditPage })));
const ChatWorkspace = lazy(() =>
  import("@/components/chat/ChatWorkspace").then((module) => ({ default: module.ChatWorkspace })),
);
const ConfirmPage = lazy(() => import("@/pages/ConfirmPage").then((module) => ({ default: module.ConfirmPage })));
const PortfolioPage = lazy(() =>
  import("@/pages/PortfolioPage").then((module) => ({ default: module.PortfolioPage })),
);
const PolicyPage = lazy(() => import("@/pages/PolicyPage").then((module) => ({ default: module.PolicyPage })));
const SettingsPage = lazy(() => import("@/pages/SettingsPage").then((module) => ({ default: module.SettingsPage })));
const TransactionsPage = lazy(() =>
  import("@/pages/TransactionsPage").then((module) => ({ default: module.TransactionsPage })),
);
const WalletsPage = lazy(() => import("@/pages/WalletsPage").then((module) => ({ default: module.WalletsPage })));
const SetupWizardPage = lazy(() =>
  import("@/pages/SetupWizardPage").then((module) => ({ default: module.SetupWizardPage })),
);

function App() {
  const [setupMode, setSetupMode] = useState<boolean | null>(null);
  const [setupChains, setSetupChains] = useState<SetupChain[]>([]);
  const [route, setRoute] = useState<AppRoute>(() => readRoute());
  const [user, setUser] = useState<User | null>(() => {
    if (hasResetAuthRequest(window.location.search)) {
      resetLocalAuthState();
      return null;
    }
    return getCurrentUser();
  });
  const [toast, setToast] = useState("");
  const [hydratingSession, setHydratingSession] = useState(() => needsSessionHydration(user));
  const lastChatSessionRef = useRef<string | undefined>(undefined);

  useEffect(() => {
    fetchSetupStatus()
      .then((status) => {
        if (status.setupMode && !status.complete) {
          setSetupMode(true);
          setSetupChains(status.chains);
        } else {
          setSetupMode(false);
        }
      })
      .catch(() => setSetupMode(false));
  }, []);
  const page = route.page;
  const status = hydratingSession ? "guest" : authStatus(user);
  const readyUser = status === "ready" ? user : null;

  const navigate = useCallback((nextPage: Page, sessionId?: string, mode: "push" | "replace" = "push") => {
    if (nextPage === "chat" && !sessionId && lastChatSessionRef.current) {
      sessionId = lastChatSessionRef.current;
    }
    const nextRoute = normalizeRoute({ page: nextPage, sessionId });
    if (nextRoute.page === "chat" && nextRoute.sessionId) {
      lastChatSessionRef.current = nextRoute.sessionId;
    }
    const nextPath = pathForRoute(nextRoute);
    if (shouldWriteHistory(nextPath, window.location)) {
      window.history[mode === "replace" ? "replaceState" : "pushState"](null, "", nextPath);
    }
    setRoute(nextRoute);
  }, []);

  useEffect(() => {
    if (!hasResetAuthRequest(window.location.search)) return;
    resetLocalAuthState();
    setUser(null);
    navigate("landing", undefined, "replace");
  }, [navigate]);

  useEffect(() => {
    const syncRoute = () => setRoute(readRoute());
    window.addEventListener("popstate", syncRoute);
    return () => window.removeEventListener("popstate", syncRoute);
  }, []);

  useEffect(() => onCurrentUserChanged(setUser), []);

  useEffect(() => {
    if (!needsSessionHydration(user)) {
      setHydratingSession(false);
      return;
    }
    let active = true;
    setHydratingSession(true);
    hydrateSession(user, {
      onUserUpdated: (nextUser) => {
        if (active) setUser(nextUser);
      },
    })
      .then((hydrated) => {
        if (!active) return;
        setUser(hydrated);
      })
      .catch(() => {
        if (!active) return;
        resetLocalAuthState();
        setUser(null);
      })
      .finally(() => {
        if (active) setHydratingSession(false);
      });
    return () => {
      active = false;
    };
  }, [user]);

  useEffect(() => {
    if (status === "stale") {
      resetLocalAuthState();
      setUser(null);
      if (page !== "landing" && page !== "auth" && page !== "confirm") {
        navigate("landing", undefined, "replace");
      }
      return;
    }
    if (status === "ready" && (page === "landing" || page === "auth" || (page === "chat" && !route.sessionId))) {
      navigate("chat", route.sessionId, "replace");
    }
    if (hydratingSession) return;
    if (status === "guest" && page !== "landing" && page !== "auth" && page !== "confirm") {
      navigate("landing", undefined, "replace");
    }
  }, [hydratingSession, navigate, page, route.sessionId, status]);

  const handleAuthenticated = (nextUser: User) => {
    if (authStatus(nextUser) !== "ready") {
      setToast(i18n.t("errors.missingSessionCredential"));
      return;
    }
    setCurrentUser(nextUser);
    setUser(nextUser);
    navigate("chat", route.sessionId, "replace");
  };

  const handleLogout = () => {
    void logoutSession().finally(() => {
      logout();
      setUser(null);
      navigate("landing", undefined, "replace");
    });
  };

  if (setupMode === null) return <RouteFallback />;
  if (setupMode) {
    return (
      <Suspense fallback={<RouteFallback />}>
        <SetupWizardPage
          chains={setupChains}
          onComplete={() => {
            setSetupMode(false);
            navigate("chat", undefined, "replace");
          }}
        />
      </Suspense>
    );
  }

  return (
    <div className="paper-grain min-h-screen overflow-hidden text-foreground">
      <div className="relative z-10 min-h-screen">
        {page === "landing" && <LandingPage onStart={() => navigate("auth")} />}
        <Suspense fallback={<RouteFallback />}>
          {page === "auth" && (
            <AuthPage
              onBack={() => navigate("landing")}
              onAuthenticated={handleAuthenticated}
              onToast={setToast}
            />
          )}
          {page === "confirm" && route.sessionId && (
            <ConfirmPage challengeId={route.sessionId} onDone={() => navigate(readyUser ? "chat" : "landing")} />
          )}
          {readyUser && page === "chat" && route.sessionId && (
            <ChatWorkspace
              user={readyUser}
              activeSessionId={route.sessionId}
              onSessionChange={(sessionId) => navigate("chat", sessionId)}
              onNavigate={navigate}
              onLogout={handleLogout}
            />
          )}
          {readyUser && page === "portfolio" && (
            <PortfolioPage
              user={readyUser}
              onNavigate={navigate}
              onLogout={handleLogout}
              onToast={setToast}
            />
          )}
          {readyUser && page === "transactions" && (
            <TransactionsPage
              user={readyUser}
              onNavigate={navigate}
              onLogout={handleLogout}
              onToast={setToast}
            />
          )}
          {readyUser && page === "policies" && (
            <PolicyPage user={readyUser} onNavigate={navigate} onLogout={handleLogout} onToast={setToast} />
          )}
          {readyUser && page === "audit" && (
            <AuditPage user={readyUser} onNavigate={navigate} onLogout={handleLogout} />
          )}
          {readyUser && page === "wallets" && (
            <WalletsPage user={readyUser} onNavigate={navigate} onLogout={handleLogout} onToast={setToast} />
          )}
          {readyUser && page === "settings" && (
            <SettingsPage user={readyUser} onNavigate={navigate} onLogout={handleLogout} onToast={setToast} />
          )}
        </Suspense>
        <Toast message={toast} onDismiss={() => setToast("")} />
      </div>
    </div>
  );
}

function RouteFallback() {
  return <div className="min-h-screen bg-background" />;
}

export default App;
