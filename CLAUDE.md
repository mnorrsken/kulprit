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

## Gotchas

- `kubectl get` strips `managedFields` from its output unless you pass
  `--show-managed-fields`. Without it there is nothing to colour.
- `managedFields` entries for a subresource (`status`) use full object paths,
  not paths relative to the subresource.
- A manager can hold several entries. They share one index, and one colour.

## Release notes

- CHANGELOG heading format: `## [X.Y.Z] - YYYY-MM-DD`. The release workflow
  extracts the section by that exact heading and fails if it is missing.
- Release artifacts are `linux` and `darwin` on `amd64` and `arm64` only. No
  Windows build, no container image, no Helm chart.
- `make dist` builds the same four platforms locally, with the plainer
  `<binary>-<os>-<arch>` names.
