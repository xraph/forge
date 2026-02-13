import { RootProvider } from 'fumadocs-ui/provider/next';
import './global.css';
import { Inter } from 'next/font/google';
import { baseUrl, createMetadata } from '@/lib/metadata';

const inter = Inter({
  subsets: ['latin'],
});

export const metadata = createMetadata({
  title: {
    default: 'Forge',
    template: '%s | Forge',
  },
  description:
    'Production-grade Go framework for building modular, extensible backend services with 28+ extensions, type-safe DI, and multi-protocol support.',
  metadataBase: baseUrl,
});

export default function Layout({ children }: LayoutProps<'/'>) {
  return (
    <html lang="en" className={inter.className} suppressHydrationWarning>
      <body className="flex flex-col min-h-screen">
        <RootProvider>{children}</RootProvider>
      </body>
    </html>
  );
}
