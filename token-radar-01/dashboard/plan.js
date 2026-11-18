// plan.js: one priced row in, one farm plan out. Every step carries its cost in dollars and its
// minutes; the hold is solved from the row's own figures, not assumed; the verdict nets the expected
// airdrop slice against every fee and against the T-bill yield the deposit gives up. main.ts writes
// these steps into board.json's farm_steps; the page shows the same plan and re-sizes it live.

const HURDLE = 0.04; // yearly T-bill yield a deposit gives up while it farms
const GAS = { Ethereum: 3, Bitcoin: 2.5, Tron: 1.2 }, GAS_ELSE = 0.05; // $ a transaction: an assumed fee table
const WITHDRAWAL = 1; // $ an exchange charges to send funds on-chain
const HOLD = [30, 180]; // days: shorter earns no standing, longer is a review point, not a plan
const RAMP = 0.5; // TVL is taken to have grown in a straight line since listing: earlier farmers hold TVL x age / 2 dollar-days
const DEFAULTS = { deposit: 1000, share: 0.1 }; // board.json's plans: $1,000 in, a tenth of the token's value to farmers

// What farming means for each family of protocol: the entry, its transactions, minutes and fee in bps.
const PLAYS = {
  Lending: { play: 'Lend + borrow', tx: 3, min: 12, bps: 0, upkeep: 'repay and re-borrow a little',
    enter: (n, a) => `Supply it as ${a} on ${n}, borrow 20% against it and supply that too: borrowers are usually weighted above lenders` },
  Trading: { play: 'LP + volume', tx: 3, min: 15, bps: 30, upkeep: 'make one swap or re-range the position',
    enter: (n, a) => `Put half the ${a} in ${n}'s deepest pool and keep half to trade one round-trip a week` },
  'RWA & stables': { play: 'Mint + hold', tx: 2, min: 10, bps: 10, upkeep: 'top up or roll the position',
    enter: (n, a) => `Mint or buy ${n}'s token at par with the ${a} and hold it in your own wallet, never on an exchange` },
  Staking: { play: 'Stake', tx: 2, min: 8, bps: 0, upkeep: 'claim and restake the rewards',
    enter: (n, a) => `Stake the ${a} through ${n} and pick an operator outside its top three` },
  Vaults: { play: 'Vault deposit', tx: 2, min: 8, bps: 0, upkeep: 'add $25 or claim and redeposit',
    enter: (n, a) => `Deposit it in ${n}'s largest ${a} vault: approve, then deposit` },
  Infra: { play: 'Bridge + use', tx: 4, min: 20, bps: 20, upkeep: 'make one transaction in an app',
    enter: (n, a) => `Bridge the ${a} to ${n} and use its two largest apps once each: a swap and a deposit` },
};

function usd(v) {
  const a = Math.abs(v), s = v < 0 ? '-$' : '$';
  if (a >= 1e9) return s + (a / 1e9).toFixed(2) + 'B';
  if (a >= 1e6) return s + (a / 1e6).toFixed(a >= 1e8 ? 0 : 1) + 'M';
  if (a >= 1e4) return s + Math.round(a / 1e3) + 'K';
  return s + (a >= 100 ? String(Math.round(a)).replace(/\B(?=(\d{3})+$)/g, ',') : a.toFixed(2));
}
const gasOn = (chain) => (chain in GAS ? GAS[chain] : GAS_ELSE);
/** The chain to farm on: the cheapest one DefiLlama counts the protocol's TVL on. */
const cheapestChain = (chains) => chains.reduce((best, c) => (gasOn(c) < gasOn(best) ? c : best), chains[0] || 'its own chain');

/**
 * The plan for one row at a deposit size. `share` is the part of the token's value given to farmers,
 * split by dollars and by days in:
 *   your slice if it launches = share x value if launched x deposit / (TVL + deposit) x hold / (earlier + hold)
 * `earlier` is the head start of those already in: the days it has run with no token x RAMP. A late
 * farmer's marginal day is worth odds x full x earlier / (earlier + hold)^2, and the hold ends where
 * that falls to the T-bill's daily yield: the hold is solved, never assumed.
 */
