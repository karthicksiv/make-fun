// QA only: the board table's natural (max-content) width with each set of columns shown.
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
const { chromium } = createRequire(import.meta.url)('playwright');
const browser = await chromium.launch();
for (const w of [2000, 1300, 1150, 1080, 1000, 900, 640]) {
  const page = await browser.newPage({ viewport: { width: w, height: 900 } });
  await page.goto(`${pathToFileURL(process.argv[2])}#`); await page.evaluate(() => document.fonts.ready);
  const r = await page.evaluate(() => {
    const t = document.querySelector('#rows table'); t.style.width = 'max-content';
    const widest = (sel) => Math.max(...[...document.querySelectorAll(sel)].map((e) => Math.ceil(e.getBoundingClientRect().width)));
    return { natural: Math.ceil(t.getBoundingClientRect().width), nameCol: widest('#rows td:nth-child(2)'), farmCol: widest('#rows td.farm'), cols: [...document.querySelectorAll('#rows thead th')].filter((th) => getComputedStyle(th).display !== 'none').map((th) => th.textContent + ':' + Math.ceil(th.getBoundingClientRect().width)).join(' ') };
  });
  console.log(w, JSON.stringify(r));
  await page.close();
}
await browser.close();
