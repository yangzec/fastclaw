/**
 * Repair LLM markdown so a stray fence does not swallow the rest of
 * a chat bubble as a single code block.
 *
 * Streamdown / CommonMark treat an unclosed ``` (or ~~~) as a fenced
 * block that runs to EOF. Models often open one to quote an error,
 * then keep writing **bold**, lists, and `inline code` — which then
 * render as literal text inside a numbered code card. If that body
 * looks like chat prose, we break the opening fence so the rest
 * parses as markdown.
 */

const FENCE_LINE = /^([ \t]{0,3})(`{3,}|~{3,})(.*)$/;

export function looksLikeChatProse(body: string): boolean {
  if (!body.trim()) return false;
  const hasCJK = /[\u4e00-\u9fff]/.test(body);
  const hasBold = /\*\*[^*\n]+\*\*/.test(body);
  const hasInlineCode = /`[^`\n]+`/.test(body);
  const hasList = /^(\s*[-*] |\s*\d+\. )/m.test(body);
  const hasNestedFence = /```/.test(body);
  const hasHeading = /^#{1,6} /m.test(body);
  if (hasNestedFence) return true;
  if (hasCJK && (hasBold || hasInlineCode || hasList)) return true;
  if (hasList && hasBold && hasInlineCode) return true;
  if (hasHeading && (hasBold || hasList)) return true;
  return false;
}

function breakFenceMarker(marker: string): string {
  return marker[0] + "\u200b" + marker.slice(1);
}

function neutralizeInlineFences(line: string): string {
  if (FENCE_LINE.test(line)) return line;
  return line.replace(/(\S)(`{3,}|~{3,})/g, (_, prefix: string, ticks: string) => prefix + breakFenceMarker(ticks));
}

export function repairChatMarkdown(text: string): string {
  if (!text || (!text.includes("```") && !text.includes("~~~"))) {
    return text;
  }

  const lines = text.split("\n");
  let open: { index: number; char: string; len: number } | null = null;
  for (let i = 0; i < lines.length; i++) {
    const m = lines[i].match(FENCE_LINE);
    if (!m) continue;
    const marker = m[2];
    const char = marker[0];
    const len = marker.length;
    const info = m[3];
    if (open && char === open.char && len >= open.len && info.trim() === "") {
      open = null;
      continue;
    }
    if (!open) {
      open = { index: i, char, len };
    }
  }

  if (open) {
    const body = lines.slice(open.index + 1).join("\n");
    const restOfOpener = lines[open.index].replace(FENCE_LINE, "$3");
    if (looksLikeChatProse(body) || looksLikeChatProse(restOfOpener)) {
      lines[open.index] = lines[open.index].replace(/(`{3,}|~{3,})/, breakFenceMarker);
    }
  }

  return lines.map(neutralizeInlineFences).join("\n");
}
