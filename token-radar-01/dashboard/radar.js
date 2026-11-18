// radar.js: the page. It prices the snapshot with model.js, plans each farm with plan.js, and draws
// the board (sortable, filterable), a detail view for every protocol and a page on the model.
// Nothing here holds a value: every figure drawn is computed in the browser from data.js's snapshot.
const DATA = window.RADAR_DATA, app = document.getElementById('app');
const radar = DATA && buildRadar(DATA.snapshot), M = radar && radar.model, CHECK = M && M.check;
const state = { tab: 'pre', scope: 'all', verdict: 'any', chain: '', q: '', sort: 'rank', dir: 1, deposit: DEFAULTS.deposit, share: DEFAULTS.share, open: '' };
const INITIAL = { ...state };
let planned = '';

const esc = (s) => String(s).replace(/[&<>']/g, (c) => `&#${c.charCodeAt(0)};`);
const pct = (v, digits = 0) => (v * 100).toFixed(digits) + '%';
const signed = (v) => (v >= 0 ? '+' : '−') + usd(Math.abs(v));
const cash = (v) => usd(v).replace('.00', '');
const span = (days) => (days === null ? 'undated' : days < 365 ? Math.round(days) + ' d' : (days / 365).toFixed(1) + ' y');
const scopeOf = (r) => (r.watch ? 'watch' : r.tvl >= DATA.snapshot.floor_usd ? 'off' : 'early');
const SCOPES = { watch: 'Watchlist', off: 'Off-list', early: 'Early' };

/** Plan every board row at the chosen deposit and airdrop share. */
function replan() {
  for (const r of radar.board) r.plan = farmPlan(r, state.deposit, state.share, M.drift);
  planned = state.deposit + '/' + state.share;
}

/** The page against board.json: every row re-priced and re-planned here must equal the row main.ts wrote. */
function selfCheck() {
  const same = radar.board.filter((r, i) => {
    const b = DATA.board[i], steps = planLines(r, farmPlan(r, DEFAULTS.deposit, DEFAULTS.share, M.drift), DEFAULTS.deposit);
    return b && b.slug === r.slug && b.rank === r.rank && b.value === r.value && b.steps.join('\n') === steps.join('\n');
  }).length;
  return { same, ok: same === DATA.board.length && same === radar.board.length };
}

// ---------------------------------------------------------------- state, filters, sorting

function readHash() {
  Object.assign(state, INITIAL);
  for (const [k, v] of new URLSearchParams(location.hash.slice(1))) if (k in state) state[k] = typeof INITIAL[k] === 'number' ? +v : v;
}
function setState(patch) {
  Object.assign(state, patch);
  const changed = Object.keys(state).filter((k) => state[k] !== INITIAL[k]).map((k) => k + '=' + encodeURIComponent(state[k]));
  history.replaceState(null, '', '#' + changed.join('&'));
  render();
}
function filterRows() {
  const q = state.q.trim().toLowerCase(), text = (r) => (r.name + ' ' + r.cat + ' ' + r.chains.join(' ') + ' ' + (r.symbol || '')).toLowerCase();
  if (state.tab !== 'pre') return radar[state.tab].filter((r) => !q || text(r).includes(q));
  return radar.board.filter((r) =>
    (state.scope === 'all' || scopeOf(r) === state.scope) && (state.verdict === 'any' || r.plan.verdict === state.verdict) &&
    (!state.chain || r.chains.includes(state.chain)) && (!q || text(r).includes(q)));
}
function sortRows(rows, cols) {
  const col = cols.find((c) => c.key === state.sort) || cols[0];
  return rows.sort((p, q) => { const a = col.val(p), b = col.val(q); return (a < b ? -1 : a > b ? 1 : 0) * state.dir || p.tvl - q.tvl; });
}

// ---------------------------------------------------------------- the board

const LO = radar && Math.log10(Math.min(...radar.board.map((r) => r.p25))), HI = radar && Math.log10(Math.max(...radar.board.map((r) => r.p75)));
const axis = (v) => ((Math.log10(v) - LO) / (HI - LO)) * 100;
const chainsOf = (r) => esc(r.chains[0] || 'own chain') + (r.chains.length > 1 ? ` +${r.chains.length - 1}` : '');
const nameCell = (r) => `<button class='name' data-open='${r.slug}'>${esc(r.name)}</button>${r.kind === 'pre' ? `<i class='${scopeOf(r)}'>${SCOPES[scopeOf(r)].toLowerCase()}</i>` : ''}<small>${esc(r.cat)} · ${chainsOf(r)}</small>`;
// a week more than 10 points away from the market's median week is the only one coloured
const changeCell = (r) => (r.chg === null ? '–' : `<span class='${r.chg - M.drift > 10 ? 'up' : r.chg - M.drift < -10 ? 'down' : ''}'>${r.chg > 0 ? '+' : ''}${Math.abs(r.chg) > 999 ? Math.round(r.chg / 100) + '×' : r.chg.toFixed(1) + '%'}</span>`);
// the implied value, and its P25-P75 band on one log axis shared by every row
const valueCell = (r) => `<b>${usd(r.implied)}</b><span class='band'><i style='left:${axis(r.p25)}%;width:${axis(r.p75) - axis(r.p25)}%'></i><b style='left:${axis(r.implied)}%'></b></span>`;
const farmCell = (r) => `<b class='${r.plan.verdict}'>${r.plan.verdict}</b> ${signed(r.plan.net)}<small>${r.plan.play} on ${esc(r.plan.chain)} · ${r.plan.hold} d</small>`;

const BOARD_COLUMNS = [
  { key: 'rank', label: '#', val: (r) => r.rank, cell: (r) => r.rank },
  { key: 'name', label: 'Protocol', val: (r) => r.name.toLowerCase(), cell: nameCell },
  { key: 'tvl', label: 'TVL', cls: 'num wide', val: (r) => r.tvl, cell: (r) => usd(r.tvl) },
  { key: 'chg', label: '7-day', cls: 'num wide', tip: 'Seven-day change in TVL', val: (r) => r.chg, cell: changeCell },
  { key: 'age', label: 'No token for', cls: 'num wide', tip: 'How long DefiLlama has tracked it with no token recorded', val: (r) => r.aged, cell: (r) => span(r.age) },
  { key: 'ifLaunched', label: 'If launched', cls: 'num wide', tip: 'sizeCurve on its carried TVL', val: (r) => r.ifLaunched, cell: (r) => usd(r.ifLaunched) },
  { key: 'odds', label: 'Odds', cls: 'num wide', tip: 'launchOdds: a look-alike records a token within two years', val: (r) => r.odds, cell: (r) => pct(r.odds) },
  { key: 'implied', label: 'Implied value', cls: 'val', tip: 'If launched × odds, with its P25–P75 band', val: (r) => r.implied, cell: valueCell },
  { key: 'net', label: 'Farm plan, net', tip: 'Expected airdrop slice less fees and T-bill yield given up', val: (r) => r.plan.net, cell: farmCell },
];

/** A sortable table: a click on a heading sorts by it, a click on a board row opens its detail view. */
function table(cols, rows) {
  const head = cols.map((c) => `<th class='${c.cls || ''}${state.sort === c.key ? (state.dir > 0 ? ' sorted up' : ' sorted') : ''}' data-sort='${c.key}' title='${c.tip || 'Sort'}'>${c.label}</th>`).join('');
  const body = rows.map((r) => `<tr ${r.kind === 'pre' ? `data-open='${r.slug}'` : ''}>${cols.map((c) => `<td class='${c.cls || ''}'>${c.cell(r)}</td>`).join('')}</tr>`).join('');
  const empty = `<tr><td class='empty' colspan='${cols.length}'>No protocol matches these filters. <button class='link' data-reset>Clear them</button></td></tr>`;
  return `<table><thead><tr>${head}</tr></thead><tbody>${body || empty}</tbody></table>`;
}

/** A segmented control: one button per option, the chosen one lit; a click sets state[key]. */
const seg = (key, options) => `<span class='seg'>${options.map(([v, label, n]) => `<button class='${String(state[key]) === String(v) ? 'on' : ''}' data-set='${key}' data-v='${v}'>${label}${n === undefined ? '' : `<small>${n}</small>`}</button>`).join('')}</span>`;
const sizeSeg = () => seg('deposit', [[1000, '$1K'], [5000, '$5K'], [25000, '$25K']]);

function toolbar() {
  const b = radar.board, search = `<input type='search' data-q value='${esc(state.q)}' placeholder='Search' aria-label='Search name, category or chain'>`;
  if (state.tab !== 'pre') return `<nav class='tools'>${search}</nav>`;
  const chains = [...b.flatMap((r) => r.chains).reduce((m, c) => m.set(c, (m.get(c) || 0) + 1), new Map())].sort((p, q) => q[1] - p[1]);
  return `<nav class='tools'>${search}
    ${seg('scope', [['all', 'All'], ...Object.entries(SCOPES)])}
    ${seg('verdict', [['any', 'Any verdict'], ...['Farm', 'Thin', 'Skip'].map((k) => [k, k, b.filter((r) => r.plan.verdict === k).length])])}
    <select data-pick='chain' aria-label='Chain'><option value=''>All chains</option>${chains.map(([c]) => `<option${state.chain === c ? ' selected' : ''}>${esc(c)}</option>`).join('')}</select>
    <label>Deposit${sizeSeg()}</label></nav>`;
}

/** The table for the open tab. A filtered board also says what farming everything shown would net. */
function renderRows() {
  const cols = { pre: BOARD_COLUMNS, traded: TRADED_COLUMNS, aside: ASIDE_COLUMNS }[state.tab], rows = sortRows(filterRows(), cols);
  const all = radar[state.tab === 'pre' ? 'board' : state.tab].length, sum = (key) => rows.reduce((n, r) => n + r.plan[key], 0);
  const basket = state.tab === 'pre' && rows.length > 0 && rows.length < all ? ` · farming all ${rows.length} at ${cash(state.deposit)} each nets <b>${signed(sum('net'))}</b> for ${Math.round(sum('minutes') / 60)} hours of work` : '';
  const note = state.tab === 'traded' ? 'A token already trades for these, so they are never ranked. Those with a market cap that stand alone are what the size curve is fitted on.'
    : state.tab === 'aside' ? 'These record no token but are not a launch of their own, so they are never ranked.'
    : `Farm: the expected slice clears the plan's fees and the ${pct(HURDLE)} T-bill yield its deposit gives up. Thin: it clears the fees only. An implied value is this radar's estimate, not a forecast that a token launches.`;
  document.getElementById('rows').innerHTML = `<p class='count'>${rows.length === all ? all : `${rows.length} of ${all}`} protocols${basket}</p>${table(cols, rows)}<p class='note'>${note}</p>`;
}

// ---------------------------------------------------------------- the detail view

/** One protocol: how its value is built, its farm plan step by step, the plan's ledger and verdict. */
function detail(r) {
  const p = r.plan, cost = p.fees + p.forgone, box = (label, big, note, cls = '') => `<li class='${cls}'><small>${label}</small><b>${big}</b><em>${note}</em></li>`;
  const list = sortRows(filterRows(), BOARD_COLUMNS), at = list.indexOf(r), step = (to, arrow) => (list[to] && at >= 0 ? `<button class='ghost' data-open='${list[to].slug}' title='${esc(list[to].name)}'>${arrow}</button>` : '');
  const why = p.verdict === 'Farm' ? `The expected slice repays every cost ${(p.ev / cost).toFixed(1)}×.`
    : p.verdict === 'Thin' ? `It clears its ${usd(p.fees)} of fees, but not the ${usd(p.forgone)} a T-bill would have paid.` : `It expects ${usd(p.ev)} against ${usd(cost)} of costs.`;
  const line = (label, v, cls = '') => `<tr class='${cls}'><td>${label}</td><td>${v}</td></tr>`;
  return `<div class='shade' data-close></div><aside class='detail' role='dialog' aria-label='${esc(r.name)}'>
    <header><button class='ghost' data-close>Back to board</button><span>#${r.rank} of ${radar.board.length} · ${SCOPES[scopeOf(r)].toLowerCase()}</span>${step(at - 1, 'Previous')}${step(at + 1, 'Next')}</header>
    <h2>${esc(r.name)}</h2><p class='sub'>${esc(r.cat)} · ${esc(r.chains.slice(0, 4).join(', '))}${r.chains.length > 4 ? ` +${r.chains.length - 4}` : ''}</p>
    <h3>Why ${usd(r.implied)}</h3>
    <ol class='ladder'>
      ${box('TVL', usd(r.tvl), r.chg === null ? 'no 7-day change' : `${r.chg > 0 ? '+' : ''}${r.chg.toFixed(1)}% in 7 days, market ${M.drift.toFixed(1)}%`)}
      ${box('× 7-day carry', '×' + r.carry.toFixed(2), 'growthCarry: one more such week')}
      ${box('→ if launched', usd(r.ifLaunched), 'sizeCurve, from traded tokens')}
      ${box('× launch odds', pct(r.odds), `launchOdds: ${span(r.age)} with no token`)}
      ${box('= implied value', usd(r.implied), `P25–P75 ${usd(r.p25)} to ${usd(r.p75)}`, 'total')}
    </ol>
    <h3>Farm plan <small>${p.play} on ${esc(p.chain)}, ${p.hold} days</small>${sizeSeg()}</h3>
    <p class='call'><b class='${p.verdict}'>${p.verdict}.</b> ${why} Net ${signed(p.net)} on ${cash(state.deposit)}, ${signed(p.perHour)} per hour of work.</p>
    <table class='steps'><thead><tr><th></th><th>Step</th><th>When</th><th>Cost</th><th>Time</th></tr></thead><tbody>
      ${p.steps.map((s, i) => `<tr><td>${i + 1}</td><td>${esc(s.text)}</td><td>${s.day}</td><td>${usd(s.cost)}</td><td>${s.min} min</td></tr>`).join('')}
      <tr class='sum'><td></td><td>${p.steps.length} steps on ${cash(state.deposit)}</td><td>${p.hold} days</td><td>${usd(p.fees)}</td><td>${p.minutes} min</td></tr></tbody></table>
    <div class='econ'><table>
      ${line(`Your slice if it launches <small>${pct(state.share)} of its value to farmers</small>`, usd(p.slice))}
      ${line(`× ${pct(r.odds)} launch odds`, usd(p.ev))}
      ${line('− fees', usd(p.fees))}
      ${line(`− T-bill yield given up <small>${pct(HURDLE)} a year, ${p.hold} days</small>`, usd(p.forgone))}
      ${line('Net', signed(p.net), 'sum')}</table>
      <p>Breaks even at ${p.needOdds > 1 ? 'no' : pct(p.needOdds)} launch odds; the model says ${pct(r.odds)}. ${p.payFrom ? `Pays from a deposit of ${usd(p.payFrom)}.` : 'No deposit size pays.'} The hold is ${p.solved < HOLD[0] ? 'the 30-day minimum: no day here out-earns a T-bill' : p.solved > HOLD[1] ? 'capped at 180 days' : 'where one more day stops out-earning a T-bill'}.</p></div>
    <h3>Where it sits</h3>
    <div class='charts'><div>${scatter(r)}</div><div>${oddsChart(r)}</div></div></aside>`;
}

// ---------------------------------------------------------------- the header, the summary and the model page

function topBar() {
  const seal = selfCheck(), s = DATA.snapshot, tab = (v, label, n) => `<button class='${state.tab === v ? 'on' : ''}' data-set='tab' data-v='${v}'>${label}${n ? `<small>${n}</small>` : ''}</button>`;
  return `<header class='top'><h1><svg viewBox='0 0 20 20'><circle cx='10' cy='10' r='8.5'/><circle cx='10' cy='10' r='3.5'/><path d='M10 10 L16 4'/></svg>Launch Radar</h1>
    <nav>${tab('pre', 'No token yet', radar.board.length)}${tab('traded', 'Already trade', radar.traded.length)}${tab('aside', 'Set aside', radar.aside.length)}${tab('model', 'Model')}</nav>
    <p class='meta'>DefiLlama snapshot ${s.as_of}<span class='seal ${seal.ok ? 'ok' : 'bad'}'>${seal.same}/${DATA.board.length} rows match board.json</span></p></header>`;
}

/** What the board is, in three sentences, and the one line every row is priced by. */
function summary() {
  const b = radar.board, watch = b.filter((r) => r.watch).length, farms = b.filter((r) => r.plan.verdict === 'Farm').length;
  return `<section class='lead'><h2>What each unlaunched token would be worth, and whether farming it pays</h2>
    <p>${b.length} protocols in this snapshot record no token, ${watch === DATA.snapshot.watchlist.length ? 'including all' : 'with'} ${watch} on the house watchlist. Each is priced off the ${CHECK.n} tokens that already trade, a curve that misses an unseen token by a typical ×${CHECK.miss.toFixed(1)} where a flat TVL multiple misses by ×${CHECK.tests[0].miss.toFixed(1)}, then weighed by its odds of launching. ${farms} are worth farming at ${cash(state.deposit)}.</p>
    <p class='formula'><code>implied value = sizeCurve(TVL × growthCarry(7-day change)) × launchOdds(time with no token)</code><button class='link' data-set='tab' data-v='model'>How it is priced</button></p></section>`;
}

/** The model page: the three steps with their fitted figures, both fits drawn, and the held-out record. */
function modelPage() {
  const c = M.curve, o = CHECK.odds;
  const tests = CHECK.tests.map((t) => `<tr class='${t.used ? 'used' : ''}'><td>${t.label}</td><td>×${t.miss.toFixed(2)}</td><td>×${t.split.toFixed(2)}</td></tr>`).join('');
  return `<section class='model'><div>
    <h2>How a row is priced</h2><ol class='prose'>
      <li><b>TVL sets the size.</b> <code>sizeCurve</code>: value if launched = 10<sup>${c.a.toFixed(2)}</sup> × (TVL / $100M)<sup>${c.b.toFixed(2)}</sup>, a robust line through the ${CHECK.n} stand-alone tokens that trade.</li>
      <li><b>Seven-day growth moves the TVL.</b> <code>growthCarry</code>: one more week of its move beyond the market's median week (${M.drift.toFixed(1)}%) is carried into the TVL the curve reads, capped at ±${pct(M.carryCap)}.</li>
      <li><b>Time with no token sets the odds.</b> <code>launchOdds</code>: of look-alikes still tokenless at its age, the share that record a token within ${M.horizon / 365} years, by age and category, fitted on ${o.n} protocols.</li>
    </ol><p>Implied value is step 1 on step 2's TVL, times step 3. No protocol is priced by its name, and every figure is fitted in this browser from the snapshot.</p>
    <h3>Held out</h3><table class='tests'><thead><tr><th>Pricing rule, typical miss</th><th>Each token left out</th><th>${CHECK.fit} oldest price ${CHECK.test} newest</th></tr></thead><tbody>${tests}</tbody></table>
    <p>An extra term is used only if it beats the plain curve on both tests; listing age and chain count do not. Launch odds call ${pct(o.hit)} of left-out protocols right, against a base rate of ${pct(o.base)}.</p>
    <h3>Farm assumptions</h3><p><label>Share of a token's value that goes to farmers ${seg('share', [[0.05, '5%'], [0.1, '10%'], [0.2, '20%']])}</label></p>
    <p>Farmers split it by dollars and days in. A deposit gives up a ${pct(HURDLE)} T-bill yield; gas is $${GAS.Ethereum} a transaction on Ethereum, $${GAS_ELSE.toFixed(2)} on most other chains.</p></div>
    <div><h3>Traded tokens set the curve</h3>${scatter()}<h3>The no-token clock</h3>${oddsChart()}</div></section>`;
}

const tick = (v) => '$' + (v >= 1e9 ? v / 1e9 + 'B' : v / 1e6 + 'M');
/** Market cap on TVL, log-log: traded tokens, the fitted curve with its held-out band, and the board. */
function scatter(focus) {
  const W = 290, H = 124, x = (v) => 34 + ((Math.log10(v) - 6.7) / 3.9) * (W - 40), y = (v) => H - 16 - ((Math.log10(v) - 5) / 5.4) * (H - 22);
  const at = (shift) => [5e6, 4e10].map((t) => `${x(t)},${y(sizeCurve(M.curve, t) * 10 ** shift)}`);
  const dot = (r, v, cls, size) => `<circle class='${cls}' cx='${x(r.tvl).toFixed(1)}' cy='${y(v).toFixed(1)}' r='${size}'/>`;
  const ticks = [1e7, 1e8, 1e9, 1e10].map((t) => `<text x='${x(t)}' y='${H - 4}'>${tick(t)}</text>`).join('') + [1e6, 1e8, 1e10].map((t) => `<text class='left' x='30' y='${y(t) + 3}'>${tick(t)}</text><path class='grid' d='M34 ${y(t)} H${W - 6}'/>`).join('');
  return `<svg class='chart' viewBox='0 0 ${W} ${H}' role='img' aria-label='Market cap against TVL for traded tokens, with the fitted size curve'>${ticks}
    <polygon class='bandfill' points='${[...at(CHECK.band[1]), ...at(CHECK.band[2]).reverse()].join(' ')}'/><polyline class='curve' points='${at(0).join(' ')}'/>
    ${radar.tokens.map((t) => dot(t.r, t.r.mcap, 'token', 2.1)).join('')}${focus ? '' : radar.board.map((r) => dot(r, r.implied, 'pre', 1.5)).join('')}
    ${focus ? `<path class='drop' d='M${x(focus.tvl)} ${y(focus.ifLaunched)} V${y(focus.implied)}'/>${dot(focus, focus.ifLaunched, 'ring', 4)}${dot(focus, focus.implied, 'focus', 4.5)}` : ''}</svg>
    <p class='legend'>${focus ? 'Grey: market cap of traded tokens on their TVL. It sits on the curve, then drops by its odds.' : 'Grey: market cap of traded tokens on their TVL. Blue: the board, at implied value.'}</p>`;
}

/** The share of stand-alone protocols that record a token, by age: the snapshot's bars and the fitted line. */
function oddsChart(focus) {
  const W = 290, H = 124, YEARS = 5.2, BINS = [0, 0.5, 1, 1.5, 2, 3, 4, 9], x = (yrs) => 34 + (Math.min(yrs, YEARS) / YEARS) * (W - 40), y = (s) => H - 16 - s * (H - 26);
  const alone = radar.rows.filter((r) => r.alone), cat = focus ? focus.cat : '';
  const bars = BINS.slice(0, -1).map((from, i) => {
    const bin = alone.filter((r) => r.aged / 365 >= from && r.aged / 365 < BINS[i + 1]), s = bin.filter((r) => r.kind === 'traded').length / bin.length;
    return `<rect class='bar' x='${x(from) + 1}' y='${y(s)}' width='${x(BINS[i + 1]) - x(from) - 2}' height='${y(0) - y(s)}'/>`;
  }).join('');
  const line = Array.from({ length: 64 }, (_, i) => 20 + i * 30).map((d) => `${x(d / 365).toFixed(1)},${y(tokenShare(M.odds, d, cat)).toFixed(1)}`).join(' ');
  const mark = (days, cls) => `<circle class='${cls}' cx='${x(days / 365)}' cy='${y(tokenShare(M.odds, days, cat))}' r='4'/>`;
  return `<svg class='chart' viewBox='0 0 ${W} ${H}' role='img' aria-label='Share of protocols recording a token by age'>
    ${[0, 1, 2, 3, 4, 5].map((t) => `<text x='${x(t)}' y='${H - 4}'>${t} y</text>`).join('')}${[0, 0.5, 1].map((s) => `<text class='left' x='30' y='${y(s) + 3}'>${pct(s)}</text><path class='grid' d='M34 ${y(s)} H${W - 6}'/>`).join('')}
    ${bars}<polyline class='curve' points='${line}'/>${focus ? mark(focus.aged, 'focus') + mark(focus.aged + M.horizon, 'ring') : ''}</svg>
    <p class='legend'>${focus ? `Share of ${esc(cat)} look-alikes with a token: ${pct(tokenShare(M.odds, focus.aged, cat))} at its age, ${pct(tokenShare(M.odds, focus.aged + M.horizon, cat))} two years on.` : 'Share of protocols that record a token, by age. The flatter the line ahead, the lower the odds.'}</p>`;
}

// ---------------------------------------------------------------- the other two groups

const TRADED_COLUMNS = [
  { key: 'name', label: 'Protocol', val: (r) => r.name.toLowerCase(), cell: nameCell },
  { key: 'symbol', label: 'Token', val: (r) => r.symbol || '', cell: (r) => esc(r.symbol || '–') },
  { key: 'tvl', label: 'TVL', cls: 'num', val: (r) => r.tvl, cell: (r) => usd(r.tvl) },
  { key: 'mcap', label: 'Market cap', cls: 'num', val: (r) => r.mcap || 0, cell: (r) => (r.mcap ? usd(r.mcap) : '–') },
  { key: 'curve', label: 'Curve says', cls: 'num wide', val: (r) => r.tvl, cell: (r) => usd(sizeCurve(M.curve, r.tvl)) },
  { key: 'role', label: 'Role', val: (r) => (r.mcap && r.alone ? 0 : 1), cell: (r) => (r.mcap && r.alone ? 'prices the curve' : r.alone ? 'no market cap recorded' : 'kept out: ' + r.why) },
];
const ASIDE_COLUMNS = [TRADED_COLUMNS[0], TRADED_COLUMNS[2], { key: 'why', label: 'Why it is not ranked', val: (r) => r.why, cell: (r) => 'It is ' + r.why }];

function render() {
  if (planned !== state.deposit + '/' + state.share) replan();
  const row = radar.board.find((r) => r.slug === state.open);
  app.innerHTML = topBar() + (state.tab === 'model' ? modelPage() : (state.tab === 'pre' ? summary() : '') + toolbar() + `<section id='rows'></section>`) + (row ? detail(row) : '');
  if (state.tab !== 'model') renderRows();
}

if (radar) {
  app.addEventListener('click', (e) => {
    const t = e.target.closest('[data-set],[data-sort],[data-close],[data-open],[data-reset]');
    if (!t) return;
    const d = t.dataset;
    if ('reset' in d) setState({ scope: 'all', verdict: 'any', chain: '', q: '' });
    else if (d.set) setState({ [d.set]: typeof INITIAL[d.set] === 'number' ? +d.v : d.v, ...(d.set === 'tab' ? { q: '', sort: d.v === 'pre' ? 'rank' : 'tvl', dir: d.v === 'pre' ? 1 : -1 } : {}) });
    else if (d.sort) setState(state.sort === d.sort ? { dir: -state.dir } : { sort: d.sort, dir: d.sort === 'rank' || d.sort === 'name' ? 1 : -1 });
    else setState({ open: d.open || '' });
  });
  app.addEventListener('change', (e) => { if (e.target.dataset.pick) setState({ [e.target.dataset.pick]: e.target.value }); });
  app.addEventListener('input', (e) => { if ('q' in e.target.dataset) { state.q = e.target.value; renderRows(); } });
  addEventListener('keydown', (e) => {
    if (e.key === 'Escape') setState({ open: '' });
    if (e.key === '/' && !/INPUT|SELECT/.test(e.target.tagName) && app.querySelector('[data-q]')) { e.preventDefault(); app.querySelector('[data-q]').focus(); }
  });
  addEventListener('hashchange', () => { readHash(); render(); });
  readHash();
  render();
}
