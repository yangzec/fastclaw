export type ChatModelOption = {
  value: string;
  provider: string;
  id: string;
  name: string;
  contextWindow: number;
};

type Modelish = {
  id?: string;
  name?: string;
  contextWindow?: number;
};

type Providerish = {
  name?: string;
  models?: Modelish[];
};

export function shortModelLabel(model: string): string {
  const trimmed = model.trim();
  if (!trimmed) return "Model";
  const slash = trimmed.lastIndexOf("/");
  return slash >= 0 ? trimmed.slice(slash + 1) : trimmed;
}

function optionFrom(provider: string, m: Modelish): ChatModelOption | null {
  const id = (m.id || "").trim();
  if (!id) return null;
  return {
    value: `${provider}/${id}`,
    provider,
    id,
    name: (m.name || id).trim() || id,
    contextWindow: Number(m.contextWindow) || 0,
  };
}

/** Deduped provider/model list. Agent rows win, then user, then system. */
export function collectChatModels(
  agentRows: Providerish[],
  userRows: Providerish[],
  systemRows: Providerish[],
): ChatModelOption[] {
  const seen = new Set<string>();
  const out: ChatModelOption[] = [];
  for (const row of [...agentRows, ...userRows, ...systemRows]) {
    const provider = (row.name || "").trim();
    if (!provider) continue;
    for (const m of row.models || []) {
      const opt = optionFrom(provider, m);
      if (!opt || seen.has(opt.value)) continue;
      seen.add(opt.value);
      out.push(opt);
    }
  }
  return out;
}
