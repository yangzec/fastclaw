"use client";

import { useState, createElement as h, Fragment } from "react";
import { Check, ChevronDown, Copy, Download, Plug, Upload } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { FASTCLAW_PLUGINS_DIR, pluginsDirCopyText } from "@/lib/plugins-path";
import { copyToClipboard } from "@/lib/utils";

export function PluginsEmptyState(props: { title?: string; onInstall?: () => void; onUpload?: () => void }) {
  const title = props.title ?? "No plugins yet";
  const [copied, setCopied] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const path = pluginsDirCopyText();
  const copyPath = async () => {
    const ok = await copyToClipboard(path);
    if (!ok) return;
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1500);
  };
  return h("div", { className: "rounded-lg border border-dashed border-border bg-card/30 p-12" },
    h("div", { className: "flex flex-col items-center justify-center text-center" },
      h("div", { className: "mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-primary/10" }, h(Plug, { className: "h-7 w-7 text-primary" })),
      h("p", { className: "text-sm font-medium" }, title),
      h("p", { className: "mt-1 max-w-sm text-sm text-muted-foreground" }, "Add a plugin to get started."),
      h("div", { className: "mt-4 flex flex-wrap items-center justify-center gap-2" },
        h(Button, { type: "button", onClick: props.onInstall }, h(Fragment, null, h(Download, { className: "h-4 w-4" }), " Install")),
        h(Button, { type: "button", variant: "outline" as const, onClick: props.onUpload }, h(Fragment, null, h(Upload, { className: "h-4 w-4" }), " Upload")),
      ),
      h(Collapsible, { open: advancedOpen, onOpenChange: setAdvancedOpen, className: "mt-4 w-full max-w-sm" },
        h(CollapsibleTrigger, { className: "inline-flex items-center gap-1 text-xs text-muted-foreground" },
          h(ChevronDown, { className: "h-3.5 w-3.5" }),
          " Advanced: copy folder path",
        ),
        h(CollapsibleContent, { className: "mt-3 space-y-2" },
          h("p", { className: "text-xs text-muted-foreground" }, "Copy the path and place a plugin folder there, then restart FastClaw."),
          h(Button, { type: "button", variant: "secondary" as const, size: "sm" as const, onClick: copyPath }, copied ? h(Fragment, null, h(Check, { className: "h-4 w-4" }), " Copied") : h(Fragment, null, h(Copy, { className: "h-4 w-4" }), " Copy folder path")),
          h("p", { className: "text-[10px] text-muted-foreground/80" }, "Folder: ", h("code", null, FASTCLAW_PLUGINS_DIR)),
        ),
      ),
    ),
  );
}