function farmPlan(r, deposit, share, drift) {
  const p = PLAYS[r.family], chain = cheapestChain(r.chains), gas = gasOn(chain), asset = chain === 'Bitcoin' ? 'BTC' : 'USDC';
  const full = share * r.ifLaunched * deposit / (r.tvl + deposit), daily = deposit * HURDLE / 365, earlier = r.aged * RAMP;
  const solved = Math.sqrt(r.odds * full * earlier / daily) - earlier;
  const hold = Math.round(Math.max(HOLD[0], Math.min(HOLD[1], solved))), fortnights = Math.floor(hold / 14), weeks = Math.ceil(hold / 7);
  // entry timing, from the row's own week and age
  const falling = r.chg !== null && r.chg - drift < -10, young = r.age !== null && r.age < 90;
  const timing = falling ? `TVL moved ${r.chg.toFixed(1)}% this week against the market's ${drift.toFixed(1)}%: enter half now, half after a week that beats the market. `
    : young ? `Listed ${Math.round(r.age)} days ago, so enter half now and half on day 30. ` : '';
  const end = solved >= HOLD[1] ? `review. The slice still outgrows T-bills, so extend a month at a time or withdraw`
    : solved < HOLD[0] ? `withdraw and send the funds back. No day here out-earns T-bills, so ${hold} days is only the shortest stay that leaves a record`
    : `withdraw and send the funds back. Past day ${hold} an extra day adds less to the slice than T-bills pay`;
  const steps = [
    { day: 'day 0', min: 10, cost: WITHDRAWAL + gas, text: `Send ${usd(deposit)} of ${asset} from an exchange to a fresh wallet on ${chain}${r.chains.length > 1 ? `, the cheapest of its ${r.chains.length} chains` : ''}, plus $5 of gas` },
    { day: 'day 0', min: p.min, cost: p.tx * gas + deposit * p.bps / 1e4, text: timing + p.enter(r.name, asset) },
    { day: 'fortnightly', min: 3 * fortnights, cost: fortnights * gas, text: `Each fortnight ${p.upkeep}, so the wallet shows ${fortnights} separate active periods, not one deposit` },
    { day: 'weekly', min: 2 * weeks, cost: 0, text: `Check its TVL once a week and leave early if it falls below ${usd(r.tvl * 0.75)}, three quarters of today's ${usd(r.tvl)}` },
    { day: `day ${hold}`, min: 10, cost: 2 * gas + WITHDRAWAL, text: `Day ${hold}: ${end}` },
    { day: 'at launch', min: 5, cost: gas, text: `Keep the wallet and its gas. If ${r.name} opens a points or claim page, claim from this same wallet` },
  ];
  const sum = (key) => steps.reduce((n, s) => n + s[key], 0);
  const fees = sum('cost'), minutes = sum('min'), forgone = daily * hold;
  const slice = full * hold / (earlier + hold), ev = r.odds * slice, net = ev - fees - forgone;
  // per dollar deposited: what the plan earns beyond the hurdle and its proportional fees
  const edge = (ev - forgone) / deposit - p.bps / 1e4, fixed = fees - deposit * p.bps / 1e4;
  return {
    play: p.play, chain, hold, solved, steps, fees, minutes, forgone, slice, ev, net,
    needOdds: (fees + forgone) / slice, // the launch odds at which the plan breaks even
    needShare: share * (fees + forgone) / ev, // the share of the token farmers must get for it to break even
    payFrom: edge > 0 ? fixed / edge : null, // the smallest deposit that nets above zero; null: none does
    perHour: net / (minutes / 60),
    verdict: net > 0 ? 'Farm' : ev > fees ? 'Thin' : 'Skip', // Thin: clears its fees, not the T-bill hurdle
  };
}

/** The plan as board.json's farm_steps: one sentence a step, each with its cost and its minutes. */
function planLines(r, plan, deposit) {
  const lines = plan.steps.map((s, i) => `${i + 1}. ${s.text}. Cost ${usd(s.cost)}, ${s.min} min, ${s.day}.`);
  lines.push(`Sum at ${usd(deposit)} on ${plan.chain}: fees ${usd(plan.fees)}, ${usd(plan.forgone)} of T-bill yield given up over ${plan.hold} days and ${plan.minutes} minutes of work, against an expected slice of ${usd(plan.ev)} (${usd(plan.slice)} if it launches, at ${Math.round(r.odds * 100)}% odds). Net ${usd(plan.net)}: ${plan.verdict.toLowerCase()}.`);
  return lines;
}

if (typeof module === 'object') module.exports = { farmPlan, planLines, cheapestChain, usd, PLAYS, HURDLE, HOLD, RAMP, DEFAULTS };
