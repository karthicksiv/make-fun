// Dev tool: what make.fun shows any signed-in account about another entry (public view). Reads the stored key, never prints it.
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { homedir } from 'node:os';
const creds = JSON.parse(readFileSync(`${homedir()}/.makefun/credentials`, 'utf8'));
const key = Object.values(creds.keys)[0].key, [id, out] = process.argv.slice(2);
mkdirSync(out, { recursive: true });
const r = await fetch(`https://make.fun/api/submissions/${id}`, { headers: { 'x-api-key': key, accept: 'application/json' } });
const text = await r.text(); writeFileSync(`${out}/${id}.json`, text);
let d; try { d = JSON.parse(text); d = d.data ?? d; } catch { d = null; }
console.log(r.status, text.length, 'bytes');
if (d) {
  console.log('keys:', Object.keys(d).join(','));
  console.log('title:', d.title, '| repo:', d.repo_url, '| source_withheld:', d.source_withheld, '| access:', JSON.stringify(d.access).slice(0, 300));
  console.log('downloads:', (d.downloads ?? []).map((x) => x.kind + ':' + x.name).join(' | ').slice(0, 600));
  const j = d.scores?.[0]?.judge; console.log('judge keys:', j ? Object.keys(j).join(',') : null, '| heat?', !!j?.heat, '| artifact:', JSON.stringify(j?.artifact ?? null).slice(0, 300));
}
