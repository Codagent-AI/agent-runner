// Render-screenshot capture for the and-scene eval tier-2 judge.
//
// and-scene's own `npm run verify` boots a Vite preview and steps every
// presentation through its scenes to catch render errors, but it never
// captures images. The tier-2 judge must SEE what rendered, so this script
// reuses the same proven preview + registry + step-advance approach and adds a
// screenshot per scene.
//
// Run from inside the produced checkout (cwd = repo root) so bare imports like
// `playwright` resolve against the checkout's node_modules:
//
//   SHOTS_OUT=/artifacts/screenshots \
//     node --experimental-strip-types ./.eval-scene-shots.mjs
//
// It records per-presentation coverage in SHOTS_MANIFEST and exits non-zero
// when the step contract, navigation, or capture is incomplete. The harness may
// then give the judge model one penalized evidence-repair attempt.
import { spawn } from 'node:child_process'
import { createServer } from 'node:net'
import { mkdir, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'
import { chromium } from 'playwright'

const ROOT = process.cwd()
const OUT = process.env.SHOTS_OUT || '/artifacts/screenshots'
const MANIFEST = process.env.SHOTS_MANIFEST || '/artifacts/screenshot-manifest.json'
const VITE_BIN = join(ROOT, 'node_modules', '.bin', 'vite')
const MAX_STEPS = 50

function getFreePort() {
  return new Promise((resolve, reject) => {
    const srv = createServer()
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address()
      const port = typeof addr === 'object' && addr ? addr.port : 0
      srv.close((err) => (err ? reject(err) : resolve(port)))
    })
    srv.on('error', reject)
  })
}

async function waitForPreview(port, child, deadlineMs = 30_000) {
  const started = Date.now()
  while (Date.now() - started < deadlineMs) {
    if (child.exitCode !== null || child.signalCode !== null) {
      throw new Error(`vite preview exited before ready (code ${child.exitCode})`)
    }
    try {
      const res = await fetch(`http://localhost:${port}/`)
      const html = await res.text()
      if (res.ok && html.includes('id="root"')) return
    } catch {
      // server not listening yet
    }
    await new Promise((r) => setTimeout(r, 200))
  }
  throw new Error('vite preview did not become ready within 30s')
}

function startPreview(port) {
  return new Promise((resolve, reject) => {
    const child = spawn(
      VITE_BIN,
      ['preview', '--port', String(port), '--strictPort', '--host', 'localhost'],
      { cwd: ROOT, stdio: 'ignore', detached: true },
    )
    child.unref()
    child.on('error', reject)
    waitForPreview(port, child)
      .then(() => resolve(child))
      .catch((err) => {
        stopPreview(child)
        reject(err)
      })
  })
}

function stopPreview(child) {
  if (!child.pid) return
  try {
    process.kill(-child.pid, 'SIGKILL')
  } catch {
    try {
      child.kill('SIGKILL')
    } catch {
      // already gone
    }
  }
}

/** Enumerate presentations from the spec-mandated registry; fall back to root. */
async function readRegistry() {
  try {
    const url = pathToFileURL(join(ROOT, 'src/presentations/index.ts')).href
    const mod = await import(url)
    if (Array.isArray(mod.presentations) && mod.presentations.length > 0) {
      return mod.presentations
    }
  } catch (err) {
    console.error(`registry read failed, falling back to root route: ${err.message}`)
  }
  return [{ slug: '' }]
}

async function shootPresentation(page, port, slug) {
  const route = slug ? `/${slug}` : '/'
  const dir = join(OUT, slug || 'index')
  await mkdir(dir, { recursive: true })
  await page.goto(`http://localhost:${port}${route}`, { waitUntil: 'load', timeout: 30_000 })

  const progress = page.locator('[data-step-count]')
  let stepCount = 1
  let expectedScreenshots = 1
  let capturedScreenshots = 0
  const errors = []
  try {
    await progress.waitFor({ timeout: 5_000 })
    const count = Number(await progress.getAttribute('data-step-count'))
    if (!Number.isFinite(count) || count < 1) {
      throw new Error(`invalid data-step-count value: ${String(count)}`)
    }
    expectedScreenshots = count
    if (count > MAX_STEPS) {
      throw new Error(`data-step-count ${count} exceeds capture limit ${MAX_STEPS}`)
    }
    stepCount = count
  } catch (err) {
    const message = `step contract unavailable for ${slug || 'index'}; falling back to one frame: ${err.message}`
    errors.push(message)
    console.error(message)
  }

  for (let i = 0; i < stepCount; i++) {
    await page.waitForTimeout(400)
    const name = `step-${String(i).padStart(2, '0')}.png`
    await page.screenshot({ path: join(dir, name) })
    capturedScreenshots += 1
    if (i < stepCount - 1) {
      await page.keyboard.press('ArrowRight')
      try {
        await page.waitForFunction(
          (expected) => {
            const el = document.querySelector('[data-step-count]')
            return el && Number(el.getAttribute('data-step-index')) === expected
          },
          i + 1,
          { timeout: 5_000 },
        )
      } catch (err) {
        const message = `step advance stalled for ${slug || 'index'} at ${i + 1}: ${err.message}`
        errors.push(message)
        console.error(message)
      }
    }
  }

  return {
    slug: slug || 'index',
    expectedScreenshots,
    capturedScreenshots,
    complete: errors.length === 0 && capturedScreenshots === expectedScreenshots,
    errors,
  }
}

async function main() {
  await mkdir(OUT, { recursive: true })
  const presentations = await readRegistry()
  const port = await getFreePort()
  const child = await startPreview(port)
  const browser = await chromium.launch()
  const coverage = []
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 720 } })
    for (const { slug } of presentations) {
      try {
        coverage.push(await shootPresentation(page, port, slug))
      } catch (err) {
        console.error(`screenshot ${slug || 'index'} failed: ${err.message}`)
        coverage.push({
          slug: slug || 'index',
          expectedScreenshots: 1,
          capturedScreenshots: 0,
          complete: false,
          errors: [err.message],
        })
      }
    }
  } finally {
    await browser.close()
    stopPreview(child)
  }
  const manifest = {
    expectedPresentations: presentations.length,
    capturedPresentations: coverage.filter(({ capturedScreenshots }) => capturedScreenshots > 0).length,
    expectedScreenshots: coverage.reduce((sum, item) => sum + item.expectedScreenshots, 0),
    capturedScreenshots: coverage.reduce((sum, item) => sum + item.capturedScreenshots, 0),
    complete: coverage.length === presentations.length && coverage.every(({ complete }) => complete),
    presentations: coverage,
  }
  await writeFile(MANIFEST, `${JSON.stringify(manifest, null, 2)}\n`)
  console.log(`captured ${manifest.capturedScreenshots}/${manifest.expectedScreenshots} screenshot(s) for ${manifest.capturedPresentations}/${manifest.expectedPresentations} presentation(s) into ${OUT}`)
  if (!manifest.complete) process.exitCode = 1
}

main().catch(async (err) => {
  console.error(err)
  await writeFile(MANIFEST, `${JSON.stringify({
    expectedPresentations: 0,
    capturedPresentations: 0,
    expectedScreenshots: 0,
    capturedScreenshots: 0,
    complete: false,
    presentations: [],
    fatalError: err.message,
  }, null, 2)}\n`).catch(() => {})
  process.exit(1)
})
