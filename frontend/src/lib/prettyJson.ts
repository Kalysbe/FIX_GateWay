/**
 * Indents JSON text without parsing it, so numbers stay exactly as sent
 * (100.50 stays 100.50). Text that is not JSON is indented on a best-effort
 * basis and otherwise shown as is.
 */
export function prettyJsonText(text: string, indent = "  "): string {
  let out = "";
  let depth = 0;
  let inString = false;
  let escaped = false;
  const newline = () => "\n" + indent.repeat(depth);
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (inString) {
      out += c;
      if (escaped) escaped = false;
      else if (c === "\\") escaped = true;
      else if (c === '"') inString = false;
      continue;
    }
    if (c === " " || c === "\n" || c === "\r" || c === "\t") continue;
    if (c === '"') {
      inString = true;
      out += c;
    } else if (c === "{" || c === "[") {
      const close = c === "{" ? "}" : "]";
      let j = i + 1;
      while (j < text.length && /\s/.test(text[j])) j++;
      if (text[j] === close) {
        out += c + close;
        i = j;
      } else {
        depth++;
        out += c + newline();
      }
    } else if (c === "}" || c === "]") {
      depth = Math.max(0, depth - 1);
      out += newline() + c;
    } else if (c === ",") {
      out += c + newline();
    } else if (c === ":") {
      out += ": ";
    } else {
      out += c;
    }
  }
  return out;
}
