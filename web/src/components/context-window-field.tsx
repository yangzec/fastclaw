"use client";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { presetContextWindow } from "@/lib/model-defaults";

export function ContextWindowField({
  modelId,
  value,
  onChange,
  id,
  className,
}: {
  modelId: string;
  value: number;
  onChange: (next: number) => void;
  id?: string;
  className?: string;
}) {
  const fallback = presetContextWindow(modelId);
  const customized = value > 0 && value !== fallback;

  return (
    <div className={className ?? "space-y-1.5"}>
      <div className="flex items-center justify-between gap-2">
        <Label htmlFor={id} className="text-xs">
          Context window (tokens)
        </Label>
        {customized && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-6 px-1.5 text-[11px] text-muted-foreground"
            onClick={() => onChange(fallback)}
          >
            Reset to default ({fallback.toLocaleString()})
          </Button>
        )}
      </div>
      <Input
        id={id}
        type="number"
        min={1}
        step={1}
        value={value > 0 ? value : ""}
        onChange={(e) => {
          const raw = e.target.value.trim();
          onChange(raw === "" ? 0 : Number(raw) || 0);
        }}
        placeholder={String(fallback)}
        className="font-mono text-xs h-8"
      />
      <p className="text-[11px] text-muted-foreground/70">
        Default for this model is {fallback.toLocaleString()} tokens.
        Change it if your gateway or plan uses a different limit. Compaction
        uses the saved number, not the official default.
      </p>
    </div>
  );
}
