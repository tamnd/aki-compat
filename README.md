# aki-compat

Black-box Redis-wire compatibility tests for [aki](https://github.com/tamnd/aki).

aki is a Redis-wire-compatible single-file database.
The claim that matters for users is simple: a client written for Redis should talk to aki unchanged and get the same answers.
This repo checks that claim the only honest way, by running the same commands against aki, real Redis, and Valkey at the same time and asserting the replies agree.

## The model

The harness is differential.
It does not encode what aki "should" return in a golden file.
It asks the reference implementations and treats their answer as the truth.
When all three are present, Redis is the baseline and both Valkey and aki are checked against it.
When only two are present, the first is the baseline and the other is checked against it.

A test is a `Case`: a named sequence of commands run on one fresh connection per target.
Running a sequence rather than a single command lets a case set up state and then probe it, for example `SET k v` then `GET k` then `TYPE k` then `OBJECT ENCODING k`, and compare the whole trace.
The runner flushes the database before each case so cases do not leak into each other.
For every step it compares each target's reply to the baseline's, and the case fails at the first step where they diverge.

Replies are compared on the decoded RESP tree, not on raw bytes, so the few legitimate differences are handled in one place:

- A RESP2 map is a flat array of pairs and a RESP3 map is `%`. A RESP2 set is a plain array and a RESP3 set is `~`. The comparator folds these to a canonical form so a RESP2 target and a RESP3 target agree on the same logical reply.
- Sets are unordered, so members are sorted before comparison.
- Maps are unordered by key, so pairs are sorted by key before comparison.

Everything else stays exact.
A simple string is not equal to a bulk string with the same text.
A `$-1` null bulk is tracked distinctly from a `_` RESP3 null.
Differing integers, differing strings, and differing error text are real compatibility signals and the harness never papers over them.

## Layout

```
respwire/      native RESP2 and RESP3 client: send raw commands, decode exact replies, compare
target/        launch (aki server, redis-server, valkey-server) or connect to a running addr
differential/  the case table, the runner, and the suite test
goredis/       reserved stub for future library-level checks through the go-redis client
cmd/aki-compat command line runner that prints a pass/fail report
```

The differential path uses no third-party client on purpose.
A general purpose client hides exactly the distinctions a compatibility test must see, so `respwire` does the framing itself.

## Running it

Spawn all three from `PATH` (needs `aki`, `redis-server`, and `valkey-server` installed):

```
go run ./cmd/aki-compat
```

Compare aki against an already running Redis:

```
go run ./cmd/aki-compat -aki-addr 127.0.0.1:7000 -redis-addr 127.0.0.1:6379
```

The same suite runs under `go test`:

```
go test ./...
```

The suite test reads target addresses from the environment, so CI can point it at running servers without installing anything:

```
AKI_COMPAT_AKI_ADDR     connect aki at this host:port
AKI_COMPAT_REDIS_ADDR   connect redis at this host:port
AKI_COMPAT_VALKEY_ADDR  connect valkey at this host:port
```

When an address is unset the harness tries to spawn that server from `PATH`.
A missing binary is a skip for that target, not a failure.
The differential model needs at least two live targets, so on a machine with none the suite skips and the run stays green.
The `respwire` and `differential` unit tests run everywhere with no servers at all.

## CI

The default CI job runs `gofmt`, `go vet`, `go build`, and `go test` with no servers installed, so it exercises the codec, the comparator, and the skip path.
A second job installs Redis and Valkey from apt and runs the real differential suite against them.
That job is best-effort: a green baseline does not depend on apt, but when the job succeeds it gives real cross-implementation coverage.

## License

BSD 3-Clause. See [LICENSE](LICENSE).
