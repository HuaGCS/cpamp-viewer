import { useEffect, useRef, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import {
  CPAMP_HORIZONTAL_LOGO_ON_DARK_PNG_SRC_SET,
  CPAMP_HORIZONTAL_LOGO_ON_DARK_PNG_URL,
  CPAMP_HORIZONTAL_LOGO_PNG_SRC_SET,
  CPAMP_HORIZONTAL_LOGO_PNG_URL,
} from "@/assets/brand";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import {
  IconEye,
  IconEyeOff,
  IconLanguages,
  IconMoon,
  IconShield,
  IconSun,
} from "@/components/ui/icons";
import { useLanguageStore } from "@/stores/useLanguageStore";
import { useThemeStore } from "@/stores/useThemeStore";
import type { Language } from "@/types/common";
import { LANGUAGE_LABEL_KEYS, LANGUAGE_ORDER } from "@/utils/constants";
import { isSupportedLanguage } from "@/utils/language";
import { viewerApi, ViewerApiError } from "./viewerApi";
import upstreamStyles from "@/features/login/LoginPage.module.scss";
import styles from "./ViewerLoginPage.module.scss";

type ViewerLoginCopy = {
  subtitle: string;
  passwordLabel: string;
  passwordPlaceholder: string;
  passwordHint: string;
  readonlyTitle: string;
  readonlyBody: string;
  required: string;
  invalid: string;
  unavailable: string;
};

const LOGIN_COPY: Record<Language, ViewerLoginCopy> = {
  "zh-CN": {
    subtitle: "使用 Viewer 密码登录只读观察台",
    passwordLabel: "Viewer 访问密码",
    passwordPlaceholder: "请输入 Viewer 访问密码",
    passwordHint: "该密码只用于 Viewer 登录，不是 CPAMP 管理密钥。",
    readonlyTitle: "只读连接",
    readonlyBody: "页面只读取仪表盘、用量、请求监控和配额数据。",
    required: "请输入 Viewer 访问密码",
    invalid: "密码错误或会话已失效",
    unavailable: "Viewer 服务暂时不可用，请稍后重试",
  },
  "zh-TW": {
    subtitle: "使用 Viewer 密碼登入唯讀觀察台",
    passwordLabel: "Viewer 存取密碼",
    passwordPlaceholder: "請輸入 Viewer 存取密碼",
    passwordHint: "此密碼只用於 Viewer 登入，不是 CPAMP 管理金鑰。",
    readonlyTitle: "唯讀連線",
    readonlyBody: "頁面只讀取儀表板、用量、請求監控與配額資料。",
    required: "請輸入 Viewer 存取密碼",
    invalid: "密碼錯誤或工作階段已失效",
    unavailable: "Viewer 服務暫時無法使用，請稍後重試",
  },
  en: {
    subtitle: "Sign in to the read-only Viewer console",
    passwordLabel: "Viewer password",
    passwordPlaceholder: "Enter the Viewer password",
    passwordHint:
      "This password signs in to Viewer; it is not the CPAMP admin key.",
    readonlyTitle: "Read-only connection",
    readonlyBody:
      "Viewer only reads dashboard, usage, monitoring, and quota data.",
    required: "Enter the Viewer password",
    invalid: "Incorrect password or expired session",
    unavailable: "Viewer is temporarily unavailable. Try again later.",
  },
  ru: {
    subtitle: "Вход в консоль Viewer только для чтения",
    passwordLabel: "Пароль Viewer",
    passwordPlaceholder: "Введите пароль Viewer",
    passwordHint:
      "Этот пароль используется только для Viewer, а не для ключа администратора CPAMP.",
    readonlyTitle: "Подключение только для чтения",
    readonlyBody:
      "Viewer читает только панель, статистику, мониторинг запросов и квоты.",
    required: "Введите пароль Viewer",
    invalid: "Неверный пароль или истёкшая сессия",
    unavailable: "Viewer временно недоступен. Повторите попытку позже.",
  },
};

interface ViewerLoginPageProps {
  initialError?: string;
  onAuthenticated: () => void;
}

export function ViewerLoginPage({
  initialError = "",
  onAuthenticated,
}: ViewerLoginPageProps) {
  const { t } = useTranslation();
  const language = useLanguageStore((state) => state.language);
  const setLanguage = useLanguageStore((state) => state.setLanguage);
  const theme = useThemeStore((state) => state.theme);
  const cycleTheme = useThemeStore((state) => state.cycleTheme);
  const copy = LOGIN_COPY[language];
  const languageMenuRef = useRef<HTMLDivElement | null>(null);
  const [languageMenuOpen, setLanguageMenuOpen] = useState(false);
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(initialError);

  useEffect(() => {
    if (!languageMenuOpen) return;

    const closeOnOutsideClick = (event: MouseEvent) => {
      if (!languageMenuRef.current?.contains(event.target as Node)) {
        setLanguageMenuOpen(false);
      }
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setLanguageMenuOpen(false);
    };

    document.addEventListener("mousedown", closeOnOutsideClick);
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.removeEventListener("mousedown", closeOnOutsideClick);
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, [languageMenuOpen]);

  const handleLanguageSelect = (nextLanguage: string) => {
    if (!isSupportedLanguage(nextLanguage)) return;
    setLanguage(nextLanguage);
    setLanguageMenuOpen(false);
  };

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!password) {
      setError(copy.required);
      return;
    }

    setLoading(true);
    setError("");
    try {
      const session = await viewerApi.login(password);
      if (!session.authenticated) {
        setError(copy.invalid);
        return;
      }
      setPassword("");
      onAuthenticated();
    } catch (loginError) {
      if (
        loginError instanceof ViewerApiError &&
        (loginError.status === 401 || loginError.status === 403)
      ) {
        setError(copy.invalid);
      } else {
        setError(
          loginError instanceof Error && loginError.message
            ? loginError.message
            : copy.unavailable,
        );
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className={upstreamStyles.container}>
      <div className={upstreamStyles.toolBar}>
        <div className={upstreamStyles.languageMenu} ref={languageMenuRef}>
          <button
            type="button"
            className={upstreamStyles.toolButton}
            onClick={() => setLanguageMenuOpen((open) => !open)}
            title={t("language.switch")}
            aria-label={t("language.switch")}
            aria-haspopup="menu"
            aria-expanded={languageMenuOpen}
          >
            <IconLanguages size={19} />
          </button>
          {languageMenuOpen ? (
            <div className={upstreamStyles.languagePopover} role="menu">
              {LANGUAGE_ORDER.map((item) => (
                <button
                  key={item}
                  type="button"
                  className={`${upstreamStyles.languageOption} ${
                    item === language ? upstreamStyles.languageOptionActive : ""
                  }`}
                  onClick={() => handleLanguageSelect(item)}
                  role="menuitemradio"
                  aria-checked={item === language}
                >
                  {t(LANGUAGE_LABEL_KEYS[item])}
                </button>
              ))}
            </div>
          ) : null}
        </div>
        <button
          type="button"
          className={upstreamStyles.toolButton}
          onClick={cycleTheme}
          title={t("theme.switch")}
          aria-label={t("theme.switch")}
        >
          {theme === "dark" ? <IconMoon size={19} /> : <IconSun size={19} />}
        </button>
      </div>

      <div className={upstreamStyles.formPanel}>
        <div className={upstreamStyles.formContent}>
          <section className={upstreamStyles.loginCard}>
            <div className={upstreamStyles.cardBranding}>
              <img
                src={CPAMP_HORIZONTAL_LOGO_PNG_URL}
                srcSet={CPAMP_HORIZONTAL_LOGO_PNG_SRC_SET}
                alt="CPA Manager Plus"
                className={`${upstreamStyles.brandLogo} ${upstreamStyles.brandLogoLight}`}
              />
              <img
                src={CPAMP_HORIZONTAL_LOGO_ON_DARK_PNG_URL}
                srcSet={CPAMP_HORIZONTAL_LOGO_ON_DARK_PNG_SRC_SET}
                alt="CPA Manager Plus"
                className={`${upstreamStyles.brandLogo} ${upstreamStyles.brandLogoDark}`}
              />
            </div>

            <form className={upstreamStyles.loginForm} onSubmit={handleSubmit}>
              <Input
                label={copy.passwordLabel}
                type={showPassword ? "text" : "password"}
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                placeholder={copy.passwordPlaceholder}
                hint={copy.passwordHint}
                autoComplete="current-password"
                autoFocus
                disabled={loading}
                rightElement={
                  <button
                    type="button"
                    className={styles.passwordAction}
                    onClick={() => setShowPassword((visible) => !visible)}
                    title={
                      showPassword ? t("login.hide_key") : t("login.show_key")
                    }
                    aria-label={
                      showPassword ? t("login.hide_key") : t("login.show_key")
                    }
                  >
                    {showPassword ? (
                      <IconEyeOff size={18} />
                    ) : (
                      <IconEye size={18} />
                    )}
                  </button>
                }
              />

              {error ? (
                <div className={upstreamStyles.errorBox}>{error}</div>
              ) : null}

              <div className={styles.viewerMode}>
                <span className={styles.viewerModeIcon}>
                  <IconShield size={17} />
                </span>
                <span className={styles.viewerModeCopy}>
                  <strong>{copy.readonlyTitle}</strong>
                  <span>{copy.readonlyBody}</span>
                </span>
              </div>

              <Button
                type="submit"
                fullWidth
                loading={loading}
                disabled={loading}
              >
                {loading ? t("login.submitting") : t("login.submit_button")}
              </Button>
            </form>
          </section>
        </div>
      </div>
    </div>
  );
}
