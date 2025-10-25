import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';
import { type LinkItemType } from 'fumadocs-ui/layouts/docs';
import { AlbumIcon, Heart, LayoutTemplate } from 'lucide-react';
import Image from 'next/image';
import { ForgeLogo } from '@/components/logo';

export const linkItems: LinkItemType[] = [
  {
    text: 'Documentation',
    url: '/docs/forge',
    icon: <LayoutTemplate />,
    active: 'nested-url',
  },
  {
    text: 'Extensions',
    url: '/docs/extensions',
    icon: <LayoutTemplate />,
    active: 'nested-url',
  },
  {
    text: 'CLI',
    url: '/docs/cli',
    icon: <LayoutTemplate />,
    active: 'nested-url',
  },
  {
    text: 'Changelog',
    url: '/changelog', 
    icon: <LayoutTemplate />,
    active: 'url',
  },
  {
    text: 'Roadmap',
    url: '/roadmap',
    icon: <Heart />,
  },
  {
    icon: <AlbumIcon />,
    text: 'Blog',
    url: '/blog',
    active: 'nested-url',
  },
];

export const logo = (
  <>
    <ForgeLogo size={20} />
  </>
);

/**
 * Shared layout configurations
 *
 * you can customise layouts individually from:
 * Home Layout: app/(home)/layout.tsx
 * Docs Layout: app/docs/layout.tsx
 */
export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: (
        <>
          <ForgeLogo size={20} animated />
          <span className="font-bold tracking-wide text-lg font-sans [.uwu_&]:hidden max-md:hidden">
            Forge
          </span>
        </>
      ),
    },
    // see https://fumadocs.dev/docs/ui/navigation/links
    links: [...linkItems],
    githubUrl: 'https://github.com/xraph/forge',
  };
}
