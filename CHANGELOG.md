# Changelog

All notable changes to kulprit are recorded here.

## [0.1.0] - 2026-09-07

### Added

- `kubectl kulprit <resource>` renders an object as YAML with each line's
  background set by the field manager that owns it, read from
  `metadata.managedFields`.
- A legend above each object listing every manager with its operation,
  apiVersion, subresource and time.
- Fields owned by more than one manager get their own colour and a
  `# manager, manager` comment naming them.
- `-m/--manager` highlights one manager and dims the rest; repeatable.
- `--annotate` writes ownership as YAML comments, on by default when colour is
  off so the output stays useful in a pipe or a file.
- `--color=auto|always|never`, honouring `NO_COLOR` and a non-terminal stdout.
- `--show-managed-fields` keeps the `managedFields` block in the output.
- Every other flag is passed straight to `kubectl get`, including `-n`,
  `--context`, `-l` and `-A`.
