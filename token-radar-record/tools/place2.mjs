// Dev tool: where our rate sits on make.fun's dev leaderboard, and whether our entries are listed.
import { readFileSync } from 'node:fs';
import { homedir } from 'node:os';
const creds = JSON.parse(readFileSync(`${homedir()}/.makefun/credentials`, 'utf8'));
const key = Object.values(creds.keys)[0].key, MINE = Number(process.argv[2]), IDS = ['01a1edde-b4c8-76e5-92e2-26c3ec29837b', '01a1edf2-4b40-75ab-952b-fc1890440152'];
const r = await fetch('https://make.fun/api/competitions/token-radar-01/leaderboard?phase=dev', { headers: { 'x-api-key': key, accept: 'application/json' } });
const body = await r.json(); const rows = (body.data ?? body).entries ?? [];
const pct = (v) => (v / 1e4).toFixed(2) + '%';
for (const id of IDS) { const me = rows.find((x) => x.submission_id === id); console.log(id.slice(0, 8), me ? `LISTED rank ${me.rank} prize_rank ${me.prize_rank} rate ${pct(me.values.judge)} eligible ${me.prize_eligible} reasons ${JSON.stringify(me.reasons)}` : 'not listed'); }
const mine = rows.filter((x) => x.handle === process.env.MAKEFUN_HANDLE); console.log('rows under my handle:', mine.length, mine.map((x) => `${x.title} ${x.values?.judge}`).join(' | '));
const rated = rows.filter((x) => x.values?.judge != null && !IDS.includes(x.submission_id)).sort((a, b) => b.values.judge - a.values.judge);
const byEntrant = new Map(); for (const x of rated) if (!byEntrant.has(x.handle)) byEntrant.set(x.handle, x);
const best = [...byEntrant.values()];
console.log(`entrants with a higher rate than ${pct(MINE)}: ${best.filter((x) => x.values.judge > MINE).length}; at the same rate: ${best.filter((x) => x.values.judge === MINE).length}`);
for (const x of best.slice(0, 9)) console.log(`  ${pct(x.values.judge)}  @${x.handle}`);
