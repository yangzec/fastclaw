"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { LimitOptionChips } from "@/components/limit-option-chips";
import {
  compactLimitLabel,
  contextWindowOptionsFor,
  maxTokenOptionsFor,
  modelLimitsTip,
  recommendedLimits,
} from "@/lib/model-defaults";

type Props = {
  modelId: string;
  contextWindow: number;
  maxTokens: number;
  onContextWindowChange: (next: number) => void;
  onMaxTokensChange: (next: number) => void;
};

export function ModelLimitsFields({
  modelId,
  contextWindow,
  maxTokens,
  onContextWindowChange,
  onMaxTokensChange,
}: Props) {
  const rec = recommendedLimits(modelId);
  const ctxSelected = contextWindow > 0 ? contextWindow : rec.contextWindow;
  const outSelected = maxTokens > 0 ? maxTokens : rec.maxTokens;
  const onDefaults =
    ctxSelected === rec.contextWindow && outSelected === rec.maxTokens;
  const [open, setOpen] = useState(!onDefaults);
  const expanded = open || !onDefaults;
  const ctxOptions = contextWindowOptionsFor(modelId);
  const outOptions = maxTokenOptionsFor(modelId);
  const tip = modelLimitsTip(modelId);

  const applyRecommended = () => {
    onContextWindowChange(rec.contextWindow);
    onMaxTokensChange(rec.maxTokens);
    setOpen(false);
  };

  return (
    <div className="space-y-3 rounded-lg border border-border/70 bg-muted/20 p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs text-foreground/80">
          {onDefaults ? (
            <>
              推荐已套用
              <span className="ml-1.5 tabular-nums text-muted-foreground">
                上下文 {compactLimitLabel(rec.contextWindow)} · 输出 {compactLimitLabel(rec.maxTokens)}
              </span>
            </>
          ) : (
            <>
              已改过
              <span className="ml-1.5 tabular-nums text-muted-foreground">
                推荐是 {compactLimitLabel(rec.contextWindow)} / {compactLimitLabel(rec.maxTokens)}
              </span>
            </>
          )}
        </p>
        {onDefaults ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-6 px-1.5 text-[11px] text-muted-foreground"
            onClick={() => setOpen((v) => !v)}
          >
            {open ? "收起" : "调整"}
          </Button>
        ) : (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-6 px-1.5 text-[11px] text-muted-foreground"
            onClick={applyRecommended}
          >
            套用推荐
          </Button>
        )}
      </div>

      {expanded ? (
        <>
          <div className="space-y-1.5">
            <Label className="text-xs">上下文</Label>
            <LimitOptionChips
              options={ctxOptions}
              selected={ctxSelected}
              onChange={onContextWindowChange}
            />
            {!ctxOptions.some((o) => o.value === ctxSelected) && contextWindow > 0 ? (
              <Input
                type="number"
                min={1}
                value={contextWindow}
                onChange={(e) => {
                  const raw = e.target.value.trim();
                  onContextWindowChange(raw === "" ? 0 : Number(raw) || 0);
                }}
                className="font-mono text-xs h-8 max-w-40"
              />
            ) : null}
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs">最大输出</Label>
            <LimitOptionChips
              options={outOptions}
              selected={outSelected}
              onChange={onMaxTokensChange}
            />
            {!outOptions.some((o) => o.value === outSelected) && maxTokens > 0 ? (
              <Input
                type="number"
                min={1}
                value={maxTokens}
                onChange={(e) => {
                  const raw = e.target.value.trim();
                  onMaxTokensChange(raw === "" ? 0 : Number(raw) || 0);
                }}
                className="font-mono text-xs h-8 max-w-40"
              />
            ) : null}
          </div>
        </>
      ) : null}

      <p className="text-[11px] leading-relaxed text-muted-foreground">
        <span className="font-medium text-foreground/75">{tip.headline}</span>
        {expanded ? <span className="mt-0.5 block">{tip.body}</span> : null}
      </p>
    </div>
  );
}
