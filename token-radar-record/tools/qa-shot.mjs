// QA only: extra captures of views that are not submitted (model page, other tabs, empty state, mobile).
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
const { chromium } = createRequire(import.meta.url)('playwright');
const [index, outDir] = process.argv.slice(2);
const shots = [['model', 1152, 720, 'tab=model'], ['traded', 1152, 720, 'tab=traded'], ['aside', 1152, 720, 'tab=aside'], ['empty', 1152, 720, 'q=zzzz'], ['detail-wide', 1152, 720, 'open=steakhouse-financial'], ['phone', 390, 844, '']];
const browser = await chromium.launch();
for (const [name, w, h, hash] of shots) {
  const page = await browser.newPage({ viewport: { width: w, height: h }, deviceScaleFactor: 1 });
  const errors = []; page.on('pageerror', (e) => errors.push(e.message)); page.on('console', (m) => { if (m.type() === 'error') errors.push(m.text()); });
  await page.goto(`${pathToFileURL(index)}#${hash}`); await page.evaluate(() => document.fonts.ready);
  await page.screenshot({ path: `${outDir}/qa-${name}.png` });
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  console.log(name, 'errors:', errors.length ? errors.join(' | ') : 'none', 'x-overflow:', overflow);
  await page.close();
}
await browser.close();
