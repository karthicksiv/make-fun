// QA only: x-overflow of every view at many widths.
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
const { chromium } = createRequire(import.meta.url)('playwright');
const [index] = process.argv.slice(2);
const browser = await chromium.launch();
const views = ['', 'verdict=Farm&sort=net&dir=-1', 'tab=traded', 'tab=aside', 'tab=model', 'open=mezzanine', 'q=zzzz'];
const widths = [1920, 1700, 1620, 1619, 1600, 1440, 1439, 1400, 1300, 1280, 1261, 1260, 1181, 1180, 1152, 1121, 1120, 1101, 1061, 1060, 1024, 961, 960, 921, 920, 900, 840, 800, 768, 761, 760, 700, 640, 601, 600, 561, 560, 521, 480, 430, 390, 360];
let bad = 0;
for (const hash of views) {
  const line = [];
  for (const w of widths) {
    const page = await browser.newPage({ viewport: { width: w, height: 800 }, deviceScaleFactor: 1 });
    const errors = []; page.on('pageerror', (e) => errors.push(e.message));
    await page.goto(`${pathToFileURL(index)}#${hash}`); await page.evaluate(() => document.fonts.ready);
    const over = await page.evaluate(() => {
      const d = document.querySelector('.detail');
      return Math.max(document.documentElement.scrollWidth - document.documentElement.clientWidth, d ? d.scrollWidth - d.clientWidth : 0);
    });
    if (over > 0 || errors.length) { bad++; line.push(`${w}:${over}${errors.length ? '!' + errors[0] : ''}`); }
    await page.close();
  }
  console.log((hash || 'board').padEnd(30), line.length ? line.join(' ') : 'ok at all ' + widths.length + ' widths');
}
await browser.close();
console.log(bad ? `${bad} overflow(s)` : 'NO OVERFLOW');
