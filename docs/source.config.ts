import {
  defineConfig,
  defineCollections,
  defineDocs,
} from "fumadocs-mdx/config";
import { metaSchema, pageSchema } from "fumadocs-core/source/schema";
import z from "zod";

// You can customise Zod schemas for frontmatter and `meta.json` here
// see https://fumadocs.dev/docs/mdx/collections
export const docs = defineDocs({
  dir: "content/docs",
  docs: {
    schema: pageSchema,
    postprocess: {
      includeProcessedMarkdown: true,
    },
  },
  meta: {
    schema: metaSchema,
  },
});

export const blogPosts = defineCollections({
  type: "doc",
  dir: "content/blog",
  schema: pageSchema.extend({
    author: z.string(),
    date: z.string().date().or(z.date()),
    category: z.string().default("General"),
    tags: z.array(z.string()).default([]),
    coverGradient: z.string().default("from-amber-500/20 via-orange-500/10 to-transparent"),
  }),
});

export default defineConfig({
  mdxOptions: {
    // MDX options
  },
});
