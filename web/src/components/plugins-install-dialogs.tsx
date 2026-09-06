"use client";

import { useEffect, useRef, useState, createElement as h, Fragment } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Files, Info, Loader2 } from "lucide-react";
import { installPlugin, uploadPlugin } from "@/lib/api";

type Cb = (info?: { needsRestart?: boolean }) => void;

export function InstallPluginDialog(props: { open: boolean; onOpenChange: (v: boolean) => void; onInstalled: Cb }) {
  const { open, onOpenChange, onInstalled } = props;
  const [source, setSource] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => { if (!open) { setSource(""); setError(null); setBusy(false); } }, [open]);
  const handleInstall = async () => {
    const trimmed = source.trim();
    if (!trimmed) return;
    setBusy(true); setError(null);
    try {
      const resp = await installPlugin(trimmed);
      if (!resp.ok) { setError(resp.error || "Install failed"); return; }
      onOpenChange(false);
      onInstalled({ needsRestart: resp.needsRestart });
    } catch (e) { setError(e instanceof Error ? e.message : "Install failed"); }
    finally { setBusy(false); }
  };
  return h(Dialog, { open, onOpenChange },
    h(DialogContent, { className: "sm:max-w-md" },
      h(DialogHeader, null,
        h(DialogTitle, null, "Install plugin"),
        h(DialogDescription, null, "Enter a plugin source."),
      ),
      h(Input, { autoFocus: true, placeholder: "mem0", value: source, onChange: (e: any) => setSource(e.target.value) }),
      error ? h("p", { className: "text-xs text-destructive" }, error) : null,
      h("div", { className: "flex justify-end gap-2 pt-2" },
        h(Button, { variant: "outline" as const, onClick: () => onOpenChange(false), disabled: busy }, "Cancel"),
        h(Button, { onClick: handleInstall, disabled: !source.trim() || busy }, busy ? "..." : "Install"),
      ),
    ),
  );
}

export function UploadPluginDialog(props: { open: boolean; onOpenChange: (v: boolean) => void; onInstalled: Cb }) {
  const { open, onOpenChange, onInstalled } = props;
  const inputRef = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [dragOver, setDragOver] = useState(false);
  const reset = () => { setFile(null); setError(null); setDragOver(false); if (inputRef.current) inputRef.current.value = ""; };
  const setOpen = (v: boolean) => { onOpenChange(v); if (!v) reset(); };
  const accept = (files: FileList | null) => {
    if (!files || files.length === 0) return;
    if (files.length > 1) { setError("One zip at a time."); return; }
    const f = files[0];
    if (!/\.zip$/i.test(f.name)) { setError("Must be a zip."); return; }
    setFile(f); setError(null);
  };
  const confirm = async () => {
    if (!file) return;
    setBusy(true); setError(null);
    try {
      const resp = await uploadPlugin(file);
      if (!resp.ok) { setError(resp.error || "upload failed"); return; }
      setOpen(false);
      onInstalled({ needsRestart: resp.needsRestart });
    } catch (e) { setError(e instanceof Error ? e.message : "upload failed"); }
    finally { setBusy(false); if (inputRef.current) inputRef.current.value = ""; }
  };
  return h(Dialog, { open, onOpenChange: setOpen },
    h(DialogContent, { className: "sm:max-w-md" },
      h(DialogHeader, null,
        h(DialogTitle, null, "Upload plugin"),
        h(DialogDescription, null, "Zip must include plugin.json with an id."),
      ),
      h("input", { ref: inputRef, type: "file", accept: ".zip,application/zip", className: "hidden", onChange: (e: any) => accept(e.target.files) }),
      h("button", { type: "button", onClick: () => inputRef.current?.click(), onDragOver: (e: any) => { e.preventDefault(); setDragOver(true); }, onDragLeave: () => setDragOver(false), onDrop: (e: any) => { e.preventDefault(); setDragOver(false); accept(e.dataTransfer.files); }, className: "flex h-40 w-full flex-col items-center justify-center gap-3 rounded-xl border-2 border-dashed px-6 " + (dragOver ? "border-primary" : "border-border") },
        h(Files, { className: "h-10 w-10", strokeWidth: 1.4 }),
        file ? h("p", { className: "text-sm font-medium break-all" }, file.name) : h("p", { className: "text-sm text-muted-foreground" }, "Drag and drop or click"),
      ),
      h("div", { className: "flex items-center gap-2 text-xs text-muted-foreground" }, h(Info, { className: "h-3.5 w-3.5 shrink-0" }), h("span", null, "Restart FastClaw if needed.")),
      error ? h("p", { className: "text-xs text-destructive" }, error) : null,
      h("div", { className: "flex justify-end gap-2 pt-2" },
        h(Button, { variant: "outline" as const, onClick: () => setOpen(false), disabled: busy }, "Cancel"),
        h(Button, { onClick: confirm, disabled: !file || busy }, busy ? "..." : "Upload"),
      ),
    ),
  );
}
