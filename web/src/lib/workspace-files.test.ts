import assert from "node:assert/strict";
import { test } from "node:test";
import {
  filesForOpenBadge,
  hasWorkspaceArtifact,
  isAppTreePath,
  isSystemFile,
  mergeTurnWorkspaceFiles,
  panelVisibleFiles,
  sameWorkspaceArtifact,
  stripWorkspaceScope,
  workspaceToolPath,
} from "./workspace-files.ts";

test("workspaceToolPath treats /workspace and relative as the same file", () => {
  assert.equal(workspaceToolPath("notes.md"), "notes.md");
  assert.equal(workspaceToolPath("/workspace/notes.md"), "notes.md");
  assert.equal(workspaceToolPath("/workspace/app/page.tsx"), "app/page.tsx");
  assert.equal(workspaceToolPath("/tmp/scratch.txt"), null);
  assert.equal(workspaceToolPath("SOUL.md"), null);
  assert.equal(workspaceToolPath(""), null);
});

test("isSystemFile only matches identity files at the workspace root", () => {
  assert.equal(isSystemFile("SOUL.md"), true);
  assert.equal(isSystemFile("docs/SOUL.md"), false);
});

test("sameWorkspaceArtifact matches tool path to store key", () => {
  assert.equal(sameWorkspaceArtifact("notes.md", "sessions/s-1/notes.md"), true);
  assert.equal(sameWorkspaceArtifact("/workspace/notes.md", "sessions/s-1/notes.md"), true);
  assert.equal(sameWorkspaceArtifact("app/page.tsx", "projects/p1/app/page.tsx"), true);
  assert.equal(sameWorkspaceArtifact("app/page.tsx", "projects/p1/s-1/app/page.tsx"), true);
  assert.equal(
    sameWorkspaceArtifact("projects/p1/notes.md", "projects/p1/s-1/notes.md"),
    false,
  );
  assert.equal(sameWorkspaceArtifact("a.md", "sessions/s-1/b.md"), false);
});

test("hasWorkspaceArtifact uses artifact equality not string equality", () => {
  const seen = new Set(["notes.md"]);
  assert.equal(hasWorkspaceArtifact(seen, "sessions/s-1/notes.md"), true);
  assert.equal(hasWorkspaceArtifact(seen, "sessions/s-1/other.md"), false);
});

test("stripWorkspaceScope matches backend session and project prefixes", () => {
  assert.equal(stripWorkspaceScope("sessions/s-1/notes.md"), "notes.md");
  assert.equal(stripWorkspaceScope("projects/p1/s-9/app/page.tsx"), "app/page.tsx");
  assert.equal(stripWorkspaceScope("projects/p1/shared.md"), "shared.md");
  assert.equal(stripWorkspaceScope("notes.md"), "notes.md");
});

test("isAppTreePath is only the coding app subtree", () => {
  assert.equal(isAppTreePath("sessions/s-1/app/page.tsx"), true);
  assert.equal(isAppTreePath("sessions/s-1/notes.md"), false);
  assert.equal(isAppTreePath("projects/p1/s-1/app/src/main.ts"), true);
  assert.equal(isAppTreePath("upload.pdf"), false);
});

test("mergeTurnWorkspaceFiles does not double-count write_file + list path", () => {
  const pre = new Map<string, string>();
  const merged = mergeTurnWorkspaceFiles(
    [{ path: "notes.md", size: 12 }],
    [{ path: "sessions/s-1/notes.md", size: 12, modTime: 100 }],
    pre,
  );
  assert.equal(merged.length, 1);
  assert.equal(merged[0].path, "sessions/s-1/notes.md");
});

test("mergeTurnWorkspaceFiles keeps a second distinct file", () => {
  const pre = new Map<string, string>();
  const merged = mergeTurnWorkspaceFiles(
    [{ path: "notes.md", size: 12 }],
    [
      { path: "sessions/s-1/notes.md", size: 12, modTime: 100 },
      { path: "sessions/s-1/chart.png", size: 80, modTime: 101 },
    ],
    pre,
  );
  assert.equal(merged.length, 2);
  assert.deepEqual(merged.map((f) => f.path).sort(), [
    "sessions/s-1/chart.png",
    "sessions/s-1/notes.md",
  ]);
});

test("mergeTurnWorkspaceFiles skips unchanged listed files unless write_file hit them", () => {
  const pre = new Map([["sessions/s-1/old.md", "10|50"]]);
  const merged = mergeTurnWorkspaceFiles(
    [],
    [
      { path: "sessions/s-1/old.md", size: 10, modTime: 50 },
      { path: "sessions/s-1/new.md", size: 3, modTime: 90 },
    ],
    pre,
  );
  assert.equal(merged.length, 1);
  assert.equal(merged[0].path, "sessions/s-1/new.md");
});

test("panelVisibleFiles keeps session-root files in Changed view", () => {
  const workspace = [
    { path: "sessions/s-1/notes.md", size: 1 },
    { path: "sessions/s-1/app/page.tsx", size: 2 },
    { path: "sessions/s-1/app/layout.tsx", size: 3 },
  ];
  const changed = {
    available: true,
    files: [{ path: "sessions/s-1/app/page.tsx" }],
  };
  const visible = panelVisibleFiles(workspace, changed, false);
  // Small workspace (3 files) is not template noise — show everything.
  assert.equal(visible.length, 3);

  const noisy = Array.from({ length: 30 }, (_, i) => ({
    path: `sessions/s-1/app/f${i}.tsx`,
    size: i,
  }));
  noisy.push({ path: "sessions/s-1/notes.md", size: 1 });
  const noisyChanged = {
    available: true,
    files: [{ path: "sessions/s-1/app/f0.tsx" }],
  };
  const filtered = panelVisibleFiles(noisy, noisyChanged, false);
  const paths = filtered.map((f) => f.path);
  assert.ok(paths.includes("sessions/s-1/app/f0.tsx"));
  assert.ok(paths.includes("sessions/s-1/notes.md"));
  assert.equal(paths.includes("sessions/s-1/app/f1.tsx"), false);
});

test("filesForOpenBadge matches the panel default list", () => {
  const two = [
    { path: "sessions/s-1/notes.md", size: 1 },
    { path: "sessions/s-1/app/page.tsx", size: 2 },
  ];
  const badge = filesForOpenBadge(two, {
    available: true,
    files: [{ path: "sessions/s-1/app/page.tsx" }],
  });
  const panel = panelVisibleFiles(two, {
    available: true,
    files: [{ path: "sessions/s-1/app/page.tsx" }],
  }, false);
  assert.deepEqual(badge.map((f) => f.path).sort(), panel.map((f) => f.path).sort());
  assert.equal(badge.length, 2);
});
