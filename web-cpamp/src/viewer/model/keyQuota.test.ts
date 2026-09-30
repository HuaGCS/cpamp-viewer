import { describe, expect, it } from "vitest";
import {
  formatQuotaPercent,
  formatQuotaTime,
  formatQuotaUSD,
  hasQuotaAmounts,
  keyQuotaCopy,
  quotaTone,
} from "./keyQuota";

describe("Key quota display", () => {
  it("preserves small balances and never labels a consumed quota as 100%", () => {
    expect(formatQuotaUSD(0)).toBe("$0.00");
    expect(formatQuotaUSD(0.0001)).toBe("<$0.01");
    expect(formatQuotaUSD(0.01)).toBe("$0.01");
    expect(formatQuotaUSD(800)).toBe("$800.00");
    expect(formatQuotaPercent(0)).toBe("0%");
    expect(formatQuotaPercent(0.001)).toBe("<1%");
    expect(formatQuotaPercent(1)).toBe("1%");
    expect(formatQuotaPercent(99.999999999999)).toBe("99%");
    expect(formatQuotaPercent(100, true)).toBe("99%");
    expect(formatQuotaPercent(100)).toBe("100%");
  });

  it("matches fork14 thresholds with floating point noise removed", () => {
    expect(quotaTone(0)).toBe("danger");
    expect(quotaTone(4.99)).toBe("danger");
    expect(quotaTone(4.999999999999999)).toBe("warning");
    expect(quotaTone(5)).toBe("warning");
    expect(quotaTone(20)).toBe("warning");
    expect(quotaTone(20.000000000000004)).toBe("warning");
    expect(quotaTone(20.01)).toBe("success");
  });

  it("does not turn unavailable or malformed data into a zero balance", () => {
    expect(hasQuotaAmounts({ id: "view_test", name: "Test", state: "active" })).toBe(false);
    expect(hasQuotaAmounts({
      id: "view_test", name: "Test", state: "active",
      weekly_limit_usd: 800, remaining_usd: 0, remaining_percent: 0,
    })).toBe(true);
    expect(hasQuotaAmounts({
      id: "view_test", name: "Test", state: "inactive",
      weekly_limit_usd: 800, remaining_usd: 0, remaining_percent: 0,
    })).toBe(false);
    expect(hasQuotaAmounts({
      id: "view_test", name: "Test", state: "active",
      weekly_limit_usd: 800, remaining_usd: Number.NaN, remaining_percent: 0,
    })).toBe(false);
    expect(formatQuotaTime("not-a-date", "en")).toBe("");
    expect(formatQuotaTime(undefined, "zh-CN")).toBe("");
  });

  it("provides Chinese and English with an English fallback", () => {
    expect(keyQuotaCopy("zh-CN").inactive).toBe("额度策略未启用");
    expect(keyQuotaCopy("en").title).toBe("Key quota");
    expect(keyQuotaCopy("ru").title).toBe("Key quota");
  });
});
