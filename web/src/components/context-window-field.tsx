"use client";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { LimitOptionChips } from "@/components/limit-option-chips";
import {
  contextWindowOptionsFor,
  contextWindowTip,
  presetContextWindow,
} from "@/lib/model-defaults";

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
  const options = contextWindowOptionsFor(modelId);
  const selected = value > 0 ? value : fallback;
  const inList = options.some((o) => o.value === selected);
  const customized = value > 0 && value !== fallback;
  const tip = contextWindowTip(modelId);

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
            Reset to suggested ({fallback.toLocaleString()})
          </Button>
        )}
      </div>
      <LimitOptionChips options={options} selected={selected} onChange={onChange} />
      {!inList && value > 0 ? (
        <Input
          id={id}
          type="number"
          min={1}
          step={1}
          value={value}
          onChange={(e) => {
            const raw = e.target.value.trim();
            onChange(raw === "" ? 0 : Number(raw) || 0);
          }}
          className="font-mono text-xs h-8 max-w-40"
        />
      ) : null}
      <div className="rounded-md bg-muted/50 px-2.5 py-2 text-[11px] leading-relaxed text-muted-foreground">
        <p className="font-medium text-foreground/80">{tip.headline}</p>
        <p className="mt-0.5">{tip.body}</p>
      </div>
    </div>
  );
}
