import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/Button";
import {
  IconExternalLink,
  IconRefreshCw,
  IconSatellite,
  IconSettings,
  IconTimer,
} from "@/components/ui/icons";
import { buildDashboardVersionReleaseURL } from "@/features/dashboard/versionReleaseLinks";
import styles from "./VersionCard.module.scss";

export type VersionHealthTone = "ok" | "warn" | "error" | "muted";

export interface VersionBadge {
  label: string;
  className: string;
  releaseUrl?: string;
}

export interface VersionHealthItem {
  label: string;
  value: string;
  tone: VersionHealthTone;
  icon: ReactNode;
  to?: string;
}

interface VersionCardSurfaceProps {
  appVersion: string;
  apiVersion: string;
  cpaBase: string;
  serverBuildDate?: string | number;
  appBadge?: VersionBadge | null;
  apiBadge?: VersionBadge | null;
  checkingAppVersion?: boolean;
  checkingApiVersion?: boolean;
  healthItems: VersionHealthItem[];
  onAppVersionCheck?: () => void | Promise<void>;
  onApiVersionCheck?: () => void | Promise<void>;
  children?: ReactNode;
}

export function VersionCardSurface({
  appVersion,
  apiVersion,
  cpaBase,
  serverBuildDate,
  appBadge,
  apiBadge,
  checkingAppVersion = false,
  checkingApiVersion = false,
  healthItems,
  onAppVersionCheck,
  onApiVersionCheck,
  children,
}: VersionCardSurfaceProps) {
  const { t, i18n } = useTranslation();
  const buildTimeDisplay = serverBuildDate
    ? new Date(serverBuildDate).toLocaleString(i18n.language)
    : t("dashboard.version_unknown");
  const appReleaseUrl = buildDashboardVersionReleaseURL("manager", appVersion);
  const apiReleaseUrl = buildDashboardVersionReleaseURL("core", apiVersion);
  const renderVersionValue = (value: string, releaseUrl: string) =>
    releaseUrl ? (
      <a
        className={styles.versionLink}
        href={releaseUrl}
        target="_blank"
        rel="noopener noreferrer"
      >
        <span className={styles.value}>{value}</span>
        <IconExternalLink size={12} />
      </a>
    ) : (
      <span className={styles.value}>{value}</span>
    );
  const renderBadgeValue = (badge: VersionBadge | null | undefined) => {
    if (!badge) return null;
    const className = `${styles.badge} ${badge.className}`;
    return badge.releaseUrl ? (
      <a
        className={className}
        href={badge.releaseUrl}
        target="_blank"
        rel="noopener noreferrer"
      >
        {badge.label}
      </a>
    ) : (
      <span className={className}>{badge.label}</span>
    );
  };

  return (
    <div className={styles.container}>
      <section className={styles.section}>
        <h2 className={styles.heading}>{t("dashboard.system_overview")}</h2>
        <div className={`${styles.grid} ${styles.systemGrid}`}>
          <div className={styles.item}>
            <div className={styles.icon}>
              <IconSettings size={18} />
            </div>
            <div className={styles.content}>
              <div className={styles.versionHeader}>
                <div className={styles.label}>{t("dashboard.app_version")}</div>
                <Button
                  type="button"
                  variant="ghost"
                  size="xs"
                  iconOnly
                  className={styles.versionAction}
                  onClick={(event) => {
                    event.stopPropagation();
                    void onAppVersionCheck?.();
                  }}
                  onKeyDown={(event) => event.stopPropagation()}
                  loading={checkingAppVersion}
                  title={t("system_info.version_check_button")}
                  aria-label={t("system_info.version_check_button")}
                >
                  {!checkingAppVersion && <IconRefreshCw size={14} />}
                </Button>
              </div>
              <div className={styles.valueWrap}>
                {renderVersionValue(
                  appVersion || t("dashboard.version_unknown"),
                  appReleaseUrl,
                )}
                {renderBadgeValue(appBadge)}
              </div>
            </div>
          </div>

          <div className={styles.item}>
            <div className={styles.icon}>
              <IconSatellite size={18} />
            </div>
            <div className={styles.content}>
              <div className={styles.versionHeader}>
                <div className={styles.label}>{t("dashboard.api_version")}</div>
                <Button
                  type="button"
                  variant="ghost"
                  size="xs"
                  iconOnly
                  className={styles.versionAction}
                  onClick={() => void onApiVersionCheck?.()}
                  loading={checkingApiVersion}
                  title={t("system_info.version_check_button")}
                  aria-label={t("system_info.version_check_button")}
                >
                  {!checkingApiVersion && <IconRefreshCw size={14} />}
                </Button>
              </div>
              <div className={styles.valueWrap}>
                {renderVersionValue(
                  apiVersion || t("dashboard.version_unknown"),
                  apiReleaseUrl,
                )}
                {renderBadgeValue(apiBadge)}
              </div>
            </div>
          </div>

          <div className={styles.item}>
            <div className={styles.icon}>
              <IconTimer size={18} />
            </div>
            <div className={styles.content}>
              <div className={styles.label}>{t("dashboard.build_time")}</div>
              <div className={styles.value}>{buildTimeDisplay}</div>
            </div>
          </div>

          <div className={styles.item}>
            <div className={styles.icon}>
              <IconExternalLink size={18} />
            </div>
            <div className={styles.content}>
              <div className={styles.label}>{t("dashboard.cpa_base")}</div>
              <div className={styles.value}>{cpaBase || "-"}</div>
            </div>
          </div>
        </div>
      </section>

      <section className={styles.section}>
        <h2 className={styles.heading}>{t("dashboard.health_status")}</h2>
        <div className={`${styles.grid} ${styles.healthGrid}`}>
          {healthItems.map((item) => {
            const content = (
              <>
                <div className={`${styles.healthIcon} ${styles[item.tone]}`}>
                  {item.icon}
                </div>
                <div className={styles.content}>
                  <div className={styles.label}>{item.label}</div>
                  <div
                    className={`${styles.value} ${styles[`${item.tone}Text`]}`}
                  >
                    {item.value}
                  </div>
                </div>
              </>
            );

            return item.to ? (
              <Link
                key={item.label}
                to={item.to}
                className={`${styles.healthItem} ${styles.healthLink}`}
              >
                {content}
              </Link>
            ) : (
              <div key={item.label} className={styles.healthItem}>
                {content}
              </div>
            );
          })}
        </div>
      </section>
      {children}
    </div>
  );
}
