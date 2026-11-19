// Dev tool: this entry's row on the dev leaderboard, and where its rate would place among valid entries.
import { readFileSync } from 'node:fs';
import { homedir } from 'node:os';
const creds = JSON.parse(readFileSync(`${homedir()}/.makefun/credentials`, 'utf8'));
const key = Object.values(creds.keys)[0].key;
const r = await fetch('https://make.fun/api/competitions/token-radar-01/leaderboard?phase=dev', { headers: { 'x-api-key': key, accept: 'application/json' } });
const d = (await r.json()); const board = d.data ?? d; const rows = board.entries;
const mine = rows.filter((x) => x.handle === process.env.MAKEFUN_HANDLE || /Launch Radar Terminal/.test(x.title ?? ''));
const ranked = rows.filter((x) => x.ranked && x.values?.judge != null);
console.log('entries', rows.length, '| ranked with a rate', ranked.length, '| statuses', JSON.stringify(Object.entries(rows.reduce((m, x) => ({ ...m, [x.status]: (m[x.status] ?? 0) + 1 }), {})).sort((a, b) => b[1] - a[1]).slice(0, 6)));
for (const m of mine) console.log('MINE', JSON.stringify({ rank: m.rank, prize_rank: m.prize_rank, status: m.status, ranked: m.ranked, compliant: m.compliant, prize_eligible: m.prize_eligible, reasons: m.reasons, values: m.values, title: m.title, received_at: m.received_at }));
if (mine.length === 0) console.log('MINE: no row yet');
const pct = (v) => (v / 1e4).toFixed(2) + '%';
for (const n of [1, 5, 10, 20, 40, 100, 500]) { const x = ranked[n - 1]; if (x) console.log(`place ${n}: rate ${pct(x.values.judge)} low ${pct(x.values.judge_low)} high ${pct(x.values.judge_high)} @${x.handle}`); }
