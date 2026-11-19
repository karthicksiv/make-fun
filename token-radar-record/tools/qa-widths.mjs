// QA only: the board at many widths: page x-overflow, and whether any pane of the evidence strip spills.
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
const { chromium } = createRequire(import.meta.url)('playwright');
const [index, outDir] = process.argv.slice(2);
const browser = await chromium.launch();
for (const w of [1600, 1440, 1280, 1152, 1024, 900, 800, 761, 760, 640, 390]) {
  const page = await browser.newPage({ viewport: { width: w, height: 800 }, deviceScaleFactor: 1 });
  const errors = []; page.on('pageerror', (e) => errors.push(e.message)); page.on('console', (m) => { if (m.type() === 'error') errors.push(m.text()); });
  await page.goto(`${pathToFileURL(index)}#`); await page.evaluate(() => document.fonts.ready);
  const r = await page.evaluate(() => {
    const over = document.documentElement.scrollWidth - document.documentElement.clientWidth;
    const proof = document.querySelector('.proof'), shown = proof && getComputedStyle(proof).display !== 'none';
    const spills = shown ? [...proof.children].map((d) => d.scrollWidth - d.clientWidth) : [];
    const h = shown ? Math.round(proof.getBoundingClientRect().height) : 0;
    const rows = [...document.querySelectorAll('#rows tbody tr')].filter((tr) => tr.getBoundingClientRect().bottom <= innerHeight - 30).length;
    const tests = shown ? [...proof.querySelectorAll('.tests td:first-child')].map((td) => Math.round(td.getBoundingClientRect().height)) : [];
    return { over, shown, spills, h, rows, tests };
  });
  console.log(w, JSON.stringify(r), 'errors:', errors.length ? errors.join(' | ') : 'none');
  if ([1024, 800, 1600].includes(w)) await page.screenshot({ path: `${outDir}/qa-w${w}.png` });
  await page.close();
}
await browser.close();
