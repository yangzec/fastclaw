"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { BookOpen, FileText, Files, Loader2, Trash2, Upload, X } from "lucide-react";

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { apiFetch } from "@/lib/api";
import { useAgentIdFromURL } from "@/hooks/use-agent-id";
import { useAgentName } from "@/hooks/use-agent-name";

const MAX_BYTES = 256 * 1024;
const ALLOWED_EXTS = new Set([".md", ".markdown", ".txt", ".csv", ".json", ".yaml", ".yml", ".log"]);

type KnowledgeFile = {
  name: string;
  storedName?: string;
  path: string;
  size: number;
  hash?: string;
};

function fileExt(name: string): string {
  const i = name.lastIndexOf(".");
  return i >= 0 ? name.slice(i).toLowerCase() : "";
}

export default function AgentKnowledgePage() {
  const agentId = useAgentIdFromURL();
  const agentName = useAgentName(agentId);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const [files, setFiles] = useState<KnowledgeFile[]>([]);
  const [loading, setLoading] = useState(true);
  const [uploadOpen, setUploadOpen] = useState(false);
  const [uploadFiles, setUploadFiles] = useState<File[]>([]);
  const [uploading, setUploading] = useState(false);
  const [uploadProgress, setUploadProgress] = useState("");
  const [uploadError, setUploadError] = useState("");
  const [dragOver, setDragOver] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<KnowledgeFile | null>(null);

  const fetchFiles = useCallback(async () => {
    setLoading(true);
    try {
      const res = await apiFetch(`/api/agents/${agentId}/knowledge-files`);
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data?.error || `Failed to load files (${res.status})`);
      setFiles((data.files || []) as KnowledgeFile[]);
    } finally {
      setLoading(false);
    }
  }, [agentId]);

  useEffect(() => {
    fetchFiles().catch(() => setLoading(false));
  }, [fetchFiles]);

  const handleUploadOpenChange = (open: boolean) => {
    setUploadOpen(open);
    if (!open) {
      setUploadFiles([]);
      setUploadError("");
      setUploadProgress("");
      setDragOver(false);
      if (fileInputRef.current) fileInputRef.current.value = "";
    }
  };

  const acceptFiles = (list: FileList | null) => {
    if (!list?.length) return;
    const next: File[] = [];
    const errors: string[] = [];
    const seen = new Set(uploadFiles.map((f) => `${f.name}:${f.size}`));
    for (const file of Array.from(list)) {
      const key = `${file.name}:${file.size}`;
      if (seen.has(key)) continue;
      if (!ALLOWED_EXTS.has(fileExt(file.name))) {
        errors.push(`${file.name}: unsupported type. Use .md .txt .csv .json .yaml .yml .log`);
        continue;
      }
      if (file.size > MAX_BYTES) {
        errors.push(`${file.name}: too large; maximum size is 256KB.`);
        continue;
      }
      seen.add(key);
      next.push(file);
    }
    if (next.length) {
      setUploadFiles((prev) => [...prev, ...next]);
    }
    setUploadError(errors.join("\n"));
    if (fileInputRef.current) fileInputRef.current.value = "";
  };

  const removeSelected = (index: number) => {
    setUploadFiles((prev) => prev.filter((_, i) => i !== index));
  };

  const upload = async () => {
    if (!uploadFiles.length) return;
    setUploading(true);
    setUploadError("");
    const failures: string[] = [];
    let duplicates = 0;
    let saved = 0;
    try {
      for (let i = 0; i < uploadFiles.length; i++) {
        const file = uploadFiles[i];
        setUploadProgress(`Uploading ${i + 1}/${uploadFiles.length}…`);
        const form = new FormData();
        form.append("file", file);
        const res = await apiFetch(`/api/agents/${agentId}/knowledge-files`, {
          method: "POST",
          body: form,
        });
        const data = await res.json().catch(() => ({}));
        if (!res.ok || data?.ok === false) {
          failures.push(`${file.name}: ${data?.error || `upload failed (${res.status})`}`);
          continue;
        }
        if (data?.duplicate) {
          duplicates += 1;
          continue;
        }
        saved += 1;
      }
      await fetchFiles();
      const notes: string[] = [];
      if (duplicates) {
        notes.push(
          duplicates === 1
            ? "1 file was already in the knowledge base."
            : `${duplicates} files were already in the knowledge base.`,
        );
      }
      if (failures.length) {
        notes.push(failures.join("\n"));
        setUploadError(notes.join("\n"));
        setUploadFiles((prev) =>
          prev.filter((file) => failures.some((msg) => msg.startsWith(`${file.name}:`))),
        );
        return;
      }
      if (saved === 0 && duplicates > 0) {
        setUploadError(notes.join("\n"));
        return;
      }
      handleUploadOpenChange(false);
    } catch (err) {
      setUploadError(err instanceof Error ? err.message : String(err));
    } finally {
      setUploading(false);
      setUploadProgress("");
    }
  };

  const deleteFile = async () => {
    if (!deleteTarget) return;
    const target = deleteTarget;
    setDeleteTarget(null);
    const res = await apiFetch(
      `/api/agents/${agentId}/knowledge-files/${encodeURIComponent(target.storedName || target.name)}`,
      { method: "DELETE" },
    );
    if (res.ok) fetchFiles();
  };

  return (
    <div className="p-6 space-y-6 max-w-5xl mx-auto">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Knowledge</h2>
          <p className="text-sm text-muted-foreground mt-1">
            Reference files scoped to <strong>{agentName}</strong>
          </p>
        </div>
        <Button variant="outline" onClick={() => setUploadOpen(true)}>
          <Upload className="h-4 w-4 mr-2" />
          Upload files
        </Button>
      </div>

      {loading ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {[1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-32" />
          ))}
        </div>
      ) : files.length === 0 ? (
        <div className="rounded-lg border border-border bg-card">
          <div className="flex flex-col items-center justify-center py-16">
            <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-primary/10 mb-4">
              <BookOpen className="h-7 w-7 text-primary" />
            </div>
            <p className="text-sm text-muted-foreground mb-1">No knowledge files yet</p>
            <p className="text-xs text-muted-foreground/60 mb-4 max-w-sm text-center">
              Upload reference files this agent should use when answering.
            </p>
            <Button variant="outline" size="sm" onClick={() => setUploadOpen(true)}>
              <Upload className="h-4 w-4 mr-2" />
              Upload files
            </Button>
          </div>
        </div>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {files.map((file) => (
            <div
              key={file.path}
              className="group rounded-lg border border-border bg-card p-5 transition-colors hover:bg-muted/50"
            >
              <div className="flex items-start justify-between gap-3">
                <div className="flex min-w-0 items-center gap-2.5">
                  <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-primary/10">
                    <FileText className="h-4 w-4 text-primary" />
                  </div>
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium" title={file.name}>
                      {file.name}
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {formatBytes(file.size)}
                      {file.hash ? ` · ${file.hash.slice(0, 8)}` : ""}
                    </p>
                  </div>
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7 shrink-0 text-muted-foreground opacity-0 transition-opacity hover:text-destructive group-hover:opacity-100"
                  onClick={() => setDeleteTarget(file)}
                  title="Delete knowledge file"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}

      <Dialog open={uploadOpen} onOpenChange={handleUploadOpenChange}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Upload knowledge files</DialogTitle>
          </DialogHeader>
          <input
            ref={fileInputRef}
            type="file"
            multiple
            className="hidden"
            accept=".md,.markdown,.txt,.csv,.json,.yaml,.yml,.log"
            onChange={(event) => acceptFiles(event.target.files)}
          />
          <button
            type="button"
            onClick={() => fileInputRef.current?.click()}
            onDragOver={(event) => {
              event.preventDefault();
              setDragOver(true);
            }}
            onDragLeave={() => setDragOver(false)}
            onDrop={(event) => {
              event.preventDefault();
              setDragOver(false);
              acceptFiles(event.dataTransfer.files);
            }}
            className={`flex h-40 w-full flex-col items-center justify-center gap-3 rounded-xl border-2 border-dashed bg-muted/20 px-6 py-8 text-center transition-colors hover:bg-muted/40 ${
              dragOver ? "border-primary bg-primary/5" : "border-border"
            }`}
          >
            <Files
              className={`h-10 w-10 ${
                uploadFiles.length ? "text-primary" : "text-muted-foreground/60"
              }`}
              strokeWidth={1.4}
            />
            <p className="text-sm text-muted-foreground">
              {uploadFiles.length
                ? "Drop or click to add more files"
                : "Drag and drop or click to upload"}
            </p>
          </button>
          {uploadFiles.length > 0 && (
            <ul className="max-h-40 space-y-1.5 overflow-y-auto">
              {uploadFiles.map((file, index) => (
                <li
                  key={`${file.name}-${file.size}-${index}`}
                  className="flex items-center justify-between gap-2 rounded-md border border-border bg-muted/30 px-3 py-1.5"
                >
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium" title={file.name}>
                      {file.name}
                    </p>
                    <p className="text-xs text-muted-foreground">{formatBytes(file.size)}</p>
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="h-6 w-6 shrink-0 text-muted-foreground hover:text-destructive"
                    onClick={() => removeSelected(index)}
                    disabled={uploading}
                    title="Remove from selection"
                  >
                    <X className="h-3.5 w-3.5" />
                  </Button>
                </li>
              ))}
            </ul>
          )}
          <p className="text-xs text-muted-foreground">
            One or more text files, up to 256KB each. Files with the same content are skipped.
          </p>
          {uploadProgress && (
            <p className="text-xs text-muted-foreground">{uploadProgress}</p>
          )}
          {uploadError && (
            <p className="whitespace-pre-wrap rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive break-words">
              {uploadError}
            </p>
          )}
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="outline" onClick={() => handleUploadOpenChange(false)} disabled={uploading}>
              Cancel
            </Button>
            <Button onClick={upload} disabled={!uploadFiles.length || uploading}>
              {uploading ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  {uploadProgress || "Uploading..."}
                </>
              ) : (
                <>
                  <Upload className="h-4 w-4 mr-2" />
                  {uploadFiles.length > 1 ? `Upload ${uploadFiles.length} files` : "Upload"}
                </>
              )}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      <AlertDialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete knowledge file?</AlertDialogTitle>
            <AlertDialogDescription>
              This removes {deleteTarget?.name} from this agent knowledge base.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={deleteFile} className="bg-destructive text-destructive-foreground hover:bg-destructive/90">
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
}
