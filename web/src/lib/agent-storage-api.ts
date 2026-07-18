import { apiFetch } from "@/lib/api";

export type ObjectStoreSource = "agent" | "user" | "global";

export interface ObjectStoreConfig {
  configured?: boolean;
  enabled: boolean;
  source: ObjectStoreSource;
  type?: string;
  accountId?: string;
  bucket?: string;
  prefix?: string;
  endpoint?: string;
  publicBaseURL?: string;
  useSSL?: boolean;
  hasAccessKey: boolean;
  hasSecretKey: boolean;
}

export type AgentObjectStoreConfig = ObjectStoreConfig;
export type UserObjectStoreConfig = ObjectStoreConfig;

export interface ObjectStoreInput {
  accountId: string;
  bucket: string;
  prefix?: string;
  endpoint?: string;
  publicBaseURL?: string;
  accessKey?: string;
  secretKey?: string;
}

export type AgentObjectStoreInput = ObjectStoreInput;

export async function getAgentObjectStore(agentId: string): Promise<AgentObjectStoreConfig> {
  const res = await apiFetch(`/api/agents/${encodeURIComponent(agentId)}/objectstore`);
  return res.json();
}

export async function testAgentObjectStore(agentId: string, input: ObjectStoreInput): Promise<{ ok: boolean; latencyMs: number }> {
  const res = await apiFetch(`/api/agents/${encodeURIComponent(agentId)}/objectstore/test`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || "Connection test failed");
  return res.json();
}

export async function saveAgentObjectStore(agentId: string, input: ObjectStoreInput): Promise<{ ok: boolean; latencyMs: number; objectstore: AgentObjectStoreConfig }> {
  const res = await apiFetch(`/api/agents/${encodeURIComponent(agentId)}/objectstore`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || "Save failed");
  return res.json();
}

export async function removeAgentObjectStore(agentId: string): Promise<{ ok: boolean; objectstore: AgentObjectStoreConfig }> {
  const res = await apiFetch(`/api/agents/${encodeURIComponent(agentId)}/objectstore`, { method: "DELETE" });
  if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || "Remove failed");
  return res.json();
}

export async function getUserObjectStore(): Promise<UserObjectStoreConfig> {
  const res = await apiFetch("/api/me/objectstore");
  return res.json();
}

export async function testUserObjectStore(input: ObjectStoreInput): Promise<{ ok: boolean; latencyMs: number }> {
  const res = await apiFetch("/api/me/objectstore/test", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || "Connection test failed");
  return res.json();
}

export async function saveUserObjectStore(input: ObjectStoreInput): Promise<{ ok: boolean; latencyMs: number; objectstore: UserObjectStoreConfig }> {
  const res = await apiFetch("/api/me/objectstore", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || "Save failed");
  return res.json();
}

export async function removeUserObjectStore(): Promise<{ ok: boolean; objectstore: UserObjectStoreConfig }> {
  const res = await apiFetch("/api/me/objectstore", { method: "DELETE" });
  if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || "Remove failed");
  return res.json();
}
