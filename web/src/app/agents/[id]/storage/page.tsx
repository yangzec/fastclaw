"use client";

import { StorageSettingsForm } from "@/components/storage-settings-form";
import { useAgentIdFromURL } from "@/hooks/use-agent-id";

export default function AgentStoragePage() {
  const agentId = useAgentIdFromURL();
  return <StorageSettingsForm scope="agent" agentId={agentId} />;
}
