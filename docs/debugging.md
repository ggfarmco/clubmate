# Debugging the core

## Profiles

`debug.pprof` `{profile, debug?, seconds?, gc?}` returns a Go runtime profile of the running core as `{profile, data}` (`data` is base64 in JSON). It is served only on the core socket (mode 0600, peer uid checked), never over TCP, and refuses plugin callers.

- `profile`: any `runtime/pprof` profile (`heap`, `allocs`, `goroutine`, `block`, `mutex`, `threadcreate`) or `cpu`. An unknown name is invalid params.
- `debug`: `0` (default) gives the gzipped protobuf for `go tool pprof`; `1` or `2` give the text form (`goroutine` with `2` is a full stack dump).
- `seconds`: length of a `cpu` profile, default 10, at most 120. Only one cpu profile runs at a time.
- `gc`: run a GC before writing the profile, so `heap` shows live memory only.

`clubmate debug pprof <profile> [-o file] [--debug N] [--seconds N] [--gc]` calls it and writes `data` to `-o` (default `./<profile>.pprof`; with `--debug` the text goes to stdout, `-o -` forces stdout). It talks to the core of the current flavor and state dir, like every other command, so the dev core needs the dev `clubmate` or `CLUBMATE_RUNTIME_DIR`.

```sh
clubmate debug pprof heap --gc -o heap.pprof
go tool pprof -top heap.pprof
clubmate debug pprof goroutine --debug 1 | head
clubmate debug pprof cpu --seconds 30 -o cpu.pprof
go tool pprof -http=: cpu.pprof
```

To compare growth, take `heap --gc` twice some time apart and diff them: `go tool pprof -top -diff_base heap1.pprof heap2.pprof`.
