import { type Page } from '@/lib/source';

export async function getLLMText(page: Page) {
  if (page.data.type === 'openapi') return '';

  const category =
    {
      go: 'Authsome Golang (the Go library for Authsome)',
      rust: 'Authsome Rust (the Rust library for Authsome)',
      ui: 'Authsome UI (the one stop Auth UI Kit)',
      cli: 'Authsome CLI (the CLI tool for automating Authsome apps)',
    }[page.slugs[0]] ?? page.slugs[0];

  const processed = await page.data.getText('processed');

  return `# ${category}: ${page.data.title}
URL: ${page.url}
Source: https://raw.githubusercontent.com/xraph/authsome/refs/heads/main/apps/docs/content/docs/${page.path}

${page.data.description}
        
${processed}`;
}