import json, sys, math, datetime, collections, re
import numpy as np
exec(open(sys.argv[2]).read().split("pre = [p for p in P")[0].replace("snap = json.load(open(sys.argv[1]))", "snap = json.load(open(sys.argv[1]))"))
al = [p for p in P if p['alone']]
AMAX = max(p['age'] for p in al if p['age'] is not None)
for p in al: p['a'] = p['age'] if p['age'] is not None else AMAX
fams = sorted({p['fam'] for p in al})
def X_of(grp, use_fam=True, use_tvl=False):
    rows = []
    for p in grp:
        r = [1.0, math.log(max(p['a'], 14) / 365)]
        if use_tvl: r.append(math.log10(p['tvl_usd']) - 8)
        if use_fam: r += [1.0 if p['fam'] == f else 0.0 for f in fams[1:]]
        rows.append(r)
    return np.array(rows)
def logit_fit(X, y, ridge=1e-2, it=25):
    b = np.zeros(X.shape[1])
    for _ in range(it):
        p = 1 / (1 + np.exp(-X @ b)); Wd = p * (1 - p) + 1e-9
        H = X.T @ (X * Wd[:, None]) + ridge * np.eye(len(b)); g = X.T @ (y - p) - ridge * b
        b = b + np.linalg.solve(H, g)
    return b
y = np.array([1.0 if p['tok'] else 0.0 for p in al])
for nm, kw in (('age', dict(use_fam=False)), ('age+fam', dict()), ('age+fam+tvl', dict(use_tvl=True))):
    X = X_of(al, **kw); b = logit_fit(X, y)
    # 10-fold CV deterministic
    pr = np.zeros(len(al))
    for k in range(10):
        te = np.arange(len(al)) % 10 == k; bb = logit_fit(X[~te], y[~te]); pr[te] = 1 / (1 + np.exp(-X[te] @ bb))
    acc = ((pr > .5) == (y > .5)).mean(); brier = ((pr - y) ** 2).mean(); base = max(y.mean(), 1 - y.mean()); b0 = (y.mean() - y) ** 2
    print('%-12s coef %s | CV acc %.3f (base %.3f) Brier %.3f (base %.3f)' % (nm, b.round(2), acc, base, brier, b0.mean()))
X = X_of(al); b = logit_fit(X, y)
def F(age, fam_):
    r = [1.0, math.log(max(age, 14) / 365)] + [1.0 if fam_ == f else 0.0 for f in fams[1:]]
    return 1 / (1 + math.exp(-np.dot(r, b)))
print('fams', fams)
for f in fams: print('  %-8s F(90d) %.2f F(1y) %.2f F(2y) %.2f F(3y) %.2f F(5y) %.2f | n %d tok %.2f' % (f, F(90, f), F(365, f), F(730, f), F(1095, f), F(1825, f), len([p for p in al if p['fam'] == f]), np.mean([p['tok'] for p in al if p['fam'] == f])))
def odds_by(t, f, H=1825): return max(0, (F(H, f) - F(t, f)) / (1 - F(t, f)))
def odds_2y(t, f): return (F(t + 730, f) - F(t, f)) / (1 - F(t, f))
pre = sorted([p for p in al if not p['tok']], key=lambda p: -p['tvl_usd'])
print('%-26s %-8s %6s %8s %8s' % ('name', 'fam', 'age', 'by5y', 'in2y'))
for p in pre[:45]: print('%-26s %-8s %6s %8.2f %8.2f' % (p['name'][:26], p['fam'], p['age'], odds_by(p['a'], p['fam']), odds_2y(p['a'], p['fam'])))
print('undated pre:', [(p['name'], p['fam']) for p in pre if p['age'] is None][:20])
# by category instead of family? token share by category among alone
cs = collections.defaultdict(list)
for p in al: cs[p['category']].append(p['tok'])
print(sorted(((k, len(v), round(float(np.mean(v)), 2)) for k, v in cs.items() if len(v) >= 8), key=lambda x: x[2]))
