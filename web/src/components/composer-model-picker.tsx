"use client";

import { useEffect, useMemo, useState } from "react";
import { Check, ChevronDown } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { getAgent, getMe, updateAgent } from "@/lib/api";
import { shortModelLabel, type ChatModelOption } from "@/lib/chat-models";
import { loadChatModels } from "@/lib/load-chat-models";
import { formatTokenCount } from "@/lib/format-tokens";
import { PROVIDER_LABELS } from "@/lib/provider-presets";
import { presetContextWindow } from "@/lib/model-defaults";

type Props = {
  agentId: string;
  canSwitch: boolean;
  compact?: boolean;
  onChanged?: (model: string) => void;
};

export function ComposerModelPicker({ agentId, canSwitch, compact, onChanged }: Props) {
  const [model, setModel] = useState("");
  const [options, setOptions] = useState<ChatModelOption[]>([]);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!agentId) return;
    let aborted = false;
    (async () => {
      const [agent, me] = await Promise.all([getAgent(agentId), getMe().catch(() => null)]);
      if (aborted) return;
      setModel(agent?.model || "");
      const uid = me?.user?.id || "";
      if (!uid) {
        setOptions([]);
        return;
      }
      const list = await loadChatModels(uid, agentId);
      if (!aborted) setOptions(list);
    })().catch(() => {
      if (!aborted) setOptions([]);
    });
    return () => {
      aborted = true;
    };
  }, [agentId]);

  const groups = useMemo(() => {
    const map = new Map<string, ChatModelOption[]>();
    for (const opt of options) {
      const label = PROVIDER_LABELS[opt.provider] || opt.provider;
      const list = map.get(label) || [];
      list.push(opt);
      map.set(label, list);
    }
    return [...map.entries()];
  }, [options]);

  const label = shortModelLabel(model);
  const triggerClass = compact
    ? "flex h-10 max-w-[9.5rem] shrink-0 items-center gap-1 rounded-lg px-2 text-xs text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50 md:h-8"
    : "flex h-9 max-w-[11rem] shrink-0 items-center gap-1 rounded-full border border-border px-3 text-xs text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50";

  if (!agentId) return null;

  const pick = async (value: string) => {
    if (!canSwitch || value === model || saving) return;
    const prev = model;
    setModel(value);
    setSaving(true);
    try {
      const res = await updateAgent(agentId, { model: value });
      if (res?.error) {
        setModel(prev);
        return;
      }
      onChanged?.(value);
    } catch {
      setModel(prev);
    } finally {
      setSaving(false);
    }
  };

  if (!canSwitch || options.length === 0) {
    return (
      <span className={triggerClass + " cursor-default"} title={model || "No model configured"}>
        <span className="truncate">{label}</span>
      </span>
    );
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={saving}
        className={triggerClass}
        aria-label="Switch model"
        title={model ? `Model ${model}` : "Switch model"}
      >
        <span className="truncate">{label}</span>
        <ChevronDown className="h-3 w-3 shrink-0 opacity-60" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-64 w-72" side="top">
        <div className="px-1.5 py-1 text-xs font-medium text-muted-foreground">
          This agent&apos;s model
        </div>
        {groups.map(([group, items], gi) => (
          <div key={group}>
            {gi > 0 ? <DropdownMenuSeparator /> : null}
            <DropdownMenuGroup>
              <DropdownMenuLabel>{group}</DropdownMenuLabel>
              {items.map((opt) => (
                <DropdownMenuItem
                  key={opt.value}
                  onClick={() => void pick(opt.value)}
                  className="justify-between gap-3"
                >
                  <span className="flex min-w-0 items-center gap-1.5">
                    {opt.value === model ? <Check className="h-3.5 w-3.5" /> : <span className="w-3.5" />}
                    <span className="truncate">{opt.name || opt.id}</span>
                  </span>
                  <span className="shrink-0 tabular-nums text-[11px] text-muted-foreground">
                    {formatTokenCount(opt.contextWindow || presetContextWindow(opt.id))}
                  </span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
          </div>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
