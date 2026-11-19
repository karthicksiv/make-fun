// Entry point for the forge: launches the statically linked Go binary that is committed beside this
// file (bin/forge, built from the Go sources in forge/) and mirrors its exit status.
//
// The Go process owns the whole trial lifecycle: it binds an ephemeral port, POSTs that port to
// $MAKEFUN_ENV_URL/register, serves git smart HTTP, REST v3, GraphQL v4 and MCP from memory, polls
// $MAKEFUN_ENV_URL/done and exits 0 once the suite says it is finished. This file only starts it,
// forwards signals, and falls back to a private copy of the binary when the repository mount does
// not allow executing files in place.

import { spawn, type ChildProcess } from 'node:child_process';
import { chmodSync, copyFileSync, mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const committedBinary = join(here, 'bin', 'forge');

interface Outcome {
  /** True when the binary could not be started at all (as opposed to started and exited). */
  readonly spawnFailed: boolean;
  readonly code: number;
}

let child: ChildProcess | null = null;

for (const signal of ['SIGTERM', 'SIGINT', 'SIGHUP'] as const) {
  process.on(signal, () => {
    if (child !== null && child.exitCode === null) child.kill(signal);
    else process.exit(1);
  });
}

/** Run the forge binary at `path` to completion, with this process's environment and stdio. */
function run(path: string): Promise<Outcome> {
  return new Promise<Outcome>((resolve) => {
    let settled = false;
    const settle = (outcome: Outcome): void => {
      if (settled) return;
      settled = true;
      resolve(outcome);
    };
    let started: ChildProcess;
    try {
      started = spawn(path, [], { stdio: 'inherit', env: process.env });
    } catch (err) {
      process.stderr.write(`[entry] cannot start ${path}: ${String(err)}\n`);
      settle({ spawnFailed: true, code: 1 });
      return;
    }
    child = started;
    started.once('error', (err) => {
      process.stderr.write(`[entry] cannot start ${path}: ${err.message}\n`);
      settle({ spawnFailed: true, code: 1 });
    });
    started.once('exit', (code, signal) => {
      // 126 and 127 are what an exec that could not happen looks like through a shell-less spawn.
      const failedExec = code === 126 || code === 127;
      settle({ spawnFailed: failedExec, code: code ?? (signal === null ? 1 : 1) });
    });
  });
}

/** Copy the binary somewhere writable and executable, for a mount that refuses to exec in place. */
function privateCopy(): string {
  const dir = mkdtempSync(join(tmpdir(), 'forge-'));
  const target = join(dir, 'forge');
  copyFileSync(committedBinary, target);
  chmodSync(target, 0o755);
  return target;
}

let outcome = await run(committedBinary);
if (outcome.spawnFailed) {
  try {
    outcome = await run(privateCopy());
  } catch (err) {
    process.stderr.write(`[entry] cannot copy the forge binary: ${String(err)}\n`);
  }
}
process.exit(outcome.code);
