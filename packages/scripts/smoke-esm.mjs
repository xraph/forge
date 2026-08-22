#!/usr/bin/env node
/**
 * Proves the built package is loadable by Node's own ESM loader.
 *
 * Run from a package directory, after `npm run build`.
 *
 * This exists because the failure it catches is invisible everywhere else.
 * `tsc` does not add file extensions to relative import specifiers, so a
 * `"type": "module"` package built by plain `tsc` emits `from './devtools'` --
 * which every bundler resolves and Node resolves for nothing:
 *
 *   ERR_MODULE_NOT_FOUND: Cannot find module '.../dist/devtools'
 *
 * The build config now sets `moduleResolution: "NodeNext"`, so TypeScript
 * rejects an extensionless relative import at compile time (TS2835). That is
 * the primary guard. This is the end-to-end one: it packs a tarball and
 * imports it from a directory that has no link back to this workspace, which
 * additionally catches a broken `exports` map, a subpath that was never built,
 * and a `files` list that forgot to ship something. A workspace symlink can
 * hide all three; a tarball cannot.
 */
import { execFileSync } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

const run = (cmd, args, cwd) =>
  execFileSync(cmd, args, { cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] })

const pkgDir = process.cwd()
const pkg = JSON.parse(readFileSync(join(pkgDir, 'package.json'), 'utf8'))
const work = mkdtempSync(join(tmpdir(), 'forge-smoke-'))

try {
  // Pack this package, plus any sibling @forge-go peer it needs at runtime.
  const peers = pkg.peerDependencies ?? {}
  const dev = pkg.devDependencies ?? {}

  const tarballs = []
  const pack = (dir) => {
    const out = run('npm', ['pack', '--pack-destination', work, '--silent'], dir).trim()
    const name = out.split('\n').pop().trim()
    tarballs.push(join(work, name))
  }

  pack(pkgDir)
  for (const name of Object.keys(peers)) {
    if (!name.startsWith('@forge-go/')) continue
    pack(resolve(pkgDir, '..', name.replace('@forge-go/', '')))
  }

  // Third-party peers come from this package's own devDependencies, so the
  // smoke test installs the versions the package is actually developed and
  // tested against rather than a guess.
  const external = Object.keys(peers)
    .filter((n) => !n.startsWith('@forge-go/'))
    .map((n) => (dev[n] ? `${n}@${dev[n]}` : n))

  // Angular's runtime imports rxjs; react's imports react-dom in some paths.
  // Anything already in devDependencies that a peer needs is pulled in here.
  for (const extra of ['rxjs', 'zone.js', 'react-dom']) {
    if (dev[extra] && !external.some((e) => e.startsWith(`${extra}@`))) {
      external.push(`${extra}@${dev[extra]}`)
    }
  }

  const consumer = join(work, 'consumer')
  run('mkdir', ['-p', consumer])
  writeFileSync(
    join(consumer, 'package.json'),
    JSON.stringify({ name: 'smoke-consumer', version: '1.0.0', type: 'module', private: true }, null, 2),
  )

  if (external.length > 0) {
    run('npm', ['install', '--silent', '--no-audit', '--no-fund', ...external], consumer)
  }
  run('npm', ['install', '--silent', '--no-audit', '--no-fund', ...tarballs], consumer)

  // Every entry point the package advertises, not just the main one.
  const specifiers =
    pkg.exports != null
      ? Object.keys(pkg.exports).map((sub) =>
          sub === '.' ? pkg.name : `${pkg.name}/${sub.replace(/^\.\//, '')}`,
        )
      : [pkg.name]

  let failed = false
  for (const spec of specifiers) {
    try {
      const n = run(
        'node',
        [
          '--input-type=module',
          '-e',
          `import(${JSON.stringify(spec)}).then(m => console.log(Object.keys(m).length))`,
        ],
        consumer,
      ).trim()
      console.log(`  OK   ${spec} (${n} exports)`)
    } catch (err) {
      const msg = String(err.stderr || err.message)
      const code = msg.match(/(ERR_[A-Z_]+)/)?.[1] ?? 'error'
      const first = msg.split('\n').find((l) => l.includes('Error')) ?? code
      console.log(`  FAIL ${spec} -> ${code}: ${first.trim()}`)
      failed = true
    }
  }

  if (failed) {
    console.error(`\n${pkg.name} is not loadable by Node's ESM loader.`)
    process.exit(1)
  }
  console.log(`\n${pkg.name}@${pkg.version}: all entry points load under Node ESM.`)
} finally {
  rmSync(work, { recursive: true, force: true })
}
