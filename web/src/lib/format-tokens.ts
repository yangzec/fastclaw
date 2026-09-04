/** Compact token counts for the composer meter: 999, 1.2k, 12k, 1M, 1.05M. */
export function formatTokenCount(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "0";
  if (n >= 1_000_000) {
    const m = n / 1_000_000;
    return (Number.isInteger(m) ? String(m) : m.toFixed(2).replace(/\.?0+$/, "")) + "M";
  }
  if (n >= 10_000) return Math.round(n / 1000) + "k";
  if (n >= 1000) {
    const k = n / 1000;
    return (Number.isInteger(k) ? String(k) : k.toFixed(1).replace(/\.0$/, "")) + "k";
  }
  return String(Math.round(n));
}

export function estimateDraftTokens(text: string): number {
  if (!text) return 0;
  return Math.ceil(text.length / 4);
}
