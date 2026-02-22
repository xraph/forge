/**
 * GitHub API fetcher for forgery extension metadata.
 * Runs only in Node.js (Server Component / ISR context).
 */

export interface GitHubRepoData {
  stars: number;
  description: string | null;
  latestVersion: string | null;
  source: "live" | "fallback";
}

async function fetchRepoData(
  owner: string,
  repo: string,
  fallback: { stars: number; description: string; version: string },
): Promise<GitHubRepoData> {
  const token = process.env.GITHUB_TOKEN;
  const headers: HeadersInit = {
    Accept: "application/vnd.github+json",
    "X-GitHub-Api-Version": "2022-11-28",
    "User-Agent": "forge-docs/1.0",
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };

  let stars = fallback.stars;
  let description: string | null = fallback.description;
  let isLive = false;

  // Fetch repo metadata (stars, description)
  try {
    const res = await fetch(
      `https://api.github.com/repos/${owner}/${repo}`,
      { headers, next: { revalidate: 3600 } },
    );
    if (res.ok) {
      const data = await res.json();
      stars = data.stargazers_count ?? fallback.stars;
      description = data.description ?? fallback.description;
      isLive = true;
    }
  } catch {
    // Network error or rate limit — use fallback
  }

  // Fetch latest release tag, falling back to tags list
  let latestVersion: string | null = fallback.version;
  try {
    const relRes = await fetch(
      `https://api.github.com/repos/${owner}/${repo}/releases/latest`,
      { headers, next: { revalidate: 3600 } },
    );
    if (relRes.ok) {
      const rel = await relRes.json();
      latestVersion = rel.tag_name ?? fallback.version;
    } else {
      const tagRes = await fetch(
        `https://api.github.com/repos/${owner}/${repo}/tags?per_page=1`,
        { headers, next: { revalidate: 3600 } },
      );
      if (tagRes.ok) {
        const tags = await tagRes.json();
        latestVersion = tags[0]?.name ?? fallback.version;
      }
    }
  } catch {
    // Use fallback version
  }

  return {
    stars,
    description,
    latestVersion,
    source: isLive ? "live" : "fallback",
  };
}

export async function fetchAllRepoData(
  extensions: {
    slug: string;
    fallbackStars: number;
    fallbackVersion: string;
    description: string;
  }[],
  owner = "xraph",
): Promise<Map<string, GitHubRepoData>> {
  const results = await Promise.allSettled(
    extensions.map((ext) =>
      fetchRepoData(owner, ext.slug, {
        stars: ext.fallbackStars,
        description: ext.description,
        version: ext.fallbackVersion,
      }),
    ),
  );

  const map = new Map<string, GitHubRepoData>();
  for (let i = 0; i < results.length; i++) {
    const result = results[i];
    const ext = extensions[i];
    if (result.status === "fulfilled") {
      map.set(ext.slug, result.value);
    } else {
      map.set(ext.slug, {
        stars: ext.fallbackStars,
        description: ext.description,
        latestVersion: ext.fallbackVersion,
        source: "fallback",
      });
    }
  }

  return map;
}
