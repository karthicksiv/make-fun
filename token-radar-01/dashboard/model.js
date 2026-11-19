// model.js: the whole value model, in the order a row is priced. main.ts runs this file to write
// board.json and the page runs the same file on the same snapshot, so no figure on screen is typed in:
// each is fitted on, or read from, the snapshot's ten fields. No protocol is named anywhere in this file.
//
//   implied value = sizeCurve(TVL x growthCarry(7-day change)) x launchOdds(days with no token, category)

// Never ranked, each with its reason: forms of a token that already trades, which launch no token of
// their own. Told by DefiLlama's category, else by a version marker or a word in the name.
const DERIVED = [
  [/^(Bridge|Canonical Bridge)$/, 'a bridge: it carries tokens that already trade'],
  [/^(Liquid Staking|Liquid Restaking|Restaked BTC)$/, 'a staked form of a token that already trades'],
  [/^CEX$/, 'an exchange custody balance, not a protocol'],
];
const NOT_ALONE = [
  [/\s(V\d+(\.\d+)?|\d+\.\d+)$/, 'one version of a protocol, not a launch of its own'],
  [/\b(wrapped|staked|staking)\b/i, 'a wrapped or staked product, not a protocol of its own'],
];
const setAside = (p) => (DERIVED.find(([rx]) => rx.test(p.category || '')) || NOT_ALONE.find(([rx]) => rx.test(p.name)) || [])[1] || null;
const FAMILIES = [
  ['Lending', /lend|cdp|collateral/i],
  ['Trading', /dex|deriv|option|predict|synth|otc|nft|launchpad|liquidity/i],
  ['RWA & stables', /rwa|stable|btc|reserve|payment|card/i],
  ['Staking', /stak|governance|locker/i],
  ['Vaults', /curator|allocator|yield|farm|index|basis|cedefi/i],
];
const HORIZON = 730; // days ahead the launch odds look
const CARRY_CAP = 0.25; // the 7-day move is carried one more week, held to +/-25%
const REFITS = 100; // times both fits are refitted on resamples, to say how firm each rank is

const median = (v) => quantile(v, 0.5);
function quantile(v, q) {
  const s = [...v].sort((a, b) => a - b), at = (s.length - 1) * q, lo = Math.floor(at);
  return s[lo] + (s[Math.min(lo + 1, s.length - 1)] - s[lo]) * (at - lo);
}
const familyOf = (category) => (FAMILIES.find(([, rx]) => rx.test(category || '')) || ['Infra'])[0];

/** Every snapshot protocol as a row, sorted into the three groups the board keeps apart. */
function classify(snap) {
  const day = (d) => Date.parse(d + 'T00:00:00Z') / 864e5, now = day(snap.as_of), watch = new Set(snap.watchlist);
  const rows = snap.protocols.map((p) => {
    const token = p.gecko_id !== null || p.symbol !== null, why = setAside(p);
    return {
      slug: p.slug, name: p.name, cat: p.category || 'Uncategorised', family: familyOf(p.category), chains: p.chains,
      tvl: p.tvl_usd, chg: p.change_7d, age: p.listed_at ? now - day(p.listed_at) : null,
      mcap: p.mcap_usd > 0 ? p.mcap_usd : null, symbol: p.symbol, watch: watch.has(p.slug), alone: !why, why,
      // traded: a token is recorded. aside: none recorded, but not a launch of its own. pre: the board.
      kind: token ? 'traded' : why ? 'aside' : 'pre',
    };
  });
  // DefiLlama dates 15% of protocols with no listing day; 9 in 10 of those record a token, the profile
  // of the oldest cohort, so an undated protocol is aged as the oldest dated one.
  const oldest = Math.max(...rows.map((r) => r.age || 0));
  for (const r of rows) r.aged = r.age === null ? oldest : Math.max(r.age, 14);
  return rows;
}

/** Step 1, TVL. log10(market cap) = a + b x log10(TVL / $100M), Huber-weighted so one outlier token
 *  cannot tilt the line. Fitted on every traded stand-alone token the snapshot gives a market cap. */
