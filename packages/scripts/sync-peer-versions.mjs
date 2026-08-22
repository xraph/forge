#!/usr/bin/env node
/**
 * Locks every `@forge-go/*` peer range to the package's own version.
 *
 * The five client packages publish in lockstep from one tag: the release
 * workflow runs `npm version <tag>` in each directory and then publishes. But
 * `npm version` rewrites exactly one field -- `.version` -- and leaves
 * `peerDependencies` alone. So a hand-written `"@forge-go/client-core":
 * "^0.1.0"` survived every release into 1.9.x, and no published core could
 * satisfy it:
 *
 *   └─┬ @forge-go/client-react 1.9.6
 *     └── ✕ unmet peer @forge-go/client-core@^0.1.0: found 1.9.6
 *
 * Pinning the range by hand only moves the drift to the next release. Deriving
 * it from the version that was just stamped removes the chance to drift at
 * all, which is why this runs from `prepack` -- npm fires that after
 * `npm version` and before the tarball is assembled, so the published
 * manifest carries the corrected range even though the working tree that
 * produced it never did.
 *
 * Run with --check to assert without writing (CI uses this).
 */
import { readFileSync, writeFileSync } from 'node:fs'

const check = process.argv.includes('--check')
const path = new URL('package.json', `file://${process.cwd()}/`)

const pkg = JSON.parse(readFileSync(path, 'utf8'))
const peers = pkg.peerDependencies ?? {}

const want = `^${pkg.version}`
const drifted = Object.keys(peers)
  .filter((name) => name.startsWith('@forge-go/'))
  .filter((name) => peers[name] !== want)

if (drifted.length === 0) {
  if (!check) console.log(`${pkg.name}: peer ranges already at ${want}`)
  process.exit(0)
}

if (check) {
  console.error(`${pkg.name}@${pkg.version}: peer range drift`)
  for (const name of drifted) {
    console.error(`  ${name}: "${peers[name]}" should be "${want}"`)
  }
  console.error('\nRun `npm run sync-peers` in the package directory.')
  process.exit(1)
}

for (const name of drifted) {
  console.log(`${pkg.name}: ${name} "${peers[name]}" -> "${want}"`)
  peers[name] = want
}

writeFileSync(path, `${JSON.stringify(pkg, null, 2)}\n`)
