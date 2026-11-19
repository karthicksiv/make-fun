import json, sys, math, datetime, collections, re
import numpy as np
snap = json.load(open(sys.argv[1])); P = snap['protocols']; W = set(snap['watchlist'])
asof = datetime.date.fromisoformat(snap['as_of'])
DERIVED = {'Bridge','Canonical Bridge','Liquid Staking','Liquid Restaking','Restaked BTC','CEX'}
VER = re.compile(r'\s(V\d+(\.\d+)?|v\d+(\.\d+)?|\d+\.\d+)$'); WORD = re.compile(r'\b(wrapped|staked|staking)\b', re.I)
EXCH = re.compile(r'^(Binance|Coinbase|Bybit|Kraken|Bitget|OKX|Bitfinex|Gemini)')
FAM = [('Lending', r'lend|cdp|collateral'), ('Trading', r'dex|deriv|option|predict|synth|otc|nft|launchpad|liquidity manager'), ('RWA', r'rwa|stable|btc|reserve|payment|card'), ('Staking', r'stak|governance|locker'), ('Vaults', r'curator|allocator|yield|farm|index|basis|cedefi')]
def fam(c):
    for f, rx in FAM:
        if re.search(rx, c or '', re.I): return f
    return 'Infra'
def alone(p): return not (p['category'] in DERIVED or VER.search(p['name']) or WORD.search(p['name']) or EXCH.search(p['name']))
for p in P:
    p['age'] = (asof - datetime.date.fromisoformat(p['listed_at'])).days if p['listed_at'] else None
    p['tok'] = bool(p['gecko_id'] or p['symbol']); p['fam'] = fam(p['category']); p['alone'] = alone(p)
pre = [p for p in P if not p['tok'] and p['alone']]
print('pre (no token, stands alone):', len(pre), 'watch covered', len([p for p in pre if p['slug'] in W]), 'above floor offlist', len([p for p in pre if p['tvl_usd'] >= 5e7 and p['slug'] not in W]), 'below floor', len([p for p in pre if p['tvl_usd'] < 5e7]))
print('watch not in pre:', [s for s in W if s not in {p['slug'] for p in pre}])
print('set aside no-token:', collections.Counter(('derived' if p['category'] in DERIVED else 'versioned' if VER.search(p['name']) else 'word' if WORD.search(p['name']) else 'exchange') for p in P if not p['tok'] and not p['alone']))
print('traded:', len([p for p in P if p['tok']]), 'alone', len([p for p in P if p['tok'] and p['alone']]), 'with mcap', len([p for p in P if p['tok'] and (p['mcap_usd'] or 0) > 0]))
cal = [p for p in P if p['tok'] and (p['mcap_usd'] or 0) > 0 and p['alone']]
print('calibration n', len(cal), collections.Counter(p['fam'] for p in cal), 'pre fam', collections.Counter(p['fam'] for p in pre))
med_g = float(np.median([p['change_7d'] for p in P if p['change_7d'] is not None])); print('median 7d change all', med_g)
def feats(grp, terms, fams):
    X = []
    for p in grp:
        row = [1.0, math.log10(p['tvl_usd']) - 8]
        if 'g' in terms: row.append(max(-25, min(25, (p['change_7d'] if p['change_7d'] is not None else med_g) - med_g)) / 100)
        if 'age' in terms: row.append(math.log10(max(30, p['age'] if p['age'] is not None else 600)) - 2.8)
        if 'nch' in terms: row.append(math.log10(len(p['chains']) or 1))
        if 'fam' in terms: row += [1.0 if p['fam'] == f else 0.0 for f in fams[1:]]
        X.append(row)
    return np.array(X)
def wls(X, y, w, ridge=None):
    A = X.T @ (X * w[:, None]); b = X.T @ (w * y)
    if ridge is not None: A = A + np.diag(ridge)
    return np.linalg.solve(A, b)
