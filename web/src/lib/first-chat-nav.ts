import { getAgents } from "@/lib/api";
import { firstAgent, firstAgentChatPath } from "@/lib/first-chat";

export async function resolveFirstChatPath(): Promise<string> {
  try {
    return firstAgentChatPath(await getAgents()) ?? "/overview/";
  } catch {
    return "/overview/";
  }
}

export async function resolveFirstAgentSubpath(sub: string): Promise<string> {
  try {
    const agent = firstAgent(await getAgents());
    if (agent?.id) {
      return `/agents/${encodeURIComponent(agent.id)}/${sub}/`;
    }
  } catch {
    /* fall through */
  }
  return "/overview/";
}
