// Experiment only: rank ranges from refitting both fits on resamples.
const model = require('/home/user/make-fun/token-radar-01/dashboard/model.js'), fs = require('fs');
const snap = JSON.parse(fs.readFileSync(process.argv[2], 'utf8')), radar = model.buildRadar(snap), { board, tokens, rows } = radar, alone = rows.filter((r) => r.alone), drift = radar.model.drift;
for (const REFITS of [100, 200]) {
  let seed = 20261114; const t0 = Date.now();
  const draw = (set) => set.map(() => set[Math.floor(((seed = (seed * 1664525 + 1013904223) >>> 0) / 4294967296) * set.length)]);
  const seen = board.map(() => []);
  for (let i = 0; i < REFITS; i++) {
    const m = { curve: model.fitCurve(draw(tokens)), odds: model.fitOdds(draw(alone)), drift };
    const v = board.map((r) => model.impliedValue(m, r).implied);
    v.map((_, j) => j).sort((p, q) => v[q] - v[p]).forEach((j, at) => seen[j].push(at + 1));
  }
  const out = board.map((r, j) => [r.rank, Math.round(model.quantile(seen[j], 0.1)), Math.round(model.quantile(seen[j], 0.9))]);
  console.log(REFITS, 'refits', Date.now() - t0, 'ms |', out.slice(0, 14).map((x) => `${x[0]}:${x[1]}-${x[2]}`).join(' '), '| 50:', out[49].join('-'), '100:', out[99].join('-'), '198:', out[197].join('-'), '| mean width', (out.reduce((s, x) => s + x[2] - x[1], 0) / out.length).toFixed(1), '| rank outside its range:', out.filter((x) => x[0] < x[1] || x[0] > x[2]).length);
}
