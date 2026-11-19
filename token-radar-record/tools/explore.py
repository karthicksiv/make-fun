import json, sys, math, collections, datetime
snap = json.load(open(sys.argv[1]))
P = snap['protocols']; W = set(snap['watchlist'])
print('as_of', snap['as_of'], 'source', snap['source'], 'floor', snap['floor_usd'], 'n', len(P), 'watch', len(W))
tok = [p for p in P if p['gecko_id'] or p['symbol']]
notok = [p for p in P if not (p['gecko_id'] or p['symbol'])]
mc = [p for p in P if p['mcap_usd']]
print('token recorded', len(tok), 'no token', len(notok), 'with mcap', len(mc), 'mcap but no token id', len([p for p in mc if not (p['gecko_id'] or p['symbol'])]))
print('token but no mcap', len([p for p in tok if not p['mcap_usd']]))
print('symbol only', len([p for p in P if p['symbol'] and not p['gecko_id']]), 'gecko only', len([p for p in P if p['gecko_id'] and not p['symbol']]))
print('null listed_at', len([p for p in P if not p['listed_at']]), 'null change', len([p for p in P if p['change_7d'] is None]), 'null cat', len([p for p in P if not p['category']]))
c = collections.Counter(p['category'] for p in P)
ct = collections.Counter(p['category'] for p in tok)
cm = collections.Counter(p['category'] for p in mc)
cn = collections.Counter(p['category'] for p in notok)
cw = collections.Counter(p['category'] for p in P if p['slug'] in W)
print('%-28s %5s %5s %5s %5s %5s' % ('category','all','tok','mcap','notok','watch'))
for k, v in c.most_common():
    print('%-28s %5d %5d %5d %5d %5d' % (k, v, ct[k], cm[k], cn[k], cw[k]))
asof = datetime.date.fromisoformat(snap['as_of'])
def age(p):
    return (asof - datetime.date.fromisoformat(p['listed_at'])).days if p['listed_at'] else None
import statistics as st
for name, grp in (('tok', tok), ('notok', notok), ('watch', [p for p in P if p['slug'] in W])):
    a = sorted(x for x in map(age, grp) if x is not None)
    t = sorted(p['tvl_usd'] for p in grp)
    ch = sorted(p['change_7d'] for p in grp if p['change_7d'] is not None)
    q = lambda v, f: v[int(f*(len(v)-1))]
    print(name, 'age q10/50/90', q(a,.1), q(a,.5), q(a,.9), 'min', a[0], 'max', a[-1], '| tvl q10/50/90 %.0fM %.0fM %.0fM' % (q(t,.1)/1e6, q(t,.5)/1e6, q(t,.9)/1e6), '| chg q05/25/50/75/95', q(ch,.05), q(ch,.25), q(ch,.5), q(ch,.75), q(ch,.95))
r = sorted(p['mcap_usd']/p['tvl_usd'] for p in mc)
print('mcap/tvl q10/25/50/75/90', [round(r[int(f*(len(r)-1))],3) for f in (.1,.25,.5,.75,.9)], 'min', r[0], 'max', r[-1])
nch = collections.Counter(min(len(p['chains']),6) for p in P); print('chains', sorted(nch.items()))
chains = collections.Counter(ch for p in notok for ch in p['chains'][:1]); print('first chain (notok)', chains.most_common(30))
print('watchlist sample:')
for p in sorted((p for p in P if p['slug'] in W), key=lambda p:-p['tvl_usd'])[:40]:
    print('  %-28s %-24s %8.0fM chg %7s age %5s chains %s' % (p['name'][:28], p['category'], p['tvl_usd']/1e6, p['change_7d'], age(p), ','.join(p['chains'][:4])))
