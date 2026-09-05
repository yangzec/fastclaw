// Shared workspace-file helpers for the chat "Open files" badge and the
// workspace side panel. Both UIs must count the same artifacts — the
// live turn used to union write_file paths (`notes.md`) with list API
// paths (`sessions/<sid>/notes.md`) and the panel then hid session-root
// uploads behind the template "Changed" filter.

export const SYSTEM_FILES = new Set([
  "SOUL.md", "IDENTITY.md", "USER.md", "BOOTSTRAP.md",
  "MEMORY.md", "KNOWLEDGE.md", "HEARTBEAT.md", "AGENTS.md", "TOOLS.md", "agent.json",
]);

/** Coding-project app root. Keep in sync with internal/runtime.AppSubdir. */
export const APP_SUBDIR = "app";

/** Above this many workspace files, a live git baseline defaults the
 *  panel to "Changed" so a scaffolded template doesn't dump 40 files. */
export const TEMPLATE_NOISE_MIN = 24;

export type WorkspacePath = { path: string; size?: number; modTime?: number };

export function isSystemFile(path: string): boolean {
  return !path.includes("/") && SYSTEM_FILES.has(path);
}

/** Map a write_file/edit_file argument onto a workspace-relative path.
 *  Relative names and /workspace/<name> are the same artifact; /tmp
 *  scratch and identity files are not listed. */
export function workspaceToolPath(path: string): string | null {
  if (!path) return null;
  let p = path;
  if (p.startsWith("/workspace/")) p = p.slice("/workspace/".length);
  else if (p.startsWith("/")) return null;
  if (!p || isSystemFile(p)) return null;
  return p;
}

function stripWorkspacePrefix(path: string): string {
  if (path.startsWith("/workspace/")) return path.slice("/workspace/".length);
  return path;
}

/** True when `a` and `b` name the same workspace artifact under different
 *  conventions (tool arg `notes.md` vs store key `sessions/<sid>/notes.md`).
 *  Two distinct store keys stay distinct — `projects/p/notes.md` is not
 *  `projects/p/s-1/notes.md`. */
export function sameWorkspaceArtifact(a: string, b: string): boolean {
  if (!a || !b) return false;
  if (a === b) return true;
  const na = stripWorkspacePrefix(a);
  const nb = stripWorkspacePrefix(b);
  if (!na || !nb) return false;
  if (na === nb) return true;
  return na.endsWith("/" + nb) || nb.endsWith("/" + na);
}

export function hasWorkspaceArtifact(paths: Iterable<string>, path: string): boolean {
  for (const p of paths) {
    if (sameWorkspaceArtifact(p, path)) return true;
  }
  return false;
}

/** Collapse store prefixes so `sessions/<sid>/app/page.tsx` and
 *  `projects/<pid>/s-1/app/page.tsx` both become `app/page.tsx`.
 *  Mirrors backend stripScopePrefix. */
export function stripWorkspaceScope(path: string): string {
  let p = stripWorkspacePrefix(path);
  for (const top of ["projects/", "sessions/"] as const) {
    if (!p.startsWith(top)) continue;
    const rest = p.slice(top.length);
    const i = rest.indexOf("/");
    if (i < 0) return "";
    let after = rest.slice(i + 1);
    if (top === "projects/") {
      const j = after.indexOf("/");
      if (j >= 0) {
        const first = after.slice(0, j);
        if (first.startsWith("s-")) after = after.slice(j + 1);
      }
    }
    return after;
  }
  return p;
}

export function isAppTreePath(path: string): boolean {
  const rel = stripWorkspaceScope(path);
  return rel === APP_SUBDIR || rel.startsWith(APP_SUBDIR + "/");
}

export function listedFileSnapKey(f: { size?: number; modTime?: number }): string {
  return `${f.size ?? ""}|${f.modTime ?? ""}`;
}

/** Prefer the store key (sessions/… or projects/…) when both refer to
 *  the same file, so preview/download URLs keep working. */
export function dedupeWorkspaceFiles<T extends WorkspacePath>(files: T[]): T[] {
  const out: T[] = [];
  for (const f of files) {
    if (isSystemFile(f.path)) continue;
    const idx = out.findIndex((e) => sameWorkspaceArtifact(e.path, f.path));
    if (idx < 0) {
      out.push(f);
      continue;
    }
    const existing = out[idx];
    const preferIncoming =
      /^(sessions|projects)\//.test(f.path) &&
      !/^(sessions|projects)\//.test(existing.path);
    if (preferIncoming) out[idx] = f;
  }
  return out;
}

/** Union write_file hits with the post-turn list, keyed so a relative
 *  tool path and its store key count as one file. Listed store paths
 *  win. Unlisted tool paths (flush lag) are kept. */
export function mergeTurnWorkspaceFiles<T extends WorkspacePath>(
  turnFiles: T[],
  postTurnListed: T[],
  preTurn: Map<string, string>,
): T[] {
  const listedNew: T[] = [];
  for (const f of postTurnListed) {
    if (isSystemFile(f.path)) continue;
    if (preTurn.get(f.path) === listedFileSnapKey(f)) continue;
    listedNew.push(f);
  }

  const extra: T[] = [];
  for (const t of turnFiles) {
    if (isSystemFile(t.path)) continue;
    if (listedNew.some((f) => sameWorkspaceArtifact(t.path, f.path))) continue;
    const listed = postTurnListed.find((f) => sameWorkspaceArtifact(t.path, f.path));
    extra.push(listed ?? t);
  }
  return dedupeWorkspaceFiles([...listedNew, ...extra]);
}

export function isTemplateNoise(workspaceCount: number): boolean {
  return workspaceCount > TEMPLATE_NOISE_MIN;
}

/** Files the side panel should render. "Changed" is git-status inside
 *  app/, which misses session-root uploads and write_file outputs —
 *  those stay visible so the tree matches the Open-files badge. */
export function panelVisibleFiles<T extends WorkspacePath>(
  workspaceFiles: T[],
  changed: { files: T[]; available: boolean },
  showAll: boolean,
): T[] {
  const cleaned = workspaceFiles.filter((f) => !isSystemFile(f.path));
  if (!changed.available || showAll || !isTemplateNoise(cleaned.length)) {
    return dedupeWorkspaceFiles(cleaned);
  }
  const byPath = new Map<string, T>();
  for (const f of changed.files) {
    const match = cleaned.find((w) => sameWorkspaceArtifact(f.path, w.path));
    const keep = match ?? f;
    byPath.set(keep.path, keep);
  }
  for (const f of cleaned) {
    if (!isAppTreePath(f.path)) byPath.set(f.path, f);
  }
  return [...byPath.values()];
}

/** Count shown on "Open files" — same default list the panel opens to. */
export function filesForOpenBadge<T extends WorkspacePath>(
  workspaceFiles: T[],
  changed: { files: T[]; available: boolean } = { files: [], available: false },
): T[] {
  const cleaned = workspaceFiles.filter((f) => !isSystemFile(f.path));
  const showAll = !changed.available || !isTemplateNoise(cleaned.length);
  return panelVisibleFiles(cleaned, changed, showAll);
}
