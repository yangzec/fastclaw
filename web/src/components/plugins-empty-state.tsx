"use client";

import { useState } from "react";
import { Check, Copy, Plug } from "lucide-react";
import { Button } from "@/components/ui/button";
import { FASTCLAW_PLUGINS_DIR, pluginsDirCopyText } from "@/lib/plugins-path";
import { copyToClipboard } from "@/lib/utils";

export function PluginsEmptyState({
  title = "No plugins yet",
}: {
  title?: string;
}) {
  const [copied, setCopied] = useState(false);
  const path = pluginsDirCopyText();

  const copyPath = async () => {
    const ok = await copyToClipboard(path);
    if (!ok) return;
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1500);
  };

  return (
    <div className="rounded-lg border border-dashed border-border bg-card/30 p-12">
      <div className="flex flex-col items-center justify-center text-center">
        <div className="mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-primary/10">
          <Plug className="h-7 w-7 text-primary" />
        </div>
        <p className="text-sm font-medium">{title}</p>
        <p className="mt-1 max-w-sm text-sm text-muted-foreground">
          Plugins are optional add-ons. Drop a plugin folder into a directory
          on this machine.
        </p>
        <Button
          type="button"
          variant="default"
          className="mt-4"
          onClick={copyPath}
          aria-label={`Copy folder path ${path}`}
        >
          {copied ? (
            <>
              <Check className="h-4 w-4" />
              Copied
            </>
          ) : (
            <>
              <Copy className="h-4 w-4" />
              Copy folder path
            </>
          )}
        </Button>
        <p className="mt-3 max-w-sm text-xs text-muted-foreground/80">
          After you add a plugin folder with its manifest, restart FastClaw
          (or the gateway) so it can pick the new plugin up. Folder:{" "}
          <code className="text-[10px]">{FASTCLAW_PLUGINS_DIR}</code>
        </p>
      </div>
    </div>
  );
}
