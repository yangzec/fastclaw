// Codex-style follow-up: while a turn is running, Enter queues by
// default and the user can opt into same-turn steer ("插入").

export type FollowupBehavior = "queue" | "steer";

export type QueuedFollowup = {
  id: string;
  text: string;
};

const BEHAVIOR_KEY = "fastclaw.followupBehavior";
const QUEUE_KEY_PREFIX = "fastclaw.followupQueue.";

export function loadFollowupBehavior(): FollowupBehavior {
  if (typeof window === "undefined") return "queue";
  return window.localStorage.getItem(BEHAVIOR_KEY) === "steer" ? "steer" : "queue";
}

export function saveFollowupBehavior(mode: FollowupBehavior): void {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(BEHAVIOR_KEY, mode);
}

export function newFollowupId(): string {
  return `q-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
}

function queueStorageKey(agentId: string, sessionId: string): string {
  return `${QUEUE_KEY_PREFIX}${agentId}\t${sessionId}`;
}

export function loadSessionQueue(agentId: string, sessionId: string): QueuedFollowup[] {
  if (typeof window === "undefined" || !agentId || !sessionId) return [];
  try {
    const raw = window.sessionStorage.getItem(queueStorageKey(agentId, sessionId));
    if (!raw) return [];
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((item): item is QueuedFollowup =>
      !!item && typeof item === "object" && typeof item.id === "string" && typeof item.text === "string",
    );
  } catch {
    return [];
  }
}

export function saveSessionQueue(agentId: string, sessionId: string, items: QueuedFollowup[]): void {
  if (typeof window === "undefined" || !agentId || !sessionId) return;
  const key = queueStorageKey(agentId, sessionId);
  if (items.length === 0) {
    window.sessionStorage.removeItem(key);
    return;
  }
  window.sessionStorage.setItem(key, JSON.stringify(items));
}

// resolveFollowupAction picks queue vs steer for one composer submit.
// `flip` is Ctrl/Cmd+Enter — the opposite of the saved default, matching
// Codex (default from settings, modifier switches this message).
export function resolveFollowupAction(
  behavior: FollowupBehavior,
  flip: boolean,
): FollowupBehavior {
  if (flip) {
    return behavior === "queue" ? "steer" : "queue";
  }
  return behavior;
}

// Phones treat Enter as newline and have no ⌘/Ctrl+Enter, so the
// composer hint points at the on-screen 排队 / 插入 buttons instead.
export function followupComposerHint(behavior: FollowupBehavior, isMobile: boolean): string {
  if (isMobile) return "输入后点排队或插入";
  return behavior === "queue"
    ? "Enter 排队 · ⌘/Ctrl+Enter 插入当前回合"
    : "Enter 插入当前回合 · ⌘/Ctrl+Enter 排队";
}
