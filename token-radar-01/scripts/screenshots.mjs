// Captures the three screenshots the entry hands in, from the page main.ts writes. Dev tool only:
// main.ts never runs it. Needs Playwright with Chromium and the kit's snapshot.
//
//   node scripts/screenshots.mjs <kit>/input/snapshot.json
//
// The judge is sent the cover fitted into 1024x640 and the other two side by side in 510x640 cells, so
// the board is shot landscape at 16:10, at the 1440px width where the evidence stands beside it, and
// the detail and filtered views portrait at 4:5 at a device pixel ratio of 1.25, so their text lands
// near its CSS size in the judge's view instead of half of it.
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const { chromium } = createRequire(import.meta.url)('playwright');
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const snapshot = resolve(process.argv[2] ?? 'input/snapshot.json');
const out = join(root, 'out'), shots = join(root, 'screenshots');
const SHOTS = [
  { name: '1-board.png', width: 1440, height: 900, scale: 1, hash: '' },
  { name: '2-detail.png', width: 640, height: 800, scale: 1.25, hash: process.argv[3] ?? 'open=mezzanine' },
  { name: '3-filtered.png', width: 640, height: 800, scale: 1.25, hash: process.argv[4] ?? 'verdict=Farm&sort=net&dir=-1' },
];
const build = () => execFileSync(process.execPath, [join(root, 'main.ts'), '--input', snapshot, '--output', out], { stdio: 'inherit' });

// main.ts refuses to run without three screenshots, so the first build gets 1x1 stand-ins.
mkdirSync(shots, { recursive: true });
const BLANK = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64');
for (const s of SHOTS) if (!existsSync(join(shots, s.name))) writeFileSync(join(shots, s.name), BLANK);
build();

const browser = await chromium.launch();
for (const s of SHOTS) {
  const page = await browser.newPage({ viewport: { width: s.width, height: s.height }, deviceScaleFactor: s.scale });
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await page.goto(`${pathToFileURL(join(out, 'dashboard', 'index.html'))}#${s.hash}`);
  await page.waitForSelector('#rows table');
  await page.evaluate(() => document.fonts.ready);
  if (errors.length > 0) throw new Error(`${s.name}: the page threw: ${errors.join('; ')}`);
  await page.screenshot({ path: join(shots, s.name) });
  await page.close();
  process.stdout.write(`screenshots: ${s.name} ${s.width * s.scale}x${s.height * s.scale}\n`);
}
await browser.close();
build(); // again, so dossier.json carries the new captures
