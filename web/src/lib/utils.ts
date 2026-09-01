import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// Clipboard API is only reliable in a secure context (HTTPS or localhost).
// Self-hosted dashboards are often opened as http://<lan-ip>:port, where
// `navigator.clipboard` is missing or writeText rejects. execCommand must
// run in the same tick as the user gesture, so insecure origins skip the
// async API and copy immediately.
function copyWithExecCommand(text: string): boolean {
  if (typeof document === "undefined") return false;
  const el = document.createElement("textarea");
  el.value = text;
  el.setAttribute("readonly", "");
  el.setAttribute("aria-hidden", "true");
  el.style.position = "fixed";
  el.style.top = "0";
  el.style.left = "0";
  el.style.width = "1px";
  el.style.height = "1px";
  el.style.padding = "0";
  el.style.border = "none";
  el.style.outline = "none";
  el.style.boxShadow = "none";
  el.style.background = "transparent";
  el.style.opacity = "0";
  document.body.appendChild(el);

  const selection = document.getSelection();
  const savedRange = selection && selection.rangeCount > 0 ? selection.getRangeAt(0) : null;
  el.focus();
  el.select();
  el.setSelectionRange(0, el.value.length);

  let ok = false;
  try {
    ok = document.execCommand("copy");
  } catch {
    ok = false;
  }
  document.body.removeChild(el);
  if (savedRange && selection) {
    selection.removeAllRanges();
    selection.addRange(savedRange);
  }
  return ok;
}

export async function copyToClipboard(text: string): Promise<boolean> {
  if (typeof window === "undefined") return false;
  const canUseClipboard = window.isSecureContext && !!navigator.clipboard?.writeText;
  if (canUseClipboard) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      return copyWithExecCommand(text);
    }
  }
  return copyWithExecCommand(text);
}
