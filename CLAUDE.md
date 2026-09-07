# kulprit

A `kubectl` plugin that prints a Kubernetes object as YAML with every line
coloured by the field manager that owns it, read from `metadata.managedFields`.

Conventions that apply to every repo live in the Gypsum wiki ("Go Project
Layout", "GitHub Release Process", "GitHub Actions Release Workflow"). Only
what differs for this repo is below.

## Build and test

```sh
make build test vet
```

There are no dependencies outside the standard library, and there should not be
any: the YAML emitter is hand-written because every line has to be traceable
back to the field it came from, which a general-purpose marshaller does not
give you.

The binary is `kubectl-kulprit`, not `kulprit`, because that is how `kubectl`
finds a plugin on PATH.

## Verifying the renderer

Unit tests cover scalar quoting, block scalars and ownership resolution. The
emitter as a whole is checked against a real cluster instead: render every
object, parse the YAML back, and compare with the JSON it came from. Do that
after any change to `internal/render`, with a throwaway script — it needs a
cluster, so it does not belong in `go test`.

## Completion

`kubectl` completes plugin arguments by running `kubectl_complete-kulprit` from
PATH with every word typed after the plugin name — flags included, and with the
word being completed last, even when empty. It expects one candidate per line,
optionally `value<TAB>description`, then a `:<directive>` line (Cobra's
ShellCompDirective; 4 is NoFileComp).

That lookup happens inside the kubectl process, so the helper has to be a file.
A shell function cannot serve as one, `export -f` included — kubectl answers
"does not provide a matching completion executable". Do not accept a bug report
asking for `source <(... completion zsh)` to register a function; it cannot
work. The alternative, wrapping kubectl's own completion function, means
hardcoding `__start_kubectl` / `_kubectl` and losing to whoever sources last.

The helper's name comes from the CLI's root Cobra command, not from the plugin
filename: `lookupCompletionExec` in kubectl's `plugin_completion.go` takes
`cmd.CommandPath()` ("kubectl kulprit"), swaps the space for a dash and the
first dash for `_complete-`. `oc` sets `Use: "oc"` and accepts the `kubectl`
plugin prefix, so it runs the same binary but looks for `oc_complete-kulprit`.
One helper per CLI is therefore installed; `complete.DefaultCLIs` is kubectl
plus any compatible CLI on PATH.

`kulprit completion [bash|zsh|fish]` installs them next to the binary as
symlinks, which the binary recognises by the shape of `os.Args[0]` and serves
directly; a script is the fallback. Nothing is written to stdout, so sourcing
the command out of habit is harmless. `--print` emits the script form for
packagers. The release ships only the binaries; nothing else needs packaging.

`kubectl.Binary` falls back to `oc` when there is no `kubectl` on PATH, so the
plugin works on an OpenShift-only machine. `KUBECTL` overrides it.

`internal/complete.Flags` is the list the shell is offered. It has to stay in
step with `parse` in `cmd/kubectl-kulprit/main.go`, and
`TestParseHandlesEveryCompletableFlag` fails if it drifts — that is how the
`--color never` form was found to be unparseable.

## Gotchas

- `kubectl get` strips `managedFields` from its output unless you pass
  `--show-managed-fields`. Without it there is nothing to colour.
- `managedFields` entries for a subresource (`status`) use full object paths,
  not paths relative to the subresource.
- A manager can hold several entries. They share one index, and one colour.
- Nothing in completion may write to stderr: the user is mid-keystroke and it
  lands in their command line. Use `kubectl.GetQuiet`, and stay silent on error.

## Release notes

- CHANGELOG heading format: `## [X.Y.Z] - YYYY-MM-DD`. The release workflow
  extracts the section by that exact heading and fails if it is missing.
- Release artifacts are `linux` and `darwin` on `amd64` and `arm64` only. No
  Windows build, no container image, no Helm chart.
- `make dist` builds the same four platforms locally, with the plainer
  `<binary>-<os>-<arch>` names.
