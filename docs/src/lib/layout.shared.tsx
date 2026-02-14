import { ThemedLogo } from "@/components/ui/themed-logo";
import type { BaseLayoutProps } from "fumadocs-ui/layouts/shared";

export const gitConfig = {
  user: "xraph",
  repo: "forge",
  branch: "main",
};

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: (
        <>
          <ThemedLogo />
          <span className="font-bold">Forge</span>
        </>
      ),
    },
    githubUrl: `https://github.com/${gitConfig.user}/${gitConfig.repo}`,
    links: [
      {
        text: "Documentation",
        url: "/docs/forge",
        active: "nested-url",
      },
      {
        text: "Vessel DI",
        url: "/docs/vessel",
        active: "nested-url",
      },
      {
        text: "AI SDK",
        url: "/docs/ai-sdk",
        active: "nested-url",
      },
      {
        text: "Extensions",
        url: "/docs/extensions",
        active: "nested-url",
      },
      {
        text: "CLI",
        url: "/docs/cli",
        active: "nested-url",
      },
      {
        text: "Changelog",
        url: "/changelog",
        active: "url",
      },
      {
        text: "Roadmap",
        url: "/roadmap",
        active: "url",
      },
      {
        text: "Blog",
        url: "/blog",
        active: "nested-url",
      },
    ],
  };
}
