"use client";

import type { LimitOption } from "@/lib/model-defaults";

const TAG_LABEL: Record<string, string> = {
  suggested: "建议",
  official: "上限",
  legacy: "旧档",
};

export function LimitOptionChips({
  options,
  selected,
  onChange,
}: {
  options: LimitOption[];
  selected: number;
  onChange: (next: number) => void;
}) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {options.map((opt) => {
        const active = selected === opt.value;
        return (
          <button
            key={opt.value}
            type="button"
            onClick={() => onChange(opt.value)}
            className={`h-8 rounded-md border px-2.5 text-xs tabular-nums transition-colors ${
              active
                ? "border-foreground bg-foreground text-background"
                : "border-border text-muted-foreground hover:bg-muted hover:text-foreground"
            }`}
          >
            {opt.label}
            {opt.tag ? (
              <span className={`ml-1 ${active ? "opacity-80" : opt.tag === "suggested" ? "text-foreground/70" : "opacity-60"}`}>
                {TAG_LABEL[opt.tag]}
              </span>
            ) : null}
          </button>
        );
      })}
    </div>
  );
}
