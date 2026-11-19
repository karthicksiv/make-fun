// Dev tool: find this entry on make.fun's dev leaderboard. Reads the stored API key, never prints it.
import { readFileSync } from 'node:fs';
import { homedir } from 'node:os';
const creds = JSON.parse(readFileSync(`${homedir()}/.makefun/credentials`, 'utf8'));
const key = Object.values(creds.keys)[0].key, ID = '01a1edde-b4c8-76e5-92e2-26c3ec29837b';
const get = async (path) => { const r = await fetch(`https://make.fun${path}`, { headers: { 'x-api-key': key, accept: 'application/json' } }); return { status: r.status, body: await r.json().catch(() => null) }; };
const first = await get('/api/competitions/token-radar-01/leaderboard?phase=dev');
const d = first.body?.data ?? first.body;
console.log('status', first.status, 'keys', d && Object.keys(d).slice(0, 20));
const rows = d?.rows ?? d?.entries ?? [];
console.log('rows on page', rows.length, 'total', d?.total, 'next_cursor', d?.next_cursor ? 'yes' : 'no', 'columns', JSON.stringify(d?.columns ?? d?.column_titles ?? null)?.slice(0, 400));
if (rows[0]) console.log('row shape', JSON.stringify(rows[0]).slice(0, 700));
for (const k of ['me', 'mine', 'own', 'viewer', 'you']) if (d?.[k]) console.log(k, JSON.stringify(d[k]).slice(0, 900));
let mine = rows.find((r) => JSON.stringify(r).includes(ID)), cursor = d?.next_cursor, page = 1;
while (!mine && cursor && page < 40) {
  const r = await get(`/api/competitions/token-radar-01/leaderboard?phase=dev&cursor=${encodeURIComponent(cursor)}`); const dd = r.body?.data ?? r.body;
  mine = (dd?.rows ?? []).find((x) => JSON.stringify(x).includes(ID)); cursor = dd?.next_cursor; page++;
}
console.log('pages read', page, mine ? 'MINE: ' + JSON.stringify(mine).slice(0, 1500) : 'not found in the pages read');
