// Dev tool: where a dev rate would sit on make.fun's dev leaderboard. Reads the stored API key, never prints it.
import { readFileSync } from 'node:fs';
import { homedir } from 'node:os';
const creds = JSON.parse(readFileSync(`${homedir()}/.makefun/credentials`, 'utf8'));
const key = Object.values(creds.keys)[0].key, ID = '01a1edde-b4c8-76e5-92e2-26c3ec29837b', MINE = 959650;
const r = await fetch('https://make.fun/api/competitions/token-radar-01/leaderboard?phase=dev', { headers: { 'x-api-key': key, accept: 'application/json' } });
const board = (await r.json()); const rows = (board.data ?? board).entries;
const rated = rows.filter((x) => x.values?.judge != null && x.submission_id !== ID).sort((a, b) => b.values.judge - a.values.judge);
const me = rows.find((x) => x.submission_id === ID);
const above = rated.filter((x) => x.values.judge > MINE).length, tied = rated.filter((x) => x.values.judge === MINE).length;
const pct = (v) => (v / 1e4).toFixed(2) + '%';
console.log('on the leaderboard yet:', me ? `yes, rank ${me.rank}, prize_rank ${me.prize_rank}, eligible ${me.prize_eligible}, reasons ${JSON.stringify(me.reasons)}` : 'no');
console.log(`rated entries besides this one: ${rated.length}; with a higher rate: ${above}; with the same rate: ${tied}`);
console.log('distinct entrants above:', new Set(rated.filter((x) => x.values.judge > MINE).map((x) => x.handle)).size);
for (const x of rated.slice(0, 14)) console.log(`  ${pct(x.values.judge)}  low ${pct(x.values.judge_low)}  @${x.handle}  ${String(x.title).slice(0, 60)}`);
const lows = rated.map((x) => x.values.judge_low).sort((a, b) => b - a);
console.log('10th best interval low (money-place line):', pct(lows[9]), '| 40th best low (final screen line):', pct(lows[39]), '| this entry: rate', pct(MINE), 'low 94.68% high 97.25%');
