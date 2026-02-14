'use client';

import { Code2, Copy } from 'lucide-react';
import { useState } from 'react';

const code = `package main

import "github.com/xraph/forge"

func main() {
    app := forge.New(
        forge.WithAppName("my-service"),
        forge.WithAppVersion("1.0.0"),
    )

    app.Router().GET("/hello", func(ctx forge.Context) error {
        return ctx.JSON(200, forge.Map{"message": "Hello, World!"})
    })

    app.Run()
}`;

interface Token {
  text: string;
  className?: string;
}

function tokenizeLine(line: string): Token[] {
  const tokens: Token[] = [];
  let remaining = line;

  while (remaining.length > 0) {
    const strMatch = remaining.match(/^("[^"]*")/);
    if (strMatch) {
      tokens.push({ text: strMatch[1], className: 'text-amber-300' });
      remaining = remaining.slice(strMatch[1].length);
      continue;
    }
    const kwMatch = remaining.match(/^(package|import|func|return|error)\b/);
    if (kwMatch) {
      tokens.push({ text: kwMatch[1], className: 'text-purple-400' });
      remaining = remaining.slice(kwMatch[1].length);
      continue;
    }
    const fnMatch = remaining.match(
      /^(New|WithAppName|WithAppVersion|Router|GET|JSON|Run|Map)\b/,
    );
    if (fnMatch) {
      tokens.push({ text: fnMatch[1], className: 'text-sky-400' });
      remaining = remaining.slice(fnMatch[1].length);
      continue;
    }
    const numMatch = remaining.match(/^(\d+)\b/);
    if (numMatch) {
      tokens.push({ text: numMatch[1], className: 'text-orange-300' });
      remaining = remaining.slice(numMatch[1].length);
      continue;
    }
    const commentMatch = remaining.match(/^(\/\/.*)/);
    if (commentMatch) {
      tokens.push({ text: commentMatch[1], className: 'text-zinc-500' });
      remaining = '';
      continue;
    }
    tokens.push({ text: remaining[0] });
    remaining = remaining.slice(1);
  }

  return tokens;
}

function highlightGo(source: string): React.ReactNode[] {
  return source.split('\n').map((line, i) => {
    const tokens = tokenizeLine(line);
    return (
      <div key={i} className="table-row">
        <span className="table-cell pr-4 text-right text-zinc-600 select-none text-xs w-8">
          {String(i + 1).padStart(2, '0')}
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
            : '\u00A0'}
        </span>
      </div>
    );
  });
}

export function CodeCard({ className = '' }: { className?: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    await navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div
      className={`overflow-hidden border border-fd-border bg-zinc-950 shadow-2xl shadow-black/10 ${className}`}
    >
      <div className="flex items-center justify-between border-b border-zinc-800 px-4 py-3">
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-1.5">
            <div className="size-3 bg-zinc-700" />
            <div className="size-3 bg-zinc-700" />
            <div className="size-3 bg-zinc-700" />
          </div>
          <div className="flex items-center gap-2">
            <Code2 className="size-3.5 text-zinc-500" />
            <span className="text-xs text-zinc-400 font-medium">main.go</span>
          </div>
        </div>
        <button
          type="button"
          onClick={handleCopy}
          className="text-zinc-500 hover:text-zinc-300 transition-colors"
          aria-label="Copy code"
        >
          <Copy className={`size-3.5 ${copied ? 'text-emerald-400' : ''}`} />
        </button>
      </div>
      <pre className="overflow-x-auto p-4 text-[13px] leading-relaxed font-mono text-zinc-300">
        <code className="table w-full">{highlightGo(code)}</code>
      </pre>
    </div>
  );
}
