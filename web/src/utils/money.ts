/**
 * Money helpers (ADR 0019): every amount on the wire is an int64 counting
 * the currency's minor units (fen for CNY). Formatting to yuan happens
 * only here, at the display edge — and always with integer math, never
 * floats.
 */

/** fenToYuan renders minor units as a "12.34" yuan string. */
export function fenToYuan(fen: number | undefined | null): string {
  const value = fen ?? 0;
  const sign = value < 0 ? "-" : "";
  const abs = Math.abs(value);
  return `${sign}${Math.floor(abs / 100)}.${String(abs % 100).padStart(2, "0")}`;
}

/** yuanToFen parses a user-typed yuan string back into minor units. */
export function yuanToFen(yuan: string): number {
  const [whole = "0", frac = ""] = String(yuan).trim().split(".");
  const two = `${frac}00`.slice(0, 2);
  return Number(whole || "0") * 100 + Number(two || "0");
}

/** Pay Method (支付方式) — Native is the only MVP method. */
export const PAY_METHOD_NATIVE = 1;

/** Channel codes and labels — mirrors internal/domain/pay/entity. */
export const CHANNEL_LABELS: Record<number, string> = {
  1: "微信支付",
  2: "支付宝",
  3: "云闪付",
  4: "Stripe",
};

/** Payment status vocabulary — mirrors pay_payment.status. */
export const PAYMENT_STATUS: Record<number, { label: string; color: string }> = {
  1: { label: "待支付", color: "processing" },
  2: { label: "已支付", color: "success" },
  3: { label: "失败", color: "error" },
  4: { label: "已关闭", color: "default" },
  5: { label: "已过期", color: "warning" },
};

/** Refund status vocabulary — mirrors pay_refund.status. */
export const REFUND_STATUS: Record<number, { label: string; color: string }> = {
  1: { label: "退款中", color: "processing" },
  2: { label: "退款成功", color: "success" },
  3: { label: "退款失败", color: "error" },
};

/** Merchant account status — RuoYi convention (0 enabled, 1 disabled). */
export const MERCHANT_STATUS: Record<number, { label: string; color: string }> = {
  0: { label: "启用", color: "success" },
  1: { label: "停用", color: "error" },
};
