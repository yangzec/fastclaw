import { listProviders, type ProviderRow, type ScopeName } from "@/lib/api";
import { collectChatModels, type ChatModelOption } from "@/lib/chat-models";

export async function loadChatModels(
  userId: string,
  agentId?: string,
): Promise<ChatModelOption[]> {
  const asRows = (data: unknown): ProviderRow[] => {
    const providers = (data as { providers?: ProviderRow[] } | null)?.providers;
    return Array.isArray(providers) ? providers : [];
  };
  const [agentRows, userRows, systemRows] = await Promise.all([
    agentId
      ? listProviders("agent" as ScopeName, agentId).then(asRows).catch(() => [])
      : Promise.resolve([]),
    listProviders("user", userId).then(asRows).catch(() => []),
    listProviders("system", "").then(asRows).catch(() => []),
  ]);
  return collectChatModels(agentRows, userRows, systemRows);
}
