import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';

export const gitConfig = {
  user: 'xraph',
  repo: 'forge',
  branch: 'main',
};

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: (
        <>
          <img alt="Forge" src="/logo.svg" className="h-5 w-auto" />
          <span className="font-bold">Forge</span>
        </>
      ),
    },
    githubUrl: `https://github.com/${gitConfig.user}/${gitConfig.repo}`,
    links: [
      {
        text: 'Documentation',
        url: '/docs/forge',
        active: 'nested-url',
      },
    ],
  };
}
