/** Canonical drop folder for install-wide hook and tool plugins. */
export const FASTCLAW_PLUGINS_DIR = "~/.fastclaw/plugins";

/** Path shown / copied in the empty-state CTA. Trailing slashes stripped. */
export function pluginsDirCopyText(dir = FASTCLAW_PLUGINS_DIR): string {
  return dir.replace(/\/+$/, "") || FASTCLAW_PLUGINS_DIR;
}
