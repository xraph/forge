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
 * Raw GitHub URL used as a fallback when the repo-root CHANGELOG.md is not
 * present on disk (e.g. on Vercel, where the build only includes the `docs`
 * root directory and the parent CHANGELOG.md is not uploaded).
 */
const CHANGELOG_RAW_URL =
  "https://raw.githubusercontent.com/xraph/forge/main/CHANGELOG.md";

/**
 * Read the raw CHANGELOG.md contents.
 *
 * Prefers the local file (so local dev and full-repo CI reflect uncommitted
 * changes), and falls back to fetching it from GitHub when the file is not
 * available on disk. Returns `null` if neither source is reachable so callers
 * can degrade gracefully instead of failing the build.
 */
async function readChangelogSource(): Promise<string | null> {
  const candidatePaths = [
    path.resolve(process.cwd(), "..", "CHANGELOG.md"),
    path.resolve(process.cwd(), "CHANGELOG.md"),
  ];

  for (const candidate of candidatePaths) {
    try {
      return fs.readFileSync(candidate, "utf-8");
    } catch {
      // Try the next candidate / the network fallback below.
    }
  }

  try {
    const res = await fetch(CHANGELOG_RAW_URL);
    if (res.ok) {
      return await res.text();
    }
    console.warn(
      `[changelog] failed to fetch ${CHANGELOG_RAW_URL}: ${res.status} ${res.statusText}`,
    );
  } catch (err) {
    console.warn(`[changelog] failed to fetch ${CHANGELOG_RAW_URL}:`, err);
  }

  return null;
}

/**
 * Parse the root CHANGELOG.md file at build time and return structured entries.
 *
 * Sources the changelog from the local file when available and falls back to
 * GitHub otherwise; returns an empty list if neither is reachable so the page
 * still renders instead of crashing the build.
 */
export async function getChangelog(): Promise<ChangelogEntry[]> {
  const raw = await readChangelogSource();
  if (!raw) {
    return [];
  }

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
