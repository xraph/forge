import { type Page } from '@/lib/source';

export async function getLLMText(page: Page) {
  if (page.data.type === 'openapi') return '';

  const category =
    {
      go: 'Forge Golang (the Go library for Forge)',
      rust: 'Forge Rust (the Rust library for Forge)',
      ui: 'Forge UI (the one stop Auth UI Kit)',
      cli: 'Forge CLI (the CLI tool for automating Forge apps)',
    }[page.slugs[0]] ?? page.slugs[0];

  const processed = await page.data.getText('processed');

  return `# ${category}: ${page.data.title}
URL: ${page.url}
Source: https://raw.githubusercontent.com/xraph/forge/refs/heads/main/apps/docs/content/docs/${page.path}

${page.data.description}
        
${processed}`;
}