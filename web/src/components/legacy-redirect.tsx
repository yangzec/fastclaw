"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

/** Replaces leftover top-level pages that now live elsewhere. */
export function LegacyRedirect({ to }: { to: string }) {
  const router = useRouter();
  useEffect(() => {
    router.replace(to);
  }, [router, to]);
  return (
    <div className="flex min-h-[40vh] items-center justify-center text-sm text-muted-foreground">
      Redirecting…
    </div>
  );
}
