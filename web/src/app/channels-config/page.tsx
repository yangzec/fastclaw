"use client";

import { useEffect, useState } from "react";
import { LegacyRedirect } from "@/components/legacy-redirect";
import { resolveFirstAgentSubpath } from "@/lib/first-chat-nav";

export default function LegacyChannelsConfigPage() {
  const [to, setTo] = useState<string | null>(null);
  useEffect(() => {
    resolveFirstAgentSubpath("channels").then(setTo);
  }, []);
  if (!to) {
    return (
      <div className="flex min-h-[40vh] items-center justify-center text-sm text-muted-foreground">
        Redirecting…
      </div>
    );
  }
  return <LegacyRedirect to={to} />;
}
