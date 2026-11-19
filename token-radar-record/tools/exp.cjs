// Experiment only (not entry code): candidate rows for the held-out table, the band's coverage, and rank ranges.
const model = require('/home/user/make-fun/token-radar-01/dashboard/model.js'), fs = require('fs');
const snap = JSON.parse(fs.readFileSync(process.argv[2], 'utf8')), radar = model.buildRadar(snap);
const { tokens, rows, board } = radar, M = radar.model, drift = M.drift, log = Math.log10, median = model.median, quantile = model.quantile;
const typical = (errs) => 10 ** median(errs.map(Math.abs));
const curveErr = (term) => (fit, t) => {
  const c = model.fitCurve(fit), res = (u) => u.y - c.a - c.b * u.x;
  if (!term) return res(t);
  const z = fit.map(term), mz = z.reduce((s, v) => s + v, 0) / z.length;
  const slope = fit.reduce((s, u, i) => s + (z[i] - mz) * res(u), 0) / z.reduce((s, v) => s + (v - mz) ** 2, 0);
  return res(t) - slope * (term(t) - mz);
};
// family offsets, shrunk: mean residual of the family in the fit set x n/(n+k)
const famErr = (k) => (fit, t) => {
  const c = model.fitCurve(fit), res = (u) => u.y - c.a - c.b * u.x, mine = fit.filter((u) => u.r.family === t.r.family);
  const off = mine.length ? (mine.reduce((s, u) => s + res(u), 0) / mine.length) * (mine.length / (mine.length + k)) : 0;
  return res(t) - off;
};
const score = (set, price) => {
  const dated = set.filter((t) => t.r.age !== null).sort((p, q) => q.r.age - p.r.age), half = dated.length >> 1;
  const each = set.map((t, i) => price(set.filter((_, j) => j !== i), t)), split = dated.slice(half).map((t) => price(dated.slice(0, half), t));
  return { each, split, miss: typical(each), splitMiss: typical(split), far: 10 ** quantile(each.map(Math.abs), 0.75), half, old: dated.slice(0, half), neu: dated.slice(half) };
};
const carry = (sign) => tokens.map((t) => ({ ...t, x: t.x + sign * log(model.growthCarry(t.r.chg, drift)) }));
const show = (name, s) => console.log(name.padEnd(28), 'LOO x' + s.miss.toFixed(2), ' old>new x' + s.splitMiss.toFixed(2), ' far(Q3) x' + s.far.toFixed(2), ' within3 ' + Math.round(100 * s.each.filter((e) => Math.abs(e) < log(3)).length / s.each.length) + '%');
const flat = (fit, t) => t.y - t.x - median(fit.map((f) => f.y - f.x));
show('flat TVL multiple', score(tokens, flat));
const plain = score(tokens, curveErr()); show('size curve on TVL', plain);
const mdl = score(carry(1), curveErr()); show('+ 7d carry = model', mdl);
show('carry, sign flipped', score(carry(-1), curveErr()));
for (const k of [0, 3, 8, 16]) show('+ family offsets k=' + k, score(carry(1), famErr(k)));
show('plain + family k=8', score(tokens, famErr(8)));
show('+ listing age', score(carry(1), curveErr((t) => log(t.r.aged))));
show('+ chain count', score(carry(1), curveErr((t) => log(t.r.chains.length || 1))));
show('+ fitted 7d change', score(tokens, curveErr((t) => (t.r.chg === null ? drift : Math.max(-50, Math.min(50, t.r.chg))))));
// band coverage out of sample: the P25-P75 of the older half's left-out errors, checked on the newer half priced by the older fit
const older = score(mdl.old, curveErr()).each, q = (p) => quantile(older, p), inside = (lo, hi) => mdl.split.filter((e) => e >= q(lo) && e <= q(hi)).length;
console.log('band out of sample: P25-P75 holds', inside(0.25, 0.75), 'of', mdl.split.length, '| P10-P90 holds', inside(0.1, 0.9), 'of', mdl.split.length, '| in-sample within x3:', Math.round(100 * mdl.each.filter((e) => Math.abs(e) < log(3)).length / mdl.each.length) + '%');
// rank ranges: each row's value shaken by a draw from the model's held-out misses, 200 times
let seed = 20261114; const rnd = () => { seed = (seed * 1664525 + 1013904223) >>> 0; return seed / 4294967296; };
const ranks = board.map(() => []);
for (let s = 0; s < 200; s++) {
  const v = board.map((r) => r.implied * 10 ** mdl.each[Math.floor(rnd() * mdl.each.length)]);
  const order = v.map((x, i) => i).sort((a, b) => v[b] - v[a]); order.forEach((i, pos) => ranks[i].push(pos + 1));
}
for (const i of [0, 1, 2, 3, 4, 9, 19, 49, 99, 149, 197]) console.log('rank', board[i].rank, board[i].name.padEnd(24), 'P10-P90 rank', Math.round(quantile(ranks[i], 0.1)), '-', Math.round(quantile(ranks[i], 0.9)));
