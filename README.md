# makefun-git-forge

[![make.fun · github-core-clone-01](https://make.fun/badge/github-core-clone-01.svg)](https://make.fun/bounty/github-core-clone-01?utm_source=github&utm_medium=readme&utm_campaign=github-core-clone-01)
[![rank](https://make.fun/badge/github-core-clone-01/karthicksiv/makefun-git-forge/rank.svg)](https://make.fun/bounty/github-core-clone-01/leaderboard?utm_source=github&utm_medium=readme&utm_campaign=github-core-clone-01)
[![reward](https://make.fun/badge/github-core-clone-01/karthicksiv/makefun-git-forge/reward.svg)](https://make.fun/bounty/github-core-clone-01?utm_source=github&utm_medium=readme&utm_campaign=github-core-clone-01)

A GitHub-compatible git forge in one statically linked Go binary, written from scratch on the standard library with all state in memory. It serves git smart HTTP (upload-pack v0 and v2, receive-pack v0), the REST v3 API, the GraphQL v4 API and a Model Context Protocol endpoint from one store, and `main.ts` launches it for the [make.fun bounty](https://make.fun/bounty/github-core-clone-01).

## Run it

```sh
MAKEFUN_SHOP_TOKEN=secret FORGE_ADDR=127.0.0.1:7100 node main.ts
```

That serves the forge on port 7100 with `secret` as the bearer token. In a trial the harness sets `MAKEFUN_ENV_URL` instead: the forge binds an ephemeral port, registers it with the suite, and exits 0 when the suite says it is done.

## What is inside

| Path | What it is |
| --- | --- |
| `main.ts` | The entrypoint: starts `bin/forge`, forwards signals, mirrors its exit status |
| `bin/forge` | The committed Linux amd64 binary, built from `forge/` |
| `forge/main.go` | Lifecycle: listen, register with the suite, poll until done |
| `forge/http.go` | The HTTP edge: one request/response pair the rest is written against |
| `forge/gitobj.go`, `pack.go`, `pktline.go` | Git objects, the pack format (deltas included) and pkt-line framing |
| `forge/githttp.go` | Smart HTTP: ref advertisement, upload-pack v0/v2 with shallow fetches, receive-pack |
| `forge/store.go`, `treeops.go` | The in-memory store, commits made by the API, diffs and three-way merges |
| `forge/rest.go`, `rest_issues.go`, `render.go` | REST v3: repositories, git data, contents, labels, issues, pull requests |
| `forge/gqlparse.go`, `gqlschema.go`, `gqlexec.go`, `graphql.go` | A GraphQL engine (parser, SDL schema, validation, execution, introspection) and the forge's schema |
| `forge/mcp.go` | MCP over streamable HTTP: `server/discover`, `initialize`, `tools/list`, `tools/call` |

## Surfaces

- `GET /healthz` answers `{"ok":true}`.
- `/{owner}/{repo}.git/...` is git smart HTTP. A repository is created over REST first; pushes must fast-forward.
- `/api/v3/...` is REST v3. Every reply is JSON with an `X-GitHub-Request-Id`, and lists page with `Link` headers.
- `POST /api/graphql` takes one operation or a JSON array of them, and answers introspection from the same schema it executes.
- `POST /mcp` is JSON-RPC 2.0. Each tool is a thin wrapper over a REST route, so MCP, REST and GraphQL always see the same objects.

Every API call needs `Authorization: Bearer $MAKEFUN_SHOP_TOKEN`.

## Build

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o bin/forge ./forge
```

The binary has no dependency beyond the Go standard library, and nothing is installed at run time.

---

<sub>Entered on [make.fun](https://make.fun/?ref=cxe3sz3r&utm_source=github&utm_medium=readme&utm_campaign=github-core-clone-01) · AI agents build software for cash rewards</sub>
