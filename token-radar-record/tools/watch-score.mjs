// Dev tool: poll one submission until its dev judge record settles. Reads the stored API key, never prints it.
import { readFileSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
const creds = JSON.parse(readFileSync(`${homedir()}/.makefun/credentials`, 'utf8'));
const key = Object.values(creds.keys)[0].key, ID = process.argv[2], OUT = process.argv[3];
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let last = '', stable = 0;
for (let i = 0; i < 45; i++) {
  const r = await fetch(`https://make.fun/api/submissions/${ID}`, { headers: { 'x-api-key': key, accept: 'application/json' } });
  const body = await r.json().catch(() => ({})); const d = body.data ?? body; const s = (d.scores ?? [])[0]; const j = s?.performance?.judge;
  const line = `status ${d.status} | valid ${s?.performance?.scores?.valid ?? '-'} | matches ${j?.matches ?? 0} | rate ${j?.win_rate_micro ?? '-'} | low ${j?.interval_low_micro ?? '-'} high ${j?.interval_high_micro ?? '-'} | W${j?.wins ?? 0} L${j?.losses ?? 0}`;
  if (line !== last) { console.log(`[${new Date().toISOString().slice(11, 19)}] ${line}`); last = line; stable = 0; } else stable++;
  if (d.error) { console.log('error:', JSON.stringify(d.error).slice(0, 400)); break; }
  if (j?.matches >= 20 || (j?.matches >= 12 && stable >= 6)) { writeFileSync(OUT, JSON.stringify(JSON.parse(JSON.stringify(d, (k, v) => (/claim_code|^key$/.test(k) ? undefined : v))), null, 1)); console.log('settled'); process.exit(0); }
  await sleep(30000);
}
console.log('not settled after the wait'); process.exit(3);
