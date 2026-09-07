# Changelog

All notable changes to kulprit are recorded here.

## [0.2.1] - 2026-09-07

### Fixed

- Completion did not work under `oc`. The CLI builds the helper's name from its
  own root command, so `oc` looks for `oc_complete-kulprit` while only
  `kubectl_complete-kulprit` was ever installed. `completion` now installs one
  helper per CLI: kubectl always, plus any compatible CLI found on PATH.
  `--for <cli>` names them by hand.

### Added

- kulprit falls back to running `oc` when there is no `kubectl` on PATH, so it
  works on a machine that only has the OpenShift CLI. `KUBECTL` still overrides
  the choice.

## [0.2.0] - 2026-09-07

### Added

- Shell completion. `kubectl kulprit completion [bash|zsh|fish]` installs the
  helper kubectl needs, next to the binary, and says what the shell still
  wants. `kubectl kulprit <TAB>` then completes resource types, names,
  namespaces and flags.
- Completing `-m/--manager` offers the field managers of the object on the
  command line, read from its `managedFields`.
- `completion --print` writes the helper to stdout for packagers.

### Notes

- Releases still contain only the four binaries. The completion helper is
  created on the machine by the command above, not shipped, so it works however
  the plugin was installed.

### Fixed

- `--color never` written as two words was not parsed: the value was passed to
  `kubectl get` as if it were a resource name. Both `--color never` and
  `--color=never` work now, and a bare `--color` still means `always`.

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
