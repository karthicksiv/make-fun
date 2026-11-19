// Dev tool: fetch the cover image of the leaderboard's top rows.
import { readFileSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
const creds = JSON.parse(readFileSync(`${homedir()}/.makefun/credentials`, 'utf8'));
const key = Object.values(creds.keys)[0].key, out = process.argv[2];
const H = { 'x-api-key': key, accept: 'application/json' };
const r = await fetch('https://make.fun/api/competitions/token-radar-01/leaderboard?phase=dev', { headers: H });
const body = await r.json(); const rows = ((body.data ?? body).entries ?? []).filter((x) => x.values?.judge != null).sort((a, b) => b.values.judge - a.values.judge);
console.log('image:', JSON.stringify(rows[0].image).slice(0, 300)); console.log('values:', JSON.stringify(rows[0].values).slice(0, 300)); console.log('interval', JSON.stringify(rows[0].interval), 'j', JSON.stringify(rows[0].j ?? null).slice(0, 200));
let i = 0;
for (const x of rows.slice(0, 4)) {
  i++; const im = x.image; const url = typeof im === 'string' ? im : im?.url ?? im?.preview_url ?? im?.src;
  if (!url) continue;
  const img = await fetch(url.startsWith('http') ? url : 'https://make.fun' + url, { headers: { 'x-api-key': key } }); const buf = Buffer.from(await img.arrayBuffer());
  const ext = (img.headers.get('content-type') || '').split('/')[1]?.split(';')[0] || 'bin';
  writeFileSync(`${out}/now${i}.${ext}`, buf); console.log(i, '@' + x.handle, img.status, img.headers.get('content-type'), buf.length);
}
