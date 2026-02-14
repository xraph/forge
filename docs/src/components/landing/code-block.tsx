"use client";

import { Copy, Code2 } from "lucide-react";
import { useState } from "react";

interface Token {
  text: string;
  className?: string;
}

function tokenizeGo(line: string): Token[] {
  const tokens: Token[] = [];
  let remaining = line;

  while (remaining.length > 0) {
    // Comments
    const commentMatch = remaining.match(/^(\/\/.*)/);
    if (commentMatch) {
      tokens.push({ text: commentMatch[1], className: "text-zinc-500" });
      remaining = "";
      continue;
    }

    // Strings
    const strMatch = remaining.match(/^("[^"]*")/);
    if (strMatch) {
      tokens.push({ text: strMatch[1], className: "text-amber-300" });
      remaining = remaining.slice(strMatch[1].length);
      continue;
    }

    // Back-tick strings
    const btMatch = remaining.match(/^(`[^`]*`)/);
    if (btMatch) {
      tokens.push({ text: btMatch[1], className: "text-amber-300" });
      remaining = remaining.slice(btMatch[1].length);
      continue;
    }

    // Keywords
    const kwMatch = remaining.match(
      /^(package|import|func|return|error|if|else|for|range|var|const|type|struct|interface|nil|true|false|defer|go|chan|select|switch|case|default|break|continue|map)\b/,
    );
    if (kwMatch) {
      tokens.push({ text: kwMatch[1], className: "text-purple-400" });
      remaining = remaining.slice(kwMatch[1].length);
      continue;
    }

    // Type annotations (capitalized words after brackets or common type positions)
    const typeMatch = remaining.match(
      /^(string|int|bool|byte|rune|float32|float64|int8|int16|int32|int64|uint|uint8|uint16|uint32|uint64|error|any|context\.Context)\b/,
    );
    if (typeMatch) {
      tokens.push({ text: typeMatch[1], className: "text-cyan-300" });
      remaining = remaining.slice(typeMatch[1].length);
      continue;
    }

    // Function/method calls and known identifiers (PascalCase or known names)
    const fnMatch = remaining.match(/^([A-Z][a-zA-Z0-9]*)\b/);
    if (fnMatch) {
      tokens.push({ text: fnMatch[1], className: "text-sky-400" });
      remaining = remaining.slice(fnMatch[1].length);
      continue;
    }

    // Numbers
    const numMatch = remaining.match(/^(\d+\.?\d*)\b/);
    if (numMatch) {
      tokens.push({ text: numMatch[1], className: "text-orange-300" });
      remaining = remaining.slice(numMatch[1].length);
      continue;
    }

    // Operators
    const opMatch = remaining.match(/^(:=|!=|==|<=|>=|\+\+|--|&&|\|\||\.\.\.)/);
    if (opMatch) {
      tokens.push({ text: opMatch[1], className: "text-zinc-400" });
      remaining = remaining.slice(opMatch[1].length);
      continue;
    }

    // Everything else
    tokens.push({ text: remaining[0] });
    remaining = remaining.slice(1);
  }

  return tokens;
}

function highlightGo(source: string): React.ReactNode[] {
  return source.split("\n").map((line, i) => {
    const tokens = tokenizeGo(line);
    return (
      <div key={i} className="table-row group/line">
        <span className="table-cell pr-4 text-right text-zinc-600 select-none text-xs w-8 group-hover/line:text-zinc-500 transition-colors">
          {String(i + 1).padStart(2, "0")}
        </span>
        <span className="table-cell">
          {tokens.length > 0
            ? tokens.map((t, j) =>
                t.className ? (
                  <span key={j} className={t.className}>
                    {t.text}
                  </span>
                ) : (
                  <span key={j}>{t.text}</span>
                ),
              )
            : "\u00A0"}
        </span>
      </div>
    );
  });
}

interface CodeBlockProps {
  code: string;
  filename?: string;
  className?: string;
}

export function CodeBlock({
  code,
  filename = "main.go",
  className = "",
}: CodeBlockProps) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    await navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div
      className={`overflow-hidden border border-fd-border bg-[#0a0a0f] shadow-2xl shadow-black/40 ${className}`}
    >
      {/* Header */}
      <div className="flex items-center justify-between border-b border-zinc-800 px-4 py-2.5 bg-zinc-900/50">
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-1.5">
            <div className="size-2.5 rounded-full bg-zinc-700/80" />
            <div className="size-2.5 rounded-full bg-zinc-700/80" />
            <div className="size-2.5 rounded-full bg-zinc-700/80" />
          </div>
          <div className="flex items-center gap-2">
            <Code2 className="size-3 text-zinc-600" />
            <span className="text-[11px] text-zinc-500 font-medium tracking-wide">
              {filename}
            </span>
          </div>
        </div>
        <button
          type="button"
          onClick={handleCopy}
          className="text-zinc-600 hover:text-zinc-400 transition-colors"
          aria-label="Copy code"
        >
          <Copy className={`size-3.5 ${copied ? "text-emerald-400" : ""}`} />
        </button>
      </div>
      {/* Code */}
      <pre className="overflow-x-auto p-4 text-[13px] leading-relaxed font-mono text-zinc-300 no-scrollbar">
        <code className="table w-full">{highlightGo(code)}</code>
      </pre>
    </div>
  );
}