function fitCurve(tokens) {
  let a = 0, b = 0, w = tokens.map(() => 1);
  for (let pass = 0; pass < 25; pass++) {
    let sw = 0, sx = 0, sy = 0, sxx = 0, sxy = 0;
    tokens.forEach((t, i) => { sw += w[i]; sx += w[i] * t.x; sy += w[i] * t.y; sxx += w[i] * t.x * t.x; sxy += w[i] * t.x * t.y; });
    b = (sw * sxy - sx * sy) / (sw * sxx - sx * sx);
    a = (sy - b * sx) / sw;
    const miss = tokens.map((t) => Math.abs(t.y - a - b * t.x)), scale = median(miss) / 0.6745 || 1;
    w = miss.map((m) => Math.min(1, (1.345 * scale) / (m || 1e-9)));
  }
  return { a, b };
}
const sizeCurve = (curve, tvl) => 10 ** (curve.a + curve.b * Math.log10(tvl / 1e8));

/** Step 2, seven-day growth. One more week of the protocol's move beyond the market's median week
 *  is carried into the TVL the curve reads, held to +/-25% so a single week cannot run the value. */
const growthCarry = (chg, drift) => 1 + Math.max(-CARRY_CAP, Math.min(CARRY_CAP, ((chg === null ? drift : chg) - drift) / 100));

/** Step 3, time with no token. P(a stand-alone protocol records a token | age, category) is a logistic
 *  in log age with a shrunk intercept per category, fitted on every stand-alone protocol, traded or not. */
function fitOdds(set) {
  const m = { a: 0, b: 0, cat: {} }, SHRINK = 3, groups = {};
  for (const r of set) (groups[r.cat] = groups[r.cat] || []).push(r);
  const newton = (rows, x, shrink, get, put) => {
    let grad = -shrink * get(), hess = shrink;
    for (const r of rows) { const p = tokenShare(m, r.aged, r.cat); grad += x(r) * ((r.kind === 'traded') - p); hess += x(r) ** 2 * p * (1 - p); }
    put(get() + grad / (hess || 1));
  };
  for (let pass = 0; pass < 30; pass++) {
    newton(set, () => 1, 0, () => m.a, (v) => { m.a = v; });
    newton(set, (r) => Math.log(r.aged / 365), 0, () => m.b, (v) => { m.b = v; });
    for (const c in groups) newton(groups[c], () => 1, SHRINK, () => m.cat[c] || 0, (v) => { m.cat[c] = v; });
  }
  return m;
}
const tokenShare = (m, days, cat) => 1 / (1 + Math.exp(-(m.a + m.b * Math.log(days / 365) + (m.cat[cat] || 0))));
/** Of look-alikes still tokenless at this age, the share that record a token within HORIZON days:
 *  the longer a protocol has run with no token, the flatter its curve and the lower its odds. */
function launchOdds(m, r) {
  const now = tokenShare(m, r.aged, r.cat);
  return (tokenShare(m, r.aged + HORIZON, r.cat) - now) / (1 - now);
}

/** One row priced: the three steps above and their product. */
function impliedValue(model, r) {
  const carry = growthCarry(r.chg, model.drift);
  const ifLaunched = sizeCurve(model.curve, r.tvl * carry), odds = launchOdds(model.odds, r);
  return { carry, ifLaunched, odds, implied: ifLaunched * odds };
}

/** Held-out checks, run on every load. Each traded token is priced by a curve fitted without it;
 *  the older-listed half prices the newer half; launch odds are scored on protocols left out of the fit. */