def huber(X, y, ridge=None, k=1.345):
    w = np.ones(len(y)); b = wls(X, y, w, ridge)
    for _ in range(25):
        r = y - X @ b; s = np.median(np.abs(r)) / 0.6745 or 1
        w = np.minimum(1, k * s / np.maximum(np.abs(r), 1e-9)); b = wls(X, y, w, ridge)
    return b
fams = sorted({p['fam'] for p in P})
def loo(grp, terms, robust=True, lam=4.0):
    y = np.array([math.log10(p['mcap_usd']) for p in grp]); X = feats(grp, terms, fams)
    ridge = np.zeros(X.shape[1]); 
    if 'fam' in terms: ridge[-(len(fams)-1):] = lam
    errs = []
    for i in range(len(grp)):
        m = np.arange(len(grp)) != i
        b = huber(X[m], y[m], ridge) if robust else wls(X[m], y[m], np.ones(m.sum()), ridge)
        errs.append(y[i] - X[i] @ b)
    return np.array(errs)
def flat_loo(grp):
    r = np.array([math.log10(p['mcap_usd'] / p['tvl_usd']) for p in grp]); return np.array([r[i] - np.median(np.delete(r, i)) for i in range(len(r))])
def rep(name, e): print('  %-34s med miss x%.2f  mean|e| %.3f  within x3 %.0f%%  rmse %.3f' % (name, 10**np.median(abs(e)), abs(e).mean(), 100*(abs(e) < math.log10(3)).mean(), (e**2).mean()**.5))
print('LOO, n', len(cal)); rep('flat multiple', flat_loo(cal)); rep('size OLS', loo(cal, [], False)); rep('size Huber', loo(cal, []))
for t in (['g'], ['age'], ['nch'], ['fam'], ['fam','g'], ['fam','age'], ['fam','g','age'], ['g','age']): rep('+ ' + '+'.join(t), loo(cal, t))
for lam in (1, 2, 4, 8, 16): rep('fam ridge %g' % lam, loo(cal, ['fam'], True, lam))
y = np.array([math.log10(p['mcap_usd']) for p in cal])
for t in ([], ['g'], ['age'], ['fam'], ['fam','g','age']):
    X = feats(cal, t, fams); ridge = np.zeros(X.shape[1])
    if 'fam' in t: ridge[-(len(fams)-1):] = 4
    print('  coef', t, huber(X, y, ridge).round(3))
# time split: older half fits, newer half tests (dated only)
d = sorted([p for p in cal if p['age'] is not None], key=lambda p: -p['age']); h = len(d) // 2; old, new = d[:h], d[h:]
print('time split: fit %d older (>= %dd), test %d newer' % (len(old), old[-1]['age'], len(new)))
yo = np.array([math.log10(p['mcap_usd']) for p in old]); yn = np.array([math.log10(p['mcap_usd']) for p in new])
fm = np.median([math.log10(p['mcap_usd']/p['tvl_usd']) for p in old]); rep('flat', np.array([math.log10(p['mcap_usd']/p['tvl_usd']) - fm for p in new]))
for t in ([], ['g'], ['fam'], ['fam','g']):
    Xo, Xn = feats(old, t, fams), feats(new, t, fams); ridge = np.zeros(Xo.shape[1])
    if 'fam' in t: ridge[-(len(fams)-1):] = 4
    rep('curve ' + '+'.join(t), yn - Xn @ huber(Xo, yo, ridge))
# survival: share with no token by age (stand-alone protocols only)
al = [p for p in P if p['alone'] and p['age'] is not None]
print('stand-alone dated', len(al), 'tok share', np.mean([p['tok'] for p in al]))
edges = [0, 90, 180, 365, 545, 730, 1095, 1460, 2000]
for a, b in zip(edges, edges[1:]):
    g = [p for p in al if a <= p['age'] < b]
    if g: print('  age %4d-%4d n %3d no-token share %.2f' % (a, b, len(g), 1 - np.mean([p['tok'] for p in g])))
und = [p for p in P if p['alone'] and p['age'] is None]; print('  undated n', len(und), 'no-token share %.2f' % (1 - np.mean([p['tok'] for p in und])))
