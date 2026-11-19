// Dev tool: what is left of the entry quota. Reads the stored key, never prints it.
import { readFileSync } from 'node:fs';
import { homedir } from 'node:os';
const creds = JSON.parse(readFileSync(`${homedir()}/.makefun/credentials`, 'utf8'));
const key = Object.values(creds.keys)[0].key;
const r = await fetch('https://make.fun/api/competitions/token-radar-01/quota', { headers: { 'x-api-key': key, accept: 'application/json' } });
console.log(r.status, JSON.stringify(await r.json().catch(() => ({}))).slice(0, 1500));
