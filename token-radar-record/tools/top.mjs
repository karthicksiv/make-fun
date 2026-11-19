// Dev tool: the dev leaderboard's top rows with their public fields, and their cover images saved for study.
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { homedir } from 'node:os';
const creds = JSON.parse(readFileSync(`${homedir()}/.makefun/credentials`, 'utf8'));
const key = Object.values(creds.keys)[0].key, out = process.argv[2];
mkdirSync(out, { recursive: true });
const H = { 'x-api-key': key, accept: 'application/json' };
const r = await fetch('https://make.fun/api/competitions/token-radar-01/leaderboard?phase=dev', { headers: H });
const body = await r.json(); const rows = ((body.data ?? body).entries ?? []).filter((x) => x.values?.judge != null).sort((a, b) => b.values.judge - a.values.judge);
console.log('fields:', Object.keys(rows[0]).join(','));
let i = 0;
for (const x of rows.slice(0, 8)) {
  i++;
  console.log(i, (x.values.judge / 1e4).toFixed(2) + '%', '@' + x.handle, '|', x.title, '|', x.repo_url ?? '', '|', x.submission_id, '| size', x.values?.size ?? x.values?.artifact_bytes ?? '');
  const url = x.cover_url ?? x.cover ?? x.preview_url ?? x.artifact?.preview_url;
  if (url) { const img = await fetch(url.startsWith('http') ? url : 'https://make.fun' + url, { headers: H }); const buf = Buffer.from(await img.arrayBuffer()); writeFileSync(`${out}/now${i}.${(img.headers.get('content-type') || '').includes('png') ? 'png' : (img.headers.get('content-type') || '').includes('webp') ? 'webp' : 'jpg'}`, buf); console.log('   cover', img.status, img.headers.get('content-type'), buf.length); }
}
