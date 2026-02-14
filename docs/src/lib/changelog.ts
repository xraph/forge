import fs from "node:fs";
import path from "node:path";

export type ChangelogSection = {
  heading: string;
  items: string[];
};

export type ChangelogEntry = {
  version: string;
  date: string;
  url: string;
  type: "breaking" | "major" | "minor" | "patch";
  sections: ChangelogSection[];
};

/**
 * Parse the root CHANGELOG.md file at build time and return structured entries.
 */
export function getChangelog(): ChangelogEntry[] {
  const changelogPath = path.resolve(process.cwd(), "..", "CHANGELOG.md");
  const raw = fs.readFileSync(changelogPath, "utf-8");

  const entries: ChangelogEntry[] = [];

  // Split on version headings: ## [version](url) (date)
  const versionPattern =
    /^## \[(\d+\.\d+\.\d+)\]\((https?:\/\/[^\)]+)\)\s+\((\d{4}-\d{2}-\d{2})\)/gm;

  const matches = [...raw.matchAll(versionPattern)];

  for (let i = 0; i < matches.length; i++) {
    const match = matches[i];
    const version = match[1];
    const url = match[2];
    const date = match[3];

    // Extract the text block for this version (until the next version heading or EOF)
    const startIndex = match.index! + match[0].length;
    const endIndex =
      i + 1 < matches.length ? matches[i + 1].index! : raw.length;
    const block = raw.slice(startIndex, endIndex);

    // Determine release type
    const hasBreaking = block.includes("⚠ BREAKING CHANGES");
    const [majorStr, minorStr] = version.split(".");
    const major = Number.parseInt(majorStr, 10);
    const minor = Number.parseInt(minorStr, 10);

    let type: ChangelogEntry["type"];
    if (hasBreaking) {
      type = "breaking";
    } else if (major >= 1) {
      type = "major";
    } else if (minor > 0) {
      type = "minor";
    } else {
      type = "patch";
    }

    // Parse sections (### Heading) and their bullet items
    const sections: ChangelogSection[] = [];
    const sectionPattern = /^### (.+)$/gm;
    const sectionMatches = [...block.matchAll(sectionPattern)];

    for (let j = 0; j < sectionMatches.length; j++) {
      const sectionMatch = sectionMatches[j];
      const heading = sectionMatch[1].trim();

      const sectionStart = sectionMatch.index! + sectionMatch[0].length;
      const sectionEnd =
        j + 1 < sectionMatches.length
          ? sectionMatches[j + 1].index!
          : block.length;

      const sectionText = block.slice(sectionStart, sectionEnd);

      // Extract bullet items (lines starting with * )
      const items = sectionText
        .split("\n")
        .filter((line) => line.trimStart().startsWith("* "))
        .map((line) => {
          // Clean up the markdown: remove leading "* ", strip commit links
          let cleaned = line.trimStart().replace(/^\* /, "");
          // Remove commit hash links like (abc1234)
          cleaned = cleaned.replace(/\s*\([a-f0-9]{7,}\)/g, "");
          // Remove markdown links but keep text: [text](url) -> text
          cleaned = cleaned.replace(/\[([^\]]+)\]\([^)]+\)/g, "$1");
          return cleaned.trim();
        })
        .filter(Boolean);

      if (items.length > 0) {
        sections.push({ heading, items });
      }
    }

    entries.push({ version, date, url, type, sections });
  }

  return entries;
}
