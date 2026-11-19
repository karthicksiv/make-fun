# Launch Radar: working record

Work in progress on the [`token-radar-01`](../token-radar-01) entry, kept here so that none of it lives on one machine only.

This folder is not part of the entry. `main.ts` never reads it, the dashboard never loads it, and it is left out of the commit that is submitted, so the entry make.fun judges is `token-radar-01/` and the root `main.ts` alone.

| Path | What it holds |
| --- | --- |
| `NOTES.md` | One dated entry per round: what was built, how it was checked, what the rules changed |
| `patches/` | Each round's changes to `token-radar-01/` that are not yet in the entry, as one patch against the last submitted commit |
| `tools/` | The dev scripts the entry is checked with: the judge's view of the screenshots and source, the seven gate rows, overflow at every width, interaction tests |

To bring a round's work back onto a fresh checkout, from the repository root:

```sh
git apply token-radar-record/patches/<the round's file>
```

The tools expect Node 22, Playwright with Chromium, and the bounty's kit. None of them is run by `main.ts`.
