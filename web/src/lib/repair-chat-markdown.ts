/**
 * Repair LLM markdown so a stray fence does not swallow the rest of
 * a chat bubble as a single code block — and so the fence markers
 * themselves do not leak into the rendered reply as literal ```.
 *
 * Streamdown / CommonMark treat an unclosed ``` (or ~~~) as a fenced
 * block that runs to EOF. Models often open one to quote an error,
 * wrap the whole reply in ```markdown, or glue a closer onto the next
 * sentence (`}```请求`). A previous pass broke those openers with a
 * zero-width space; that stopped the swallow but left visible ``` in
 * the bubble. We now strip / unwrap those markers when the body looks
 * like chat prose. Real code dumps stay fenced.
 */

const FENCE_LINE = /^([ \t]{0,3})(`{3,}|~{3,})(.*)$/;
const PROSE_INFO = /^(markdown|md|text|txt)?$/i;
const MARKDOWN_INFO = /^(markdown|md)$/i;
const MID_FENCE = /(`{3,}|~{3,})/g;

type Fence = { indent: string; marker: string; char: string; len: number; info: string };

function parseFence(line: string): Fence | null {
  const m = line.match(FENCE_LINE);
  if (!m) return null;
  return { indent: m[1], marker: m[2], char: m[2][0], len: m[2].length, info: m[3] };
}

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

function stripOpenerLine(line: string): string {
  const f = parseFence(line);
  if (!f) return line;
  const leftover = f.info.trim();
  if (!leftover || PROSE_INFO.test(leftover)) return "";
  return f.indent + leftover;
}

function unwrapOuterProseFence(text: string): string {
  const lines = text.split("\n");
  let start = 0;
  let end = lines.length - 1;
  while (start <= end && lines[start].trim() === "") start++;
  while (end >= start && lines[end].trim() === "") end--;
  if (start >= end) return text;

  const open = parseFence(lines[start]);
  const close = parseFence(lines[end]);
  if (!open || !close) return text;
  if (open.char !== close.char || close.len < open.len) return text;
  if (close.info.trim() !== "") return text;
  const info = open.info.trim();
  if (!PROSE_INFO.test(info)) return text;

  const body = lines.slice(start + 1, end).join("\n");
  if (!looksLikeChatProse(body) && !MARKDOWN_INFO.test(info)) return text;

  return [...lines.slice(0, start), ...lines.slice(start + 1, end), ...lines.slice(end + 1)].join("\n");
}

function unwrapOuterProseFences(text: string): string {
  for (let n = 0; n < 3; n++) {
    const next = unwrapOuterProseFence(text);
    if (next === text) break;
    text = next;
  }
  return text;
}

function collapseBlankLines(text: string): string {
  return text.replace(/\n{3,}/g, "\n\n");
}

export function repairChatMarkdown(text: string): string {
  if (!text || (!text.includes("```") && !text.includes("~~~"))) {
    return text;
  }

  text = unwrapOuterProseFences(text);

  const lines = text.split("\n");
  const inKeptFence = new Array<boolean>(lines.length).fill(false);
  let open: { index: number; char: string; len: number } | null = null;

  for (let i = 0; i < lines.length; i++) {
    const f = parseFence(lines[i]);
    if (!f) {
      if (open) inKeptFence[i] = true;
      continue;
    }
    if (open && f.char === open.char && f.len >= open.len && f.info.trim() === "") {
      inKeptFence[i] = true;
      open = null;
      continue;
    }
    if (!open) {
      open = { index: i, char: f.char, len: f.len };
      inKeptFence[i] = true;
      continue;
    }
    inKeptFence[i] = true;
  }

  if (open) {
    const body = lines.slice(open.index + 1).join("\n");
    const restOfOpener = parseFence(lines[open.index])?.info ?? "";
    const info = restOfOpener.trim();
    if (looksLikeChatProse(body) || looksLikeChatProse(restOfOpener) || MARKDOWN_INFO.test(info)) {
      lines[open.index] = stripOpenerLine(lines[open.index]);
      for (let i = open.index; i < lines.length; i++) inKeptFence[i] = false;
      open = null;
    }
  }

  for (let i = 0; i < lines.length; i++) {
    if (inKeptFence[i] || parseFence(lines[i])) continue;
    lines[i] = lines[i].replace(MID_FENCE, "\n");
  }

  return collapseBlankLines(lines.join("\n"));
}
