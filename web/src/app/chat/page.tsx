"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { getAgents } from "@/lib/api";

/** Leftover /chat/ shell — send the user to the canonical agent chat. */
export default function LegacyChatRedirect() {
  const router = useRouter();
  useEffect(() => {
    let cancelled = false;
    getAgents()
      .then((list) => {
        if (cancelled) return;
        const id = list[0]?.id;
        router.replace(id ? `/agents/${id}/chat/` : "/agents/");
      })
      .catch(() => {
        if (!cancelled) router.replace("/agents/");
      });
    return () => {
      cancelled = true;
    };
  }, [router]);
  return (
    <div className="flex min-h-[40vh] items-center justify-center text-sm text-muted-foreground">
      Redirecting…
    </div>
  );
}
