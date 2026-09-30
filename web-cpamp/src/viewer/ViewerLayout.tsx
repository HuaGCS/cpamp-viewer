import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { NavLink, Outlet, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import {
  CPAMP_SYMBOL_COLOR_PNG_URL,
  CPAMP_WORDMARK_COLOR_PNG_URL,
  CPAMP_WORDMARK_ON_DARK_PNG_URL,
} from "@/assets/brand";
import { Button } from "@/components/ui/Button";
import { DatabaseMaintenanceBanner } from "@/components/common/DatabaseMaintenanceBanner";
import {
  DatabaseMaintenanceContext,
  type DatabaseMaintenanceContextValue,
} from "@/components/common/useDatabaseMaintenance";
import {
  IconLanguages,
  IconMoon,
  IconRefreshCw,
  IconSidebarDashboard,
  IconSidebarMonitor,
  IconSidebarQuota,
  IconSidebarUsage,
  IconSun,
} from "@/components/ui/icons";
import { triggerHeaderRefresh } from "@/hooks/useHeaderRefresh";
import { useLanguageStore } from "@/stores/useLanguageStore";
import { useThemeStore } from "@/stores/useThemeStore";
import { useVisualEffectsStore } from "@/stores/useVisualEffectsStore";
import type { Theme, VisualEffectsMode } from "@/types/common";
import {
  LANGUAGE_LABEL_KEYS,
  LANGUAGE_ORDER,
  STORAGE_KEY_SIDEBAR,
} from "@/utils/constants";
import { isSupportedLanguage } from "@/utils/language";
import { useViewerMaintenance } from "@/viewer/hooks/useViewerData";
import { keyQuotaCopy } from "@/viewer/model/keyQuota";
import { usageStatusCopy } from "@/viewer/model/usageStatus";
import { ViewerModelPriceNotice } from "@/viewer/components/ViewerModelPriceNotice";
import styles from "./ViewerLayout.module.scss";

const SIDEBAR_ICON_SIZE = 20;

type NavItem = {
  path: string;
  labelKey: string;
  icon: ReactNode;
};

const navItems: NavItem[] = [
  {
    path: "/",
    labelKey: "nav.dashboard",
    icon: <IconSidebarDashboard size={SIDEBAR_ICON_SIZE} />,
  },
  {
    path: "/usage-analytics",
    labelKey: "nav.usage_analytics",
    icon: <IconSidebarUsage size={SIDEBAR_ICON_SIZE} />,
  },
  {
    path: "/monitoring",
    labelKey: "nav.monitoring_center",
    icon: <IconSidebarMonitor size={SIDEBAR_ICON_SIZE} />,
  },
  {
    path: "/quota",
    labelKey: "nav.quota_management",
    icon: <IconSidebarQuota size={SIDEBAR_ICON_SIZE} />,
  },
  {
    path: "/key-quota",
    labelKey: "viewer.key_quota",
    icon: <IconSidebarQuota size={SIDEBAR_ICON_SIZE} />,
  },
  {
    path: "/usage-status",
    labelKey: "viewer.usage_status",
    icon: <IconSidebarUsage size={SIDEBAR_ICON_SIZE} />,
  },
];

const themeOptions: Array<{ key: Theme; labelKey: string; icon: ReactNode }> = [
  { key: "auto", labelKey: "theme.auto", icon: <AutoThemeIcon /> },
  { key: "white", labelKey: "theme.white", icon: <IconSun size={16} /> },
  { key: "dark", labelKey: "theme.dark", icon: <IconMoon size={16} /> },
];

const visualEffectsOptions: Array<{
  key: VisualEffectsMode;
  labelKey: string;
  icon: ReactNode;
}> = [
  { key: "full", labelKey: "visual_effects.full", icon: <EffectsIcon full /> },
  { key: "reduced", labelKey: "visual_effects.reduced", icon: <EffectsIcon /> },
];

interface ViewerLayoutProps {
  onLogout?: () => Promise<void>;
}

export function ViewerLayout({ onLogout }: ViewerLayoutProps) {
  const { t } = useTranslation();
  const location = useLocation();
  const headerRef = useRef<HTMLElement | null>(null);
  const contentRef = useRef<HTMLDivElement | null>(null);
  const languageMenuRef = useRef<HTMLDivElement | null>(null);
  const themeMenuRef = useRef<HTMLDivElement | null>(null);
  const visualEffectsMenuRef = useRef<HTMLDivElement | null>(null);
  const language = useLanguageStore((state) => state.language);
  const setLanguage = useLanguageStore((state) => state.setLanguage);
  const theme = useThemeStore((state) => state.theme);
  const setTheme = useThemeStore((state) => state.setTheme);
  const visualEffectsMode = useVisualEffectsStore((state) => state.mode);
  const setVisualEffectsMode = useVisualEffectsStore((state) => state.setMode);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => {
    try {
      return localStorage.getItem(STORAGE_KEY_SIDEBAR) === "true";
    } catch {
      return false;
    }
  });
  const [languageMenuOpen, setLanguageMenuOpen] = useState(false);
  const [themeMenuOpen, setThemeMenuOpen] = useState(false);
  const [visualEffectsMenuOpen, setVisualEffectsMenuOpen] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [priceRefreshKey, setPriceRefreshKey] = useState(0);
  const [loggingOut, setLoggingOut] = useState(false);
  const maintenance = useViewerMaintenance();
  const maintenanceContextValue = useMemo<DatabaseMaintenanceContextValue>(
    () => ({
      status: maintenance.data?.databaseMaintenance
        ? {
            databaseMaintenance: {
              ...maintenance.data.databaseMaintenance,
              reasons: [],
            },
          }
        : null,
      loading: maintenance.loading,
      error: maintenance.error,
      refresh: maintenance.refresh,
    }),
    [
      maintenance.data,
      maintenance.error,
      maintenance.loading,
      maintenance.refresh,
    ],
  );

  const normalizedPath =
    location.pathname === "/dashboard" ? "/" : location.pathname;
  const activeNavItem =
    navItems.find((item) =>
      item.path === "/"
        ? normalizedPath === "/"
        : normalizedPath === item.path ||
          normalizedPath.startsWith(`${item.path}/`),
    ) ?? navItems[0];
  const navLabel = (item: NavItem) =>
    item.path === "/key-quota"
      ? keyQuotaCopy(language).title
      : item.path === "/usage-status"
        ? usageStatusCopy(language).title
        : t(item.labelKey);
  const currentRouteLabel = navLabel(activeNavItem);
  const showSidebarLabels = !sidebarCollapsed || sidebarOpen;

  useLayoutEffect(() => {
    const updateHeaderHeight = () => {
      const height = headerRef.current?.offsetHeight;
      if (height)
        document.documentElement.style.setProperty(
          "--header-height",
          `${height}px`,
        );
    };
    updateHeaderHeight();
    const observer =
      typeof ResizeObserver !== "undefined" && headerRef.current
        ? new ResizeObserver(updateHeaderHeight)
        : null;
    if (observer && headerRef.current) observer.observe(headerRef.current);
    window.addEventListener("resize", updateHeaderHeight);
    return () => {
      observer?.disconnect();
      window.removeEventListener("resize", updateHeaderHeight);
    };
  }, []);

  useLayoutEffect(() => {
    const updateContentCenter = () => {
      if (!contentRef.current) return;
      const rect = contentRef.current.getBoundingClientRect();
      document.documentElement.style.setProperty(
        "--content-center-x",
        `${rect.left + rect.width / 2}px`,
      );
    };
    updateContentCenter();
    const observer =
      typeof ResizeObserver !== "undefined" && contentRef.current
        ? new ResizeObserver(updateContentCenter)
        : null;
    if (observer && contentRef.current) observer.observe(contentRef.current);
    window.addEventListener("resize", updateContentCenter);
    return () => {
      observer?.disconnect();
      window.removeEventListener("resize", updateContentCenter);
      document.documentElement.style.removeProperty("--content-center-x");
    };
  }, []);

  useEffect(() => {
    contentRef.current?.scrollTo({ top: 0, left: 0, behavior: "auto" });
  }, [location.pathname]);

  useEffect(() => {
    if (!languageMenuOpen && !themeMenuOpen && !visualEffectsMenuOpen) return;

    const closeMenus = (event: MouseEvent) => {
      const target = event.target as Node;
      if (!languageMenuRef.current?.contains(target))
        setLanguageMenuOpen(false);
      if (!themeMenuRef.current?.contains(target)) setThemeMenuOpen(false);
      if (!visualEffectsMenuRef.current?.contains(target))
        setVisualEffectsMenuOpen(false);
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      setLanguageMenuOpen(false);
      setThemeMenuOpen(false);
      setVisualEffectsMenuOpen(false);
    };

    document.addEventListener("mousedown", closeMenus);
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.removeEventListener("mousedown", closeMenus);
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, [languageMenuOpen, themeMenuOpen, visualEffectsMenuOpen]);

  const closeOtherMenus = (keep: "language" | "theme" | "effects") => {
    if (keep !== "language") setLanguageMenuOpen(false);
    if (keep !== "theme") setThemeMenuOpen(false);
    if (keep !== "effects") setVisualEffectsMenuOpen(false);
  };

  const handleLanguageSelect = (nextLanguage: string) => {
    if (!isSupportedLanguage(nextLanguage)) return;
    setLanguage(nextLanguage);
    setLanguageMenuOpen(false);
  };

  const handleRefresh = useCallback(async () => {
    if (refreshing) return;
    setRefreshing(true);
    setPriceRefreshKey((value) => value + 1);
    try {
      await triggerHeaderRefresh();
    } finally {
      setRefreshing(false);
    }
  }, [refreshing]);

  const handleLogout = useCallback(async () => {
    if (loggingOut || !onLogout) return;
    setLoggingOut(true);
    try {
      await onLogout();
    } finally {
      setLoggingOut(false);
    }
  }, [loggingOut, onLogout]);

  const toggleSidebar = () => {
    if (window.matchMedia("(max-width: 768px)").matches) {
      setSidebarOpen((open) => !open);
      return;
    }
    setSidebarCollapsed((collapsed) => {
      const next = !collapsed;
      try {
        localStorage.setItem(STORAGE_KEY_SIDEBAR, String(next));
      } catch {
        // Keep the in-memory setting when storage is unavailable.
      }
      return next;
    });
  };

  return (
    <DatabaseMaintenanceContext.Provider value={maintenanceContextValue}>
      <div
      className={`app-shell ${sidebarCollapsed ? "sidebar-is-collapsed" : ""}`}
    >
      <header className="main-header" ref={headerRef}>
        <div className="navbar">
          <div className="navbar-left">
            <button
              type="button"
              className="hamburger-container"
              onClick={toggleSidebar}
              title={
                sidebarOpen
                  ? t("sidebar.toggle_collapse")
                  : t("sidebar.toggle_expand")
              }
              aria-label={
                sidebarOpen
                  ? t("sidebar.toggle_collapse")
                  : t("sidebar.toggle_expand")
              }
            >
              {sidebarOpen ? (
                <CloseIcon />
              ) : sidebarCollapsed ? (
                <SidebarExpandIcon />
              ) : (
                <SidebarCollapseIcon />
              )}
            </button>
            <nav className="app-breadcrumb" aria-label={t("common.navigation")}>
              <span className="breadcrumb-item">{currentRouteLabel}</span>
            </nav>
          </div>

          <div className="navbar-right">
            <Button
              variant="ghost"
              size="sm"
              iconOnly
              onClick={() => void handleRefresh()}
              disabled={refreshing}
              title={t("header.refresh_all")}
              aria-label={t("header.refresh_all")}
            >
              <IconRefreshCw
                size={16}
                className={refreshing ? styles.refreshing : ""}
              />
            </Button>

            <div
              className={`language-menu ${languageMenuOpen ? "open" : ""}`}
              ref={languageMenuRef}
            >
              <Button
                variant="ghost"
                size="sm"
                iconOnly
                onClick={() => {
                  closeOtherMenus("language");
                  setLanguageMenuOpen((open) => !open);
                }}
                title={t("language.switch")}
                aria-label={t("language.switch")}
                aria-haspopup="menu"
                aria-expanded={languageMenuOpen}
              >
                <IconLanguages size={16} />
              </Button>
              {languageMenuOpen ? (
                <div
                  className="notification entering language-menu-popover"
                  role="menu"
                >
                  {LANGUAGE_ORDER.map((item) => (
                    <button
                      key={item}
                      type="button"
                      className={`language-menu-option ${language === item ? "active" : ""}`}
                      onClick={() => handleLanguageSelect(item)}
                      role="menuitemradio"
                      aria-checked={language === item}
                    >
                      <span>{t(LANGUAGE_LABEL_KEYS[item])}</span>
                    </button>
                  ))}
                </div>
              ) : null}
            </div>

            <div
              className={`theme-menu ${themeMenuOpen ? "open" : ""}`}
              ref={themeMenuRef}
            >
              <Button
                variant="ghost"
                size="sm"
                iconOnly
                onClick={() => {
                  closeOtherMenus("theme");
                  setThemeMenuOpen((open) => !open);
                }}
                title={t("theme.switch")}
                aria-label={t("theme.switch")}
                aria-haspopup="menu"
                aria-expanded={themeMenuOpen}
              >
                {theme === "auto" ? (
                  <AutoThemeIcon />
                ) : theme === "dark" ? (
                  <IconMoon size={16} />
                ) : (
                  <IconSun size={16} />
                )}
              </Button>
              {themeMenuOpen ? (
                <div
                  className="notification entering theme-menu-popover"
                  role="menu"
                >
                  {themeOptions.map((option) => (
                    <button
                      key={option.key}
                      type="button"
                      className={`theme-option ${theme === option.key ? "active" : ""}`}
                      onClick={() => {
                        setTheme(option.key);
                        setThemeMenuOpen(false);
                      }}
                      role="menuitemradio"
                      aria-checked={theme === option.key}
                      title={t(option.labelKey)}
                      aria-label={t(option.labelKey)}
                    >
                      <span className="theme-option-icon">{option.icon}</span>
                    </button>
                  ))}
                </div>
              ) : null}
            </div>

            <div
              className={`visual-effects-menu ${visualEffectsMenuOpen ? "open" : ""}`}
              ref={visualEffectsMenuRef}
            >
              <Button
                variant="ghost"
                size="sm"
                iconOnly
                onClick={() => {
                  closeOtherMenus("effects");
                  setVisualEffectsMenuOpen((open) => !open);
                }}
                title={t("visual_effects.switch")}
                aria-label={t("visual_effects.switch")}
                aria-haspopup="menu"
                aria-expanded={visualEffectsMenuOpen}
              >
                <EffectsIcon full={visualEffectsMode === "full"} />
              </Button>
              {visualEffectsMenuOpen ? (
                <div
                  className="notification entering visual-effects-menu-popover"
                  role="menu"
                >
                  {visualEffectsOptions.map((option) => (
                    <button
                      key={option.key}
                      type="button"
                      className={`visual-effects-option ${
                        visualEffectsMode === option.key ? "active" : ""
                      }`}
                      onClick={() => {
                        setVisualEffectsMode(option.key);
                        setVisualEffectsMenuOpen(false);
                      }}
                      role="menuitemradio"
                      aria-checked={visualEffectsMode === option.key}
                    >
                      <span className="visual-effects-option-icon">
                        {option.icon}
                      </span>
                      <span className="visual-effects-option-label">
                        {t(option.labelKey)}
                      </span>
                    </button>
                  ))}
                </div>
              ) : null}
            </div>

            {onLogout ? (
              <Button
                variant="ghost"
                size="sm"
                iconOnly
                onClick={() => void handleLogout()}
                disabled={loggingOut}
                title={t("header.logout")}
                aria-label={t("header.logout")}
              >
                <LogoutIcon />
              </Button>
            ) : null}
          </div>
        </div>
      </header>

      <div className="main-body">
        <button
          type="button"
          className={`sidebar-backdrop ${sidebarOpen ? "visible" : ""}`}
          onClick={() => setSidebarOpen(false)}
          aria-label={t("common.close")}
          aria-hidden={!sidebarOpen}
          tabIndex={sidebarOpen ? 0 : -1}
        />

        <aside
          className={`sidebar ${sidebarOpen ? "open" : ""} ${sidebarCollapsed ? "collapsed" : ""}`}
        >
          <div className="sidebar-brand" title="CPA Manager Plus">
            <div className="sidebar-brand-main">
              <img
                src={CPAMP_SYMBOL_COLOR_PNG_URL}
                alt={showSidebarLabels ? "" : "CPA Manager Plus"}
                className="sidebar-brand-symbol"
              />
              {showSidebarLabels ? (
                <>
                  <img
                    src={CPAMP_WORDMARK_COLOR_PNG_URL}
                    alt="CPA Manager Plus"
                    className="sidebar-brand-wordmark sidebar-brand-wordmark-light"
                  />
                  <img
                    src={CPAMP_WORDMARK_ON_DARK_PNG_URL}
                    alt="CPA Manager Plus"
                    className="sidebar-brand-wordmark sidebar-brand-wordmark-dark"
                  />
                </>
              ) : null}
            </div>
          </div>

          <div className="nav-section">
            <div className="nav-menu-section">
              {navItems.map((item) => (
                <NavLink
                  key={item.path}
                  to={item.path}
                  end={item.path === "/"}
                  className={({ isActive }) =>
                    `nav-item ${isActive ? "active" : ""}`
                  }
                  onClick={() => setSidebarOpen(false)}
                  title={navLabel(item)}
                >
                  <span className="nav-icon">{item.icon}</span>
                  {showSidebarLabels ? (
                    <span className="nav-label">{navLabel(item)}</span>
                  ) : null}
                </NavLink>
              ))}
            </div>
          </div>
        </aside>

        <div className="content" ref={contentRef}>
          <main className="main-content">
            <DatabaseMaintenanceBanner />
            <ViewerModelPriceNotice
              enabled={["/", "/usage-analytics", "/monitoring"].includes(location.pathname)}
              refreshKey={priceRefreshKey}
            />
            <Outlet />
          </main>
        </div>
      </div>
      </div>
    </DatabaseMaintenanceContext.Provider>
  );
}

function AutoThemeIcon() {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      aria-hidden="true"
    >
      <rect x="4" y="5" width="16" height="11" rx="2" />
      <path d="M8 21h8M12 16v5M9 11a3 3 0 0 1 5.2-2M14.5 7v2h-2M15 11a3 3 0 0 1-5.2 2M9.5 15v-2h2" />
    </svg>
  );
}

function EffectsIcon({ full = false }: { full?: boolean }) {
  return full ? (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      aria-hidden="true"
    >
      <path d="m12 3 1.85 5.15L19 10l-5.15 1.85L12 17l-1.85-5.15L5 10l5.15-1.85L12 3zM5 3v4M3 5h4M19 17v4M17 19h4" />
    </svg>
  ) : (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      aria-hidden="true"
    >
      <path d="M4 14a8 8 0 0 1 16 0M12 14l4-5M8 14h8M5 19h14" />
    </svg>
  );
}

function CloseIcon() {
  return (
    <svg
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      aria-hidden="true"
    >
      <path d="M18 6 6 18M6 6l12 12" />
    </svg>
  );
}

function SidebarCollapseIcon() {
  return (
    <svg
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      aria-hidden="true"
    >
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M9 4v16m7-11-3 3 3 3" />
    </svg>
  );
}

function SidebarExpandIcon() {
  return (
    <svg
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      aria-hidden="true"
    >
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M9 4v16m4-11 3 3-3 3" />
    </svg>
  );
}

function LogoutIcon() {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      aria-hidden="true"
    >
      <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4m7 14 5-5-5-5m5 5H9" />
    </svg>
  );
}