function heldOut(rows, tokens, drift) {
  const log = Math.log10, typical = (errs) => 10 ** median(errs.map(Math.abs));
  const flatErr = (fit, t) => t.y - t.x - median(fit.map((f) => f.y - f.x));
  // the curve's miss on token t when fitted on `fit`; an extra term, if given, is fitted on its residuals
  const curveErr = (term) => (fit, t) => {
    const c = fitCurve(fit), res = (u) => u.y - c.a - c.b * u.x;
    if (!term) return res(t);
    const z = fit.map(term), mz = z.reduce((s, v) => s + v, 0) / z.length;
    const slope = fit.reduce((s, u, i) => s + (z[i] - mz) * res(u), 0) / z.reduce((s, v) => s + (v - mz) ** 2, 0);
    return res(t) - slope * (term(t) - mz);
  };
  // two tests of one pricing rule: each token left out in turn, and the older-listed half pricing the newer
  const score = (set, price) => {
    const dated = set.filter((t) => t.r.age !== null).sort((p, q) => q.r.age - p.r.age), half = dated.length >> 1;
    const each = set.map((t, i) => price(set.filter((_, j) => j !== i), t)), old = dated.slice(0, half), late = dated.slice(half).map((t) => price(old, t));
    return { each, old, late, miss: typical(each), split: typical(late), fit: half, test: late.length };
  };
  const carried = tokens.map((t) => ({ ...t, x: t.x + log(growthCarry(t.r.chg, drift)) }));
  const model = score(carried, curveErr()), row = (label, s, used) => ({ label, miss: s.miss, split: s.split, used });
  // is the printed range honest? the P25-P75 of the older half's own left-out misses, counted on the newer half
  const early = model.old.map((t, i) => curveErr()(model.old.filter((_, j) => j !== i), t));
  const holds = model.late.filter((e) => e >= quantile(early, 0.25) && e <= quantile(early, 0.75)).length;
  const alone = rows.filter((r) => r.alone), FOLDS = 10;
  let hits = 0;
  for (let k = 0; k < FOLDS; k++) {
    const m = fitOdds(alone.filter((_, i) => i % FOLDS !== k));
    alone.forEach((r, i) => { if (i % FOLDS === k) hits += (tokenShare(m, r.aged, r.cat) > 0.5) === (r.kind === 'traded'); });
  }
  const share = alone.filter((r) => r.kind === 'traded').length / alone.length;
  return {
    n: tokens.length, fit: model.fit, test: model.test, miss: model.miss, split: model.split, holds,
    band: [0.25, 0.75].map((q) => quantile(model.each, q)), // the P25 and P75 of the model's own held-out misses
    tests: [
      row('flat TVL multiple', score(tokens, flatErr)),
      row('size curve on TVL', score(tokens, curveErr())),
      row('+ 7d carry = model', model, true),
      row('+ listing age', score(carried, curveErr((t) => log(t.r.aged)))),
      row('+ chain count', score(carried, curveErr((t) => log(t.r.chains.length || 1)))),
    ],
    odds: { n: alone.length, hit: hits / alone.length, base: Math.max(share, 1 - share) },
  };
}

/** How firm each rank is. Both fits are refitted REFITS times, each on a resample of what it was fitted
 *  on, and the board re-ranked: a row's range is where 8 in 10 refits put it. */
function rankRanges(board, tokens, alone, drift) {
  let seed = 20261114; // fixed, so every run draws the same resamples
  const draw = (set) => set.map(() => set[Math.floor(((seed = (seed * 1664525 + 1013904223) >>> 0) / 2 ** 32) * set.length)]);
  const seen = board.map(() => []);
  for (let i = 0; i < REFITS; i++) {
    const m = { curve: fitCurve(draw(tokens)), odds: fitOdds(draw(alone)), drift }, v = board.map((r) => impliedValue(m, r).implied);
    v.map((_, j) => j).sort((p, q) => v[q] - v[p]).forEach((j, at) => seen[j].push(at + 1));
  }
  board.forEach((r, j) => { r.lo = Math.round(quantile(seen[j], 0.1)); r.hi = Math.round(quantile(seen[j], 0.9)); });
}

/** Snapshot in, radar out: the fitted model, its checks, and the board ranked by implied value. */
function buildRadar(snap) {
  const rows = classify(snap);
  const tokens = rows.filter((r) => r.kind === 'traded' && r.alone && r.mcap).map((r) => ({ r, x: Math.log10(r.tvl / 1e8), y: Math.log10(r.mcap) }));
  const drift = median(rows.filter((r) => r.chg !== null).map((r) => r.chg));
  const model = { curve: fitCurve(tokens), odds: fitOdds(rows.filter((r) => r.alone)), drift, horizon: HORIZON, carryCap: CARRY_CAP, refits: REFITS };
  model.check = heldOut(rows, tokens, drift);
  const board = rows.filter((r) => r.kind === 'pre');
  for (const r of board) {
    Object.assign(r, impliedValue(model, r));
    r.value = Math.max(1, Math.round(r.implied)); // board.json's implied_value_usd
    [r.p25, r.p75] = model.check.band.map((e) => r.implied * 10 ** e);
  }
  board.sort((p, q) => q.value - p.value || (p.slug < q.slug ? -1 : 1));
  board.forEach((r, i) => { r.rank = i + 1; });
  rankRanges(board, tokens, rows.filter((r) => r.alone), drift);
  return { rows, board, tokens, model, traded: rows.filter((r) => r.kind === 'traded'), aside: rows.filter((r) => r.kind === 'aside') };
}

if (typeof module === 'object') module.exports = { buildRadar, classify, fitCurve, sizeCurve, growthCarry, fitOdds, tokenShare, launchOdds, impliedValue, heldOut, median, quantile, familyOf };
