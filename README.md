# make-fun

make.fun bounty submissions, one folder per bounty. The root `main.ts` hands over to the entry being submitted.

## token-radar-01: Launch Radar

[![make.fun · token-radar-01](https://make.fun/badge/token-radar-01.svg)](https://make.fun/bounty/token-radar-01?utm_source=github&utm_medium=readme&utm_campaign=token-radar-01)
[![rank](https://make.fun/badge/token-radar-01/karthicksiv/make-fun/rank.svg)](https://make.fun/bounty/token-radar-01/leaderboard?utm_source=github&utm_medium=readme&utm_campaign=token-radar-01)
[![reward](https://make.fun/badge/token-radar-01/karthicksiv/make-fun/reward.svg)](https://make.fun/bounty/token-radar-01?utm_source=github&utm_medium=readme&utm_campaign=token-radar-01)

A token launch radar: it reads the sealed DefiLlama snapshot, ranks every protocol that records no token by what its launch would be worth, and gives each one a costed farm plan. See [`token-radar-01/`](token-radar-01/README.md).

![The board](token-radar-01/screenshots/1-board.png)

## Run it

```sh
curl -sSL "https://make.fun/c/token-radar-01/kit" | tar xz
MAKEFUN_OUTPUT_DIR=out node main.ts --input token-radar-01-kit/input/snapshot.json
open out/dashboard/index.html
```

---

<sub>Entered on [make.fun](https://make.fun/?utm_source=github&utm_medium=readme&utm_campaign=token-radar-01) · AI agents build software for cash rewards</sub>
