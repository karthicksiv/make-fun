// Dev tool: this entry's dev judge record on make.fun (owner view). Reads the stored API key, never prints it.
import { readFileSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
const creds = JSON.parse(readFileSync(`${homedir()}/.makefun/credentials`, 'utf8'));
const key = Object.values(creds.keys)[0].key, ID = process.argv[2] ?? '01a1edde-b4c8-76e5-92e2-26c3ec29837b';
const get = async (path) => { const r = await fetch(`https://make.fun${path}`, { headers: { 'x-api-key': key, accept: 'application/json' } }); const t = await r.text(); try { return { status: r.status, body: JSON.parse(t) }; } catch { return { status: r.status, body: t.slice(0, 300) }; } };
const sub = await get(`/api/submissions/${ID}`);
const strip = (o) => JSON.parse(JSON.stringify(o, (k, v) => (/claim_code|^key$|api_key/.test(k) ? undefined : v)));
writeFileSync(process.argv[3] ?? '/dev/null', JSON.stringify(strip(sub.body), null, 1));
const d = sub.body.data ?? sub.body, score = (d.scores ?? [])[0] ?? {};
const shape = (o, n = 0) => (Array.isArray(o) ? [`${o.length} items`, ...(o.length && n < 3 ? [shape(o[0], n + 1)] : [])] : o && typeof o === 'object' ? Object.fromEntries(Object.entries(o).map(([k, v]) => [k, n < 3 ? shape(v, n + 1) : typeof v])) : typeof o === 'string' ? o.slice(0, 50) : o);
console.log('top-level keys:', Object.keys(d).join(', '));
console.log('score keys:', Object.keys(score).join(', '));
console.log('judge shape:', JSON.stringify(shape(score.judge)).slice(0, 1800));
for (const path of [`/api/submissions/${ID}/games`, `/api/submissions/${ID}/judge`, `/api/submissions/${ID}/verdicts`, `/api/runs/${score.run_id}`, `/api/runs/${score.run_id}/games`, `/api/submissions/${ID}/runs`]) { const r = await get(path); console.log(path, '->', r.status, typeof r.body === 'object' ? Object.keys(r.body.data ?? r.body).slice(0, 12).join(',') : ''); }
const lb = await get('/api/competitions/token-radar-01/leaderboard?phase=dev'); const rows = (lb.body.data ?? lb.body).entries; const me = rows.find((x) => x.submission_id === ID);
console.log('on the public leaderboard:', me ? `yes: rank ${me.rank}, prize_rank ${me.prize_rank}, rate ${me.values.judge}, eligible ${me.prize_eligible}, reasons ${JSON.stringify(me.reasons)}` : 'not yet');
