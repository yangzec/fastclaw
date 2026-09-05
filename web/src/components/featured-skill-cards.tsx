"use client";

import { Button } from "@/components/ui/button";
import { FEATURED_SKILLS } from "@/lib/featured-skills";
import { Loader2, Sparkles } from "lucide-react";

export function FeaturedSkillCards({
  onInstall,
  installing,
  installed,
}: {
  onInstall: (name: string) => void;
  installing?: string | null;
  installed?: Set<string>;
}) {
  return (
    <div className="grid gap-2 sm:grid-cols-3">
      {FEATURED_SKILLS.map((skill) => {
        const busy = installing === skill.name;
        const done = installed?.has(skill.name);
        return (
          <div
            key={skill.name}
            className="flex flex-col rounded-lg border border-border bg-card p-3 text-left"
          >
            <div className="mb-2 flex h-8 w-8 items-center justify-center rounded-md bg-primary/10">
              <Sparkles className="h-4 w-4 text-primary" />
            </div>
            <p className="text-sm font-medium">{skill.label}</p>
            <p className="mt-1 flex-1 text-xs text-muted-foreground">{skill.blurb}</p>
            <Button
              size="sm"
              variant="outline"
              className="mt-3"
              disabled={busy || done}
              onClick={() => onInstall(skill.name)}
            >
              {busy ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : null}
              {done ? "Installed" : "Install"}
            </Button>
          </div>
        );
      })}
    </div>
  );
}
