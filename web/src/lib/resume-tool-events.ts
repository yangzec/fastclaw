import type { ToolResultMetadata } from "@/lib/api";

export type ResumeToolCall = {
  id: string;
  name: string;
  arguments: string;
  result?: string;
  metadata?: ToolResultMetadata;
};

export type ResumeChatMessage = {
  id: string;
  role: string;
  content: string;
  timestamp: number;
  toolCalls?: ResumeToolCall[];
};

type ToolCallPayload = {
  id?: string;
  name?: string;
  arguments?: string;
};

type ToolResultPayload = {
  id?: string;
  name?: string;
  result?: string;
  metadata?: ToolResultMetadata;
};

function newResumeGroup(calls: ResumeToolCall[]): ResumeChatMessage {
  return {
    id: `tg-resume-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
    role: "tool-group",
    content: "",
    timestamp: Date.now(),
    toolCalls: calls,
  };
}

/** Merge a live/replayed tool_call into the chat after a page reload. */
export function applyToolCallEvent<T extends ResumeChatMessage>(
  prev: T[],
  data: ToolCallPayload,
): T[] {
  const id = data.id || "";
  const nextCall: ResumeToolCall = {
    id,
    name: data.name || "",
    arguments: data.arguments || "{}",
  };
  if (id && prev.some((m) => m.role === "tool-group" && m.toolCalls?.some((c) => c.id === id))) {
    return prev;
  }
  const last = prev[prev.length - 1];
  if (last?.role === "tool-group" && last.toolCalls) {
    const allResolved = last.toolCalls.every((c) => c.result !== undefined);
    if (!allResolved) {
      const updated = [...prev];
      updated[updated.length - 1] = {
        ...last,
        toolCalls: [...last.toolCalls, nextCall],
      };
      return updated;
    }
  }
  return [...prev, newResumeGroup([nextCall]) as T];
}

/** Merge a live/replayed tool_result into the chat after a page reload. */
export function applyToolResultEvent<T extends ResumeChatMessage>(
  prev: T[],
  data: ToolResultPayload,
): T[] {
  const id = data.id || "";
  const result = data.result || "";
  const meta = data.metadata;
  let found = false;
  const next = prev.map((m) => {
    if (m.role !== "tool-group" || !m.toolCalls) return m;
    const idx = m.toolCalls.findIndex((c) => c.id === id);
    if (idx < 0) return m;
    found = true;
    return {
      ...m,
      toolCalls: m.toolCalls.map((c, i) =>
        i === idx ? { ...c, result, metadata: meta ?? c.metadata } : c,
      ),
    };
  });
  if (found) return next;
  return [
    ...prev,
    newResumeGroup([
      {
        id,
        name: data.name || "",
        arguments: "{}",
        result,
        metadata: meta,
      },
    ]) as T,
  ];
}

export function hasRunningTools<T extends { role: string; toolCalls?: { result?: string }[] }>(
  messages: T[],
): boolean {
  return messages.some(
    (m) =>
      m.role === "tool-group" &&
      m.toolCalls?.some((c) => c.result === undefined),
  );
}
