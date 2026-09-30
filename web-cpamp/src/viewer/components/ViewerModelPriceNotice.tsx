import { useLanguageStore } from "@/stores/useLanguageStore";
import type { Language } from "@/types/common";
import { useViewerModelPriceStatus } from "@/viewer/hooks/useViewerModelPriceStatus";
import styles from "./ViewerModelPriceNotice.module.scss";

type NoticeCopy = {
  summary: (count: number, formatted: string) => string;
  details: string;
  stale: string;
  truncated: (shown: string, total: string) => string;
  checked: string;
};

const copy: Record<Language, NoticeCopy> = {
  "zh-CN": {
    summary: (_count, formatted) =>
      `${formatted} 个模型未配置价格，费用统计可能不完整。`,
    details: "查看模型",
    stale: "显示上次检查结果，当前价格状态可能已变化。",
    truncated: (shown, total) => `仅展示前 ${shown} 个模型（共 ${total} 个）。`,
    checked: "上次检查",
  },
  "zh-TW": {
    summary: (_count, formatted) =>
      `${formatted} 個模型尚未設定價格，費用統計可能不完整。`,
    details: "查看模型",
    stale: "顯示上次檢查結果，目前價格狀態可能已變更。",
    truncated: (shown, total) => `僅顯示前 ${shown} 個模型（共 ${total} 個）。`,
    checked: "上次檢查",
  },
  en: {
    summary: (count, formatted) =>
      `${formatted} ${count === 1 ? "model has" : "models have"} no configured price. Cost statistics may be incomplete.`,
    details: "View models",
    stale: "Showing the last check. Current pricing status may have changed.",
    truncated: (shown, total) =>
      `Showing only the first ${shown} of ${total} models.`,
    checked: "Last checked",
  },
  ru: {
    summary: (_count, formatted) =>
      `Моделей без настроенной цены: ${formatted}. Статистика расходов может быть неполной.`,
    details: "Показать модели",
    stale:
      "Показан результат последней проверки. Текущее состояние цен могло измениться.",
    truncated: (shown, total) =>
      `Показаны только первые ${shown} из ${total} моделей.`,
    checked: "Последняя проверка",
  },
};

export function ViewerModelPriceNotice({
  enabled,
  refreshKey = 0,
}: {
  enabled: boolean;
  refreshKey?: number;
}) {
  const language = useLanguageStore((state) => state.language);
  const { data } = useViewerModelPriceStatus(enabled, refreshKey);

  if (
    !enabled ||
    !data?.available ||
    !Number.isFinite(data.unpriced_count) ||
    data.unpriced_count <= 0
  ) {
    return null;
  }

  const text = copy[language];
  const models = Array.isArray(data.unpriced_models)
    ? data.unpriced_models
        .filter((model) => typeof model === "string" && model.trim())
        .slice(0, 200)
    : [];
  const count = data.unpriced_count.toLocaleString(language);
  const checked = data.checked_at_ms ? new Date(data.checked_at_ms) : null;
  const checkedIsValid = checked && Number.isFinite(checked.getTime());

  return (
    <aside className={styles.notice}>
      <span className={styles.icon} aria-hidden="true">
        !
      </span>
      <div className={styles.content}>
        <p className={styles.summary} role="status" aria-live="polite">
          {text.summary(data.unpriced_count, count)}
        </p>
        {data.stale && <p className={styles.note}>{text.stale}</p>}
        {models.length > 0 && (
          <details className={styles.details}>
            <summary>{text.details}</summary>
            <ul className={styles.models}>
              {models.map((model, index) => (
                <li key={`${index}:${model}`}>{model}</li>
              ))}
            </ul>
            {data.unpriced_count > models.length && (
              <p className={styles.note}>
                {text.truncated(models.length.toLocaleString(language), count)}
              </p>
            )}
            {checkedIsValid && (
              <p className={styles.note}>
                {text.checked}:{" "}
                <time dateTime={checked.toISOString()}>
                  {checked.toLocaleString(language)}
                </time>
              </p>
            )}
          </details>
        )}
      </div>
    </aside>
  );
}
