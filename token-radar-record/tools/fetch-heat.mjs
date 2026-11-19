// Dev tool: download a submission's full judge record (judge-heat). Reads the stored API key, never prints it.
import { readFileSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
const creds = JSON.parse(readFileSync(`${homedir()}/.makefun/credentials`, 'utf8'));
const key = Object.values(creds.keys)[0].key, [saved, out] = process.argv.slice(2);
const d = JSON.parse(readFileSync(saved, 'utf8')), rec = (d.data ?? d).downloads.find((x) => x.kind === 'judge-record');
const r = await fetch('https://make.fun' + rec.url, { headers: { 'x-api-key': key, accept: 'application/json' } });
const text = await r.text();
writeFileSync(out, text);
console.log(r.status, text.length, 'bytes ->', out);
