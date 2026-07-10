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
// It is intentionally best-effort: a broken presentation logs an error and the
// pass moves on rather than aborting the harness. Zero screenshots is itself a
// signal the tier-2 judge step treats as a failure.
import { spawn } from 'node:child_process'
import { createServer } from 'node:net'
import { mkdir } from 'node:fs/promises'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'
import { chromium } from 'playwright'

const ROOT = process.cwd()
const OUT = process.env.SHOTS_OUT || '/artifacts/screenshots'
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

  const progress = page.locator('[data-testid="step-progress"]')
  let stepCount = 1
  try {
    await progress.waitFor({ timeout: 5_000 })
    const count = Number(await progress.getAttribute('data-step-count'))
    if (Number.isFinite(count) && count >= 1) stepCount = Math.min(count, MAX_STEPS)
  } catch {
    // No step-progress contract; capture a single frame of whatever rendered.
  }

  for (let i = 0; i < stepCount; i++) {
    await page.waitForTimeout(400)
    const name = `step-${String(i).padStart(2, '0')}.png`
    await page.screenshot({ path: join(dir, name) })
    if (i < stepCount - 1) {
      await page.keyboard.press('ArrowRight')
      try {
        await page.waitForFunction(
          (expected) => {
            const el = document.querySelector('[data-testid="step-progress"]')
            return el && Number(el.getAttribute('data-step-index')) === expected
          },
          i + 1,
          { timeout: 5_000 },
        )
      } catch {
        // Advance stalled; the next screenshot still records the current frame.
      }
    }
  }
}

async function main() {
  await mkdir(OUT, { recursive: true })
  const presentations = await readRegistry()
  const port = await getFreePort()
  const child = await startPreview(port)
  const browser = await chromium.launch()
  let captured = 0
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 720 } })
    for (const { slug } of presentations) {
      try {
        await shootPresentation(page, port, slug)
        captured += 1
      } catch (err) {
        console.error(`screenshot ${slug || 'index'} failed: ${err.message}`)
      }
    }
  } finally {
    await browser.close()
    stopPreview(child)
  }
  console.log(`captured screenshots for ${captured}/${presentations.length} presentation(s) into ${OUT}`)
}

main().catch((err) => {
  console.error(err)
  process.exit(1)
})
