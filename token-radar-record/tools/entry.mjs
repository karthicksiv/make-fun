// Dev tool: this entry's own records on make.fun (owner view). Reads the stored API key, never prints it.
import { readFileSync } from 'node:fs';
import { homedir } from 'node:os';
const creds = JSON.parse(readFileSync(`${homedir()}/.makefun/credentials`, 'utf8'));
const key = Object.values(creds.keys)[0].key, ID = '01a1edde-b4c8-76e5-92e2-26c3ec29837b';
for (const path of [`/api/submissions/${ID}`, `/api/submissions/${ID}/status`, `/api/competitions/token-radar-01/submissions?mine=1`, '/api/agents/me']) {
  const r = await fetch(`https://make.fun${path}`, { headers: { 'x-api-key': key, accept: 'application/json' } });
  const text = await r.text(); let body; try { body = JSON.parse(text); } catch { body = text.slice(0, 200); }
  const strip = (o) => JSON.parse(JSON.stringify(o, (k, v) => (/claim_code|^key$|api_key|checks/.test(k) ? undefined : v)));
  const flat = []; (function walk(o, p) { if (o && typeof o === 'object') for (const [k, v] of Object.entries(o)) walk(v, p ? `${p}.${k}` : k); else if (o !== null && /score|rate|rank|valid|judge|game|stage|status|artifact|grade|run|interval|place|bytes|title|dev|phase/i.test(p)) flat.push(`${p}=${String(o).slice(0, 120)}`); })(strip(body), '');
  console.log(`\n${path} -> ${r.status}\n  ` + flat.slice(0, 45).join('\n  '));
}
