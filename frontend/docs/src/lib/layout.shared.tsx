import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';
import { type LinkItemType } from 'fumadocs-ui/layouts/docs';
import { AlbumIcon, Heart, LayoutTemplate } from 'lucide-react';
import Image from 'next/image';

export const linkItems: LinkItemType[] = [
  {
    text: 'Documentation',
    url: '/docs',
    icon: <LayoutTemplate />,
    active: 'nested-url',
  },
  {
    icon: <AlbumIcon />,
    text: 'Blog',
    url: '/blog',
    active: 'nested-url',
  },
  // {
  //   text: 'Showcase',
  //   url: '/showcase',
  //   icon: <LayoutTemplate />,
  //   active: 'url',
  // },
  // {
  //   text: 'Sponsors',
  //   url: '/sponsors',
  //   icon: <Heart />,
  // },
];

export const logo = (
  <>
    <Image
      alt="Authsome"
      // src={Logo}
      sizes="100px"
      className="hidden w-20 md:w-24 [.uwu_&]:block"
      aria-label="Authsome"
    />

    Authsome
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
          <svg
            width="24"
            height="24"
            xmlns="http://www.w3.org/2000/svg"
            aria-label="Logo"
          >
            <circle cx={12} cy={12} r={12} fill="currentColor" />
          </svg>
          Authsome
        </>
      ),
    },
    // see https://fumadocs.dev/docs/ui/navigation/links
    links: [...linkItems],
    githubUrl: 'https://github.com/xraph/authsome',
  };
}
