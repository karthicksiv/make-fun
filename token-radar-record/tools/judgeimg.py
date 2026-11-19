# Dev tool: compose the judge's two images the way dossier-view/v1 does (lanczos fit, JPEG q82 4:2:0).
import sys
from PIL import Image
shots, out = sys.argv[1:-1], sys.argv[-1]
def fit(im, w, h):
    im = im.convert('RGB'); s = min(w / im.width, h / im.height, 1)
    return im.resize((max(1, round(im.width * s)), max(1, round(im.height * s))), Image.LANCZOS)
cover = fit(Image.open(shots[0]), 1024, 640)
cover.save(out + '-cover.jpg', quality=82, subsampling=2)
rest = shots[1:]
import math
cols = math.ceil(math.sqrt(len(rest))); rows = math.ceil(len(rest) / cols)
cw = (1024 - (cols - 1) * 4) // cols; ch = (640 - (rows - 1) * 4) // rows
cells = [fit(Image.open(p), cw, ch) for p in rest]
W = max(sum(c.width for c in cells[r*cols:(r+1)*cols]) + 4 * (len(cells[r*cols:(r+1)*cols]) - 1) for r in range(rows))
H = sum(max(c.height for c in cells[r*cols:(r+1)*cols]) for r in range(rows)) + 4 * (rows - 1)
sheet = Image.new('RGB', (W, H), (0, 0, 0)); y = 0
for r in range(rows):
    row = cells[r*cols:(r+1)*cols]; rh = max(c.height for c in row)
    x = (W - (sum(c.width for c in row) + 4 * (len(row) - 1))) // 2
    for c in row:
        sheet.paste(c, (x, y + (rh - c.height) // 2)); x += c.width + 4
    y += rh + 4
sheet.save(out + '-sheet.jpg', quality=82, subsampling=2)
print('cover', cover.size, 'sheet', sheet.size, 'cell', (cw, ch))
