import { getAgents } from "@/lib/api";
import { firstAgentChatPath } from "@/lib/first-chat";

export async function resolveFirstChatPath(): Promise<string> {
  try {
    return firstAgentChatPath(await getAgents()) ?? "/overview/";
  } catch {
    return "/overview/";
  }
}
