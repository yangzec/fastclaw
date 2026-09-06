/** Human title for a tool category. Prefer catalog Label; never glue name+"tool". */
export function categoryDisplayName(category: {
  label?: string;
  name: string;
}): string {
  const label = category.label?.trim();
  if (label) return label;
  return humanizeToolName(category.name);
}

/** "web_search" → "Web Search". Used only when Label is missing. */
export function humanizeToolName(name: string): string {
  return name
    .split(/[_-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

export function isProviderConfigured(
  provider: { name: string; needsKey?: boolean; needsUrl?: boolean },
  settings?: { apiKey?: string; endpoint?: string } | null,
): boolean {
  if (provider.name === "none") return false;
  const hasKey = Boolean(settings?.apiKey?.trim());
  const hasUrl = Boolean(settings?.endpoint?.trim());
  if (provider.needsKey && hasKey) return true;
  if (provider.needsUrl && hasUrl) return true;
  if (!provider.needsKey && !provider.needsUrl) return true;
  return false;
}

/** `provider/model` chain ref, or null when no model can be chosen yet. */
export function chainRefFor(
  provider: { name: string; models?: string[] },
  settings?: { options?: Record<string, string> } | null,
): string | null {
  const typed = settings?.options?.model?.trim();
  const fallback = provider.models?.[0] || "";
  const model = typed || fallback;
  if (!model) return null;
  return `${provider.name}/${model}`;
}

export function emptyChainMessage(hasConfiguredProvider: boolean): string {
  return hasConfiguredProvider
    ? "Configured providers aren't in the fallback chain yet — add one below."
    : "Add a provider key above, then add it to the fallback chain.";
}
