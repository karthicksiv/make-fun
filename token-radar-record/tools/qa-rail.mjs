// QA only: at each width, does the table stay inside its track (clear of the rail), do the panes spill, how many rows show.
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
const { chromium } = createRequire(import.meta.url)('playwright');
const [index, outDir] = process.argv.slice(2);
const browser = await chromium.launch();
for (const [w, h] of [[1920, 1080], [1700, 950], [1620, 900], [1619, 900], [1500, 900], [1440, 900], [1439, 900], [1400, 900], [1300, 800], [1261, 800], [1260, 800], [1181, 800], [1180, 800], [1101, 800], [1100, 800], [1061, 800], [1060, 800], [1024, 800], [961, 800], [960, 800], [900, 800], [921, 800], [920, 800], [840, 800], [768, 1024], [761, 800]]) {
  const page = await browser.newPage({ viewport: { width: w, height: h }, deviceScaleFactor: 1 });
  const errors = []; page.on('pageerror', (e) => errors.push(e.message));
  await page.goto(`${pathToFileURL(index)}#`); await page.evaluate(() => document.fonts.ready);
  const r = await page.evaluate(() => {
    const t = document.querySelector('#rows table').getBoundingClientRect(), rows = document.querySelector('#rows').getBoundingClientRect(), p = document.querySelector('.proof'), pr = p.getBoundingClientRect();
    const rail = getComputedStyle(p).position === 'sticky';
    const spills = [...p.children].filter((d) => getComputedStyle(d).display !== 'none').map((d) => d.scrollWidth - d.clientWidth);
    const tall = [...document.querySelectorAll('#rows tbody tr')].slice(0, 40).map((tr) => Math.round(tr.getBoundingClientRect().height));
    const wrapTests = [...p.querySelectorAll('.tests tbody td:first-child')].map((td) => Math.round(td.getBoundingClientRect().height));
    return { rail, tableOver: Math.round(t.right - rows.right), gapToRail: rail ? Math.round(pr.left - t.right) : null, railBottom: rail ? Math.round(pr.bottom) : null, stripH: rail ? null : Math.round(pr.height), spills, rowHeights: [...new Set(tall)].join('/'), testsRowH: [...new Set(wrapTests)].join('/'),
      rowsShown: [...document.querySelectorAll('#rows tbody tr')].filter((tr) => tr.getBoundingClientRect().bottom <= innerHeight - 30).length };
  });
  console.log(w + 'x' + h, JSON.stringify(r), errors.length ? 'ERR ' + errors[0] : '');
  if ([1920, 1300, 1101].includes(w)) await page.screenshot({ path: `${outDir}/qa-w${w}.png` });
  await page.close();
}
await browser.close();
