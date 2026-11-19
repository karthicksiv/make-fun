# Dev check: the seven gate rows as RULES.txt states them, run on an output directory.
import json, os, re, sys, hashlib
from PIL import Image
out, snap_path = sys.argv[1], sys.argv[2]
snap = json.load(open(snap_path)); ok = True
def row(name, good, detail=''):
    global ok; ok &= bool(good); print(('PASS ' if good else 'FAIL ') + name + (': ' + detail if detail else ''))
files = [os.path.join(d, f) for d, _, fs in os.walk(os.path.join(out, 'dashboard')) for f in fs]
size = sum(os.path.getsize(f) for f in files); names = [os.path.relpath(f, os.path.join(out, 'dashboard')) for f in files]
bad_name = [n for n in names if not all(re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9._-]{0,127}', p) for p in n.split('/'))]
net = [n for n, f in zip(names, files) if n.endswith(('.html', '.css', '.js')) and re.search(rb'https?://', open(f, 'rb').read())]
row('dashboard_files', 'index.html' in names and any(n.endswith('.css') for n in names) and any(n.endswith('.js') for n in names) and len(files) <= 400 and size <= 4 * 2**20 and not bad_name and not net, f'{len(files)} files, {size} bytes, http(s) mentions in {net or "none"}')
board = json.load(open(os.path.join(out, 'board.json'))); rows = board['rows']; slugs = {p['slug'] for p in snap['protocols']}
row('board_file', 100 <= len(rows) <= 4000 and all(r['slug'] in slugs and r['name'] and isinstance(r['rank'], int) and isinstance(r['implied_value_usd'], int) and 0 < r['implied_value_usd'] <= 10**14 for r in rows) and len({r['slug'] for r in rows}) == len(rows) and os.path.getsize(os.path.join(out, 'board.json')) <= 8 * 2**20 and len({r['implied_value_usd'] for r in rows}) > 1, f'{len(rows)} rows, {os.path.getsize(os.path.join(out, "board.json"))} bytes')
by = sorted(rows, key=lambda r: r['rank'])
row('sorted_by_value', [r['rank'] for r in by] == list(range(1, len(rows) + 1)) and all(a['implied_value_usd'] >= b['implied_value_usd'] for a, b in zip(by, by[1:])), f'top {by[0]["implied_value_usd"]}, bottom {by[-1]["implied_value_usd"]}')
row('coverage', set(snap['watchlist']) <= {r['slug'] for r in rows}, f'{len(set(snap["watchlist"]) & {r["slug"] for r in rows})}/{len(snap["watchlist"])} watchlist')
lens = [len(s.strip()) for r in rows for s in r['farm_steps']]
row('farm_steps', all(3 <= len(r['farm_steps']) <= 12 for r in rows) and 20 <= min(lens) and max(lens) <= 600, f'{len(rows[0]["farm_steps"])} steps a row, {min(lens)} to {max(lens)} characters')
shots = sorted(f for f in os.listdir(os.path.join(out, 'screenshots')) if not f.startswith('.')); dims = []
for s in shots:
    im = Image.open(os.path.join(out, 'screenshots', s)); dims.append((s, im.size, os.path.getsize(os.path.join(out, 'screenshots', s))))
row('screenshots', 3 <= len(shots) <= 4 and all(max(d) <= 1440 and min(d) >= 800 and b <= 524288 for _, d, b in dims), '; '.join(f'{s} {d[0]}x{d[1]} {b // 1024} KiB' for s, d, b in dims))
dsize = os.path.getsize(os.path.join(out, 'dossier.json')); d = json.load(open(os.path.join(out, 'dossier.json')))
row('dossier_matches (size and shape only; bytes are buildDossier\'s own)', dsize <= 1.5 * 2**20 and len(d['text'].encode()) <= 65536 and 'Cut at' not in d['text'], f'{dsize} bytes of {int(1.5 * 2**20)}, text {len(d["text"].encode())} of 65536 bytes, uncut')
sys.exit(0 if ok else 1)
