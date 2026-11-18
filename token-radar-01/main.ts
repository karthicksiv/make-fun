// Launch Radar: make.fun token-radar-01 entry (Node 22, standard library only).
//
// Reads the sealed snapshot (/input/snapshot.json, or --input <file|dir>), prices every protocol that
// records no token with dashboard/model.js, plans its farm with dashboard/plan.js, and writes
//
//   /output/board.json     the ranked board the gate reads
//   /output/data.js        the snapshot and that board, for the page
//   /output/dashboard/     the static page (index.html, model.js, plan.js, radar.js, style.css)
//   /output/screenshots/   the three captures of that page
//   /output/dossier.json   what the judge reads, written by the vendored make.fun library
//
// The model is not in this file on purpose. It lives in dashboard/, the directory the dossier shows,
// and this program imports it from there: the code that writes board.json is the code the page runs
// and the judge reads. The page re-prices the snapshot in the browser and checks itself against the
// board embedded in data.js, row by row.
//
// Outside the grader, MAKEFUN_OUTPUT_DIR stands in for /output:
//   MAKEFUN_OUTPUT_DIR=out node main.ts --input <kit>/input/snapshot.json

import { createHash } from 'node:crypto';
import { copyFileSync, existsSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import model from './dashboard/model.js';
import plan from './dashboard/plan.js';
import { buildDossier } from './vendor/makefun-board.ts';

const here = fileURLToPath(new URL('.', import.meta.url));

/** The gate's limits (the kit's reference/gate.json, as RULES.txt states them). */
const GATE = {
  rows: [100, 4000], steps: [3, 12], step_chars: [20, 600], max_value: 100_000_000_000_000,
  text_max_bytes: 65_536, screenshots: { max: 4, max_bytes: 524_288 }, dashboard: { max_files: 400, max_bytes: 4_194_304 },
};

function fail(message: string): never {
  process.stderr.write(`launch radar: ${message}\n`);
  process.exit(1);
}

/** --name <value> or --name=<value> out of the arguments, or the fallback. */
function arg(name: string, fallback: string): string {
  const at = process.argv.indexOf(`--${name}`);
  if (at >= 0 && at + 1 < process.argv.length) return process.argv[at + 1]!;
  const joined = process.argv.find((a) => a.startsWith(`--${name}=`));
  return joined === undefined ? fallback : joined.slice(name.length + 3);
}

const inputArg = arg('input', '/input');
const inputFile = inputArg.endsWith('.json') ? inputArg : join(inputArg, 'snapshot.json');
if (!existsSync(inputFile)) fail(`no snapshot at ${inputFile}: pass --input <snapshot.json|directory> or mount the sealed /input`);
const out = arg('output', process.env.MAKEFUN_OUTPUT_DIR ?? '/output');

const snapshot = JSON.parse(readFileSync(inputFile, 'utf8'));
if (!Array.isArray(snapshot.protocols) || !Array.isArray(snapshot.watchlist)) fail(`${inputFile} is not a token-radar snapshot`);

// ---------------------------------------------------------------- the board

const radar = model.buildRadar(snapshot);
const { deposit, share } = plan.DEFAULTS;
const whole = (v: number): number => Math.round(v);

const rows = radar.board.map((r: any) => {
  const farm = plan.farmPlan(r, deposit, share, radar.model.drift);
  return {
    rank: r.rank,
    slug: r.slug,
    name: r.name,
    implied_value_usd: r.value,
    value_if_launched_usd: whole(r.ifLaunched),
    launch_odds: Number(r.odds.toFixed(4)),
    p25_usd: whole(r.p25),
    p75_usd: whole(r.p75),
    category: r.cat,
    chains: r.chains,
    tvl_usd: r.tvl,
    change_7d: r.chg,
    days_with_no_token: r.age,
    on_watchlist: r.watch,
    farm: {
      verdict: farm.verdict, play: farm.play, chain: farm.chain, hold_days: farm.hold, minutes: farm.minutes,
      fees_usd: Number(farm.fees.toFixed(2)), tbill_yield_forgone_usd: Number(farm.forgone.toFixed(2)),
      expected_slice_usd: Number(farm.ev.toFixed(2)), net_usd: Number(farm.net.toFixed(2)),
    },
    farm_steps: plan.planLines(r, farm, deposit),
  };
});

/** Why the gate would refuse these rows, or null: main.ts never writes a board the gate refuses. */
function boardIssue(): string | null {
  const known = new Set(snapshot.protocols.map((p: any) => p.slug));
  if (rows.length < GATE.rows[0] || rows.length > GATE.rows[1]) return `${rows.length} rows, outside ${GATE.rows.join(' to ')}`;
  const seen = new Set<string>();
  for (let i = 0; i < rows.length; i++) {
    const r = rows[i];
    if (!known.has(r.slug) || seen.has(r.slug)) return `row ${i + 1}: slug ${r.slug} is not one of the snapshot's, or repeats`;
    seen.add(r.slug);
    if (r.rank !== i + 1) return `row ${i + 1}: rank ${r.rank}`;
    if (!Number.isSafeInteger(r.implied_value_usd) || r.implied_value_usd < 1 || r.implied_value_usd > GATE.max_value) return `${r.slug}: implied value ${r.implied_value_usd}`;
    if (i > 0 && r.implied_value_usd > rows[i - 1].implied_value_usd) return `${r.slug}: implied value rises at rank ${r.rank}`;
    if (typeof r.name !== 'string' || r.name.trim() === '') return `${r.slug}: no name`;
    if (r.farm_steps.length < GATE.steps[0] || r.farm_steps.length > GATE.steps[1]) return `${r.slug}: ${r.farm_steps.length} farm steps`;
    const bad = r.farm_steps.find((s: string) => s.trim().length < GATE.step_chars[0] || s.trim().length > GATE.step_chars[1]);
    if (bad !== undefined) return `${r.slug}: a farm step of ${bad.trim().length} characters`;
  }
  const missing = snapshot.watchlist.filter((slug: string) => !seen.has(slug));
  if (missing.length > 0) return `the watchlist's ${missing.slice(0, 3).join(', ')} ${missing.length === 1 ? 'is' : 'are'} not on the board`;
  if (rows.length > 1 && rows.every((r: any) => r.implied_value_usd === rows[0].implied_value_usd)) return 'every implied value is the same';
  return null;
}
const issue = boardIssue();
if (issue !== null) fail(`the model wrote a board the gate refuses: ${issue}`);

const check = radar.model.check;
const board = {
  schema: 'launch-radar/board/v1',
  as_of: snapshot.as_of,
  source: snapshot.source,
  claim: 'implied_value_usd is this entry\'s own estimate: value if launched x launch odds. It is not a forecast that a token will be issued.',
  model: {
    value: 'sizeCurve(tvl x growthCarry(change_7d)) x launchOdds(days with no token, category)',
    size_curve: { log10_value_at_100m_tvl: Number(radar.model.curve.a.toFixed(4)), slope: Number(radar.model.curve.b.toFixed(4)), fitted_on_traded_tokens: check.n },
    market_median_change_7d: radar.model.drift,
    launch_odds_horizon_days: radar.model.horizon,
    held_out: { typical_miss_x: Number(check.miss.toFixed(3)), flat_multiple_miss_x: Number(check.tests[0].miss.toFixed(3)), launch_odds_hit_rate: Number(check.odds.hit.toFixed(3)), base_rate: Number(check.odds.base.toFixed(3)) },
  },
  farm_plans_at: { deposit_usd: deposit, airdrop_share: share },
  counts: { protocols: radar.rows.length, ranked: rows.length, watchlist: snapshot.watchlist.length, traded: radar.traded.length, set_aside: radar.aside.length },
  rows,
};
const boardBytes = `${JSON.stringify(board)}\n`;

// ---------------------------------------------------------------- the files

mkdirSync(out, { recursive: true });
writeFileSync(join(out, 'board.json'), boardBytes);
// The page gets the snapshot, to re-price it with the same model.js, and the board, to check itself against.
const pageData = { snapshot, board: rows.map((r: any) => ({ slug: r.slug, rank: r.rank, value: r.implied_value_usd, steps: r.farm_steps })), board_sha256: createHash('sha256').update(boardBytes).digest('hex') };
writeFileSync(join(out, 'data.js'), `window.RADAR_DATA = ${JSON.stringify(pageData)};\n`);

/**
 * Replace `to` with a copy of a directory's regular files, and its sub-directories', skipping
 * dot-names; the names copied. Whatever an earlier run left in `to` goes first, so the dossier, which
 * lists every file there, is the same whatever the output directory held before.
 */
function copyTree(from: string, to: string): string[] {
  rmSync(to, { recursive: true, force: true });
  if (!existsSync(from)) return [];
  mkdirSync(to, { recursive: true });
  const copied: string[] = [];
  for (const entry of readdirSync(from, { withFileTypes: true })) {
    if (entry.name.startsWith('.')) continue;
    if (entry.isDirectory()) copied.push(...copyTree(join(from, entry.name), join(to, entry.name)).map((n) => `${entry.name}/${n}`));
    else if (entry.isFile()) {
      copyFileSync(join(from, entry.name), join(to, entry.name));
      copied.push(entry.name);
    }
  }
  return copied.sort();
}

const pageFiles = copyTree(join(here, 'dashboard'), join(out, 'dashboard'));
const shots = copyTree(join(here, 'screenshots'), join(out, 'screenshots'));
if (shots.length < 3) fail(`screenshots/ holds ${shots.length} image(s): hand in 3, the board, a detail view and a filtered view`);

// The dossier is written last: the scorer rebuilds it from this /output and compares bytes.
writeFileSync(join(out, 'dossier.json'), buildDossier(out, { text_max_bytes: GATE.text_max_bytes, screenshots: GATE.screenshots, dashboard: GATE.dashboard }));

const verdicts = rows.reduce((n: Record<string, number>, r: any) => ({ ...n, [r.farm.verdict]: (n[r.farm.verdict] ?? 0) + 1 }), {});
process.stdout.write(
  `launch radar: ${radar.rows.length} protocols read: ${rows.length} ranked (all ${snapshot.watchlist.length} of the watchlist), ${radar.traded.length} already trade, ${radar.aside.length} set aside\n` +
    `launch radar: size curve fitted on ${check.n} traded tokens, typical held-out miss x${check.miss.toFixed(2)} against x${check.tests[0].miss.toFixed(2)} for a flat multiple\n` +
    `launch radar: farm plans at $${deposit}: ${Object.entries(verdicts).map(([k, v]) => `${v} ${k.toLowerCase()}`).join(', ')}\n` +
    `launch radar: wrote board.json, data.js, dashboard/ (${pageFiles.length} files), screenshots/ (${shots.length}) and dossier.json to ${out}\n`,
);
