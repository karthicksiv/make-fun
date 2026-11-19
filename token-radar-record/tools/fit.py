import json, sys, math, datetime, collections
import numpy as np
snap = json.load(open(sys.argv[1]))
P = snap['protocols']; W = set(snap['watchlist'])
asof = datetime.date.fromisoformat(snap['as_of'])
for p in P:
    p['age'] = (asof - datetime.date.fromisoformat(p['listed_at'])).days if p['listed_at'] else None
    p['tok'] = bool(p['gecko_id'] or p['symbol'])
DERIV = {'Bridge','Canonical Bridge','Liquid Staking','Liquid Restaking','Restaked BTC','CEX'}
cal = [p for p in P if p['mcap_usd'] and p['mcap_usd'] > 0]
print('cal all', len(cal), 'non-deriv', len([p for p in cal if p['category'] not in DERIV]))
for p in sorted(cal, key=lambda p: p['mcap_usd']/p['tvl_usd'])[:8] + sorted(cal, key=lambda p: -p['mcap_usd']/p['tvl_usd'])[:10]:
    print('  %-30s %-20s tvl %8.1fM mcap %10.2fM ratio %8.3f age %s sym %s' % (p['name'][:30], p['category'][:20], p['tvl_usd']/1e6, p['mcap_usd']/1e6, p['mcap_usd']/p['tvl_usd'], p['age'], p['symbol']))
def ols(X, y, w=None):
    X = np.asarray(X, float); y = np.asarray(y, float)
    if w is None: w = np.ones(len(y))
    sw = np.sqrt(w)[:, None]
    beta, *_ = np.linalg.lstsq(X * sw, y * sw[:, 0], rcond=None)
    return beta
def huber(X, y, k=1.345, it=30):
    X = np.asarray(X, float); y = np.asarray(y, float)
    b = ols(X, y)
    for _ in range(it):
        r = y - X @ b
        s = np.median(np.abs(r - np.median(r))) / 0.6745 or 1
        w = np.minimum(1, k * s / np.maximum(np.abs(r), 1e-12))
        b = ols(X, y, w)
    return b
for name, grp in (('all', cal), ('nonderiv', [p for p in cal if p['category'] not in DERIV])):
    x = np.array([math.log10(p['tvl_usd']) for p in grp]); y = np.array([math.log10(p['mcap_usd']) for p in grp])
    X = np.c_[np.ones(len(x)), x - 8]
    b = ols(X, y); r = y - X @ b
    bh = huber(X, y); rh = y - X @ bh
    print(name, 'n', len(grp), 'OLS a,b', b.round(3), 'resid sd %.3f' % r.std(), 'med|r| %.3f (x%.2f)' % (np.median(abs(r)), 10**np.median(abs(r))), '| Huber', bh.round(3), 'med|r| %.3f' % np.median(abs(rh)))
    flat = np.median(y - x); rf = y - x - flat
    print('   flat multiple x%.3f med|r| %.3f (x%.2f)' % (10**flat, np.median(abs(rf)), 10**np.median(abs(rf))))
    # correlations of residual with age, growth, nchains
    gs = np.array([p['change_7d'] if p['change_7d'] is not None else np.nan for p in grp])
    ag = np.array([p['age'] if p['age'] is not None else np.nan for p in grp], float)
    nc = np.array([len(p['chains']) for p in grp], float)
    for nm, v in (('chg7d', gs), ('log age', np.log10(np.maximum(ag, 1))), ('log nchains', np.log10(nc))):
        m = ~np.isnan(v)
        c = np.corrcoef(v[m], rh[m])[0, 1]
        sl = ols(np.c_[np.ones(m.sum()), v[m]], rh[m])
        print('   resid ~ %-10s n %3d corr %+.3f slope %+.4f' % (nm, m.sum(), c, sl[1]))
    cats = collections.defaultdict(list)
    for p, ri in zip(grp, rh): cats[p['category']].append(ri)
    print('   cat offsets:', ', '.join('%s %+.2f(n%d)' % (k, np.mean(v), len(v)) for k, v in sorted(cats.items(), key=lambda kv: -len(kv[1]))[:16]))
