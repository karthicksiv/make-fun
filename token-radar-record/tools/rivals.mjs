// Dev tool: save the public cover image of each of the dev leaderboard's top entries. Reads the stored key, never prints it.
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { homedir } from 'node:os';
const creds = JSON.parse(readFileSync(`${homedir()}/.makefun/credentials`, 'utf8'));
const key = Object.values(creds.keys)[0].key, out = process.argv[2], N = Number(process.argv[3] ?? 10);
mkdirSync(out, { recursive: true });
const H = { 'x-api-key': key, accept: 'application/json' };
const lb = await (await fetch('https://make.fun/api/competitions/token-radar-01/leaderboard?phase=dev', { headers: H })).json();
const rows = ((lb.data ?? lb).entries ?? []).filter((x) => x.values?.judge != null).sort((a, b) => b.values.judge - a.values.judge);
console.log('rated entries:', rows.length);
let i = 0;
for (const x of rows.slice(0, N)) {
  i++;
  const s = await (await fetch(`https://make.fun/api/submissions/${x.submission_id}`, { headers: H })).json(); const d = s.data ?? s, j = d.scores?.[0]?.judge;
  const url = j?.artifact?.url; let got = '';
  if (url) { const img = await fetch('https://make.fun' + url, { headers: { 'x-api-key': key } }); const buf = Buffer.from(await img.arrayBuffer()); writeFileSync(`${out}/r${String(i).padStart(2, '0')}-${x.handle}.webp`, buf); got = `${img.status} ${buf.length}B`; }
  console.log(i, (x.values.judge / 1e4).toFixed(2) + '%', `[${(x.values.judge_low / 1e4).toFixed(2)}-${(x.values.judge_high / 1e4).toFixed(2)}]`, '@' + x.handle, '|', (x.title || '').slice(0, 110), '| W/L', j?.wins, j?.losses, 'matches', j?.matches, '| bytes', x.values.bytes, '| received', x.received_at?.slice(0, 16), '| cover', got);
}
