import { useCallback, useEffect, useState } from "react";
import { HashRouter, Navigate, Route, Routes } from "react-router-dom";
import { SplashScreen } from "@/components/common/SplashScreen";
import { ViewerDashboardPage } from "@/viewer/pages/ViewerDashboardPage";
import { ViewerMonitoringPage } from "@/viewer/pages/ViewerMonitoringPage";
import { ViewerQuotaPage } from "@/viewer/pages/ViewerQuotaPage";
import { ViewerKeyQuotaPage } from "@/viewer/pages/ViewerKeyQuotaPage";
import { ViewerUsageAnalyticsPage } from "@/viewer/pages/ViewerUsageAnalyticsPage";
import { ViewerUsageStatusPage } from "@/viewer/pages/ViewerUsageStatusPage";
import { ViewerLayout } from "./ViewerLayout";
import { ViewerLoginPage } from "./ViewerLoginPage";
import { setAuthenticationFailureHandler, viewerApi } from "./viewerApi";

type SessionState = "checking" | "authenticated" | "anonymous";

export function ViewerApp() {
  const [sessionState, setSessionState] = useState<SessionState>("checking");
  const [sessionError, setSessionError] = useState("");
  const [publicAccess, setPublicAccess] = useState(false);

  useEffect(() => {
    let active = true;
    setAuthenticationFailureHandler(() => {
      if (!active) return;
      setSessionState("anonymous");
    });

    void viewerApi
      .session()
      .then((session) => {
        if (!active) return;
        setSessionError("");
        setPublicAccess(session.public_access === true);
        setSessionState(session.authenticated ? "authenticated" : "anonymous");
      })
      .catch((error: unknown) => {
        if (!active) return;
        setSessionError(
          error instanceof Error
            ? error.message
            : "Viewer session check failed",
        );
        setSessionState("anonymous");
      });

    return () => {
      active = false;
      setAuthenticationFailureHandler(null);
    };
  }, []);

  const handleLogout = useCallback(async () => {
    try {
      await viewerApi.logout();
    } finally {
      setSessionState("anonymous");
    }
  }, []);

  if (sessionState === "checking") {
    return <SplashScreen onFinish={() => undefined} />;
  }

  if (sessionState === "anonymous") {
    return (
      <ViewerLoginPage
        initialError={sessionError}
        onAuthenticated={() => {
          setSessionError("");
          setSessionState("authenticated");
        }}
      />
    );
  }

  return (
    <HashRouter>
      <Routes>
        <Route
          element={
            <ViewerLayout onLogout={publicAccess ? undefined : handleLogout} />
          }
        >
          <Route index element={<ViewerDashboardPage />} />
          <Route path="/dashboard" element={<Navigate to="/" replace />} />
          <Route
            path="/usage-analytics"
            element={<ViewerUsageAnalyticsPage />}
          />
          <Route path="/monitoring" element={<ViewerMonitoringPage />} />
          <Route path="/quota" element={<ViewerQuotaPage />} />
          <Route path="/key-quota" element={<ViewerKeyQuotaPage />} />
          <Route path="/usage-status" element={<ViewerUsageStatusPage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </HashRouter>
  );
}
