// Dev tool: poll the dev leaderboard until our newest rate is the one listed, then print the top rows.
import { readFileSync } from 'node:fs';
import { homedir } from 'node:os';
const creds = JSON.parse(readFileSync(`${homedir()}/.makefun/credentials`, 'utf8'));
const key = Object.values(creds.keys)[0].key, WANT = Number(process.argv[2]);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
for (let i = 0; i < 30; i++) {
  const r = await fetch('https://make.fun/api/competitions/token-radar-01/leaderboard?phase=dev', { headers: { 'x-api-key': key, accept: 'application/json' } });
  const body = await r.json().catch(() => ({})); const rows = ((body.data ?? body).entries ?? []).filter((x) => x.values?.judge != null).sort((a, b) => b.values.judge - a.values.judge);
  const me = rows.find((x) => x.handle === process.env.MAKEFUN_HANDLE);
  if (me && me.values.judge === WANT) {
    console.log(`listed after ${i * 2} min: rank ${me.rank}, prize_rank ${me.prize_rank}, rate ${(me.values.judge / 1e4).toFixed(2)}%`);
    for (const x of rows.slice(0, 6)) console.log(`  ${x.rank}  ${(x.values.judge / 1e4).toFixed(2)}%  @${x.handle}`);
    process.exit(0);
  }
  await sleep(120000);
}
console.log('not listed at the new rate after 60 min'); process.exit(3);
