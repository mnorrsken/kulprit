# kulprit

[![CI](https://github.com/mnorrsken/kulprit/actions/workflows/ci.yml/badge.svg)](https://github.com/mnorrsken/kulprit/actions/workflows/ci.yml)

Find out who wrote what in a Kubernetes object.

Every field of a live object carries a record of the client that last wrote it,
in `metadata.managedFields`. Nobody reads it, because it is a wall of nested
`f:` keys nowhere near the fields it describes. `kulprit` prints the object as
ordinary YAML and colours each line by the manager that owns it, so "why does
this deployment have three replicas when the chart says two" takes one command.

It is a `kubectl` plugin: run it as `kubectl kulprit`.

## Features

- **Ownership as colour** — each line's background is the field manager that
  owns that field, so a whole block written by Helm or by a controller is one
  glance away.
- **Contested fields stand out** — a field owned by more than one manager gets
  its own colour and a comment naming them, which is usually the answer to "why
  does this keep changing back".
- **A legend per object** — every manager with its operation, apiVersion,
  subresource and the time it last wrote.
- **Focus on one manager** — `-m helm` highlights it and dims everything else.
- **Still YAML** — with colour off, ownership is written as comments and the
  output parses as ordinary YAML.
- **Every `kubectl get` flag works** — `-n`, `-A`, `-l`, `--context` and the
  rest are passed straight through.
- **No dependencies** — standard library only, a ~3 MB static binary.

## Install

```sh
go install github.com/mnorrsken/kulprit/cmd/kubectl-kulprit@latest
```

or from a checkout:

```sh
make install     # go install, into $GOBIN
```

Binaries for linux and macOS on amd64 and arm64 are attached to each
[release](https://github.com/mnorrsken/kulprit/releases).

`kubectl` finds any `kubectl-*` binary on `PATH`, so with `$GOBIN` on `PATH`
the plugin is ready:

```sh
kubectl kulprit deployment argocd-server -n argocd
```

## Use

`kulprit` runs `kubectl get` for you and passes everything it does not
recognise straight through, so every flag that command takes still works:

```sh
kubectl kulprit deploy/gypsum -n gypsum
kubectl kulprit ingress -A
kubectl kulprit cm -l app=cilium --context prod
kubectl kulprit svc gypsum -n gypsum -m helm      # highlight one manager
kubectl kulprit deploy -A --no-color > owners.yaml
```

With colour off, ownership is written as comments instead, so the output stays
readable in a pipe or a file — and stays valid YAML:

```yaml
# ingress/gypsum -n gypsum
# argocd-controller  Apply networking.k8s.io/v1 2026-08-16T23:24:53Z
#                    Update networking.k8s.io/v1 2026-08-27T17:44:50Z
# traefik            Update networking.k8s.io/v1 (status) 2026-02-25T20:27:03Z
# node-fetch         Update networking.k8s.io/v1 2026-03-12T22:15:00Z
# helm               Update networking.k8s.io/v1 2026-03-24T22:17:08Z
#
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: gypsum
  namespace: gypsum
  annotations:  # helm
    argocd.argoproj.io/tracking-id: gypsum:networking.k8s.io/Ingress:gypsum/gypsum  # argocd-controller
    freelens.app/resource-version: v1  # node-fetch
    meta.helm.sh/release-name: gypsum  # helm
    meta.helm.sh/release-namespace: gypsum
    traefik.ingress.kubernetes.io/router.entrypoints: websecure, external  # argocd-controller, helm
    traefik.ingress.kubernetes.io/router.middlewares: authelia-authelia@kubernetescrd  # argocd-controller
  creationTimestamp: "2026-02-25T20:27:03Z"
  generation: 1
  labels:  # helm
    app.kubernetes.io/instance: gypsum  # argocd-controller, helm
    app.kubernetes.io/managed-by: Helm
    app.kubernetes.io/name: gypsum
    app.kubernetes.io/version: 0.51.0  # argocd-controller
    helm.sh/chart: gypsum-0.51.0
  resourceVersion: "826126465"
  uid: 5ef0a095-53e4-4e44-a4f0-f5e36c9ccdc4
spec:
  ingressClassName: traefik
  rules:  # argocd-controller, helm
  - host: gypsum.snosr.se
    http:
      paths:
      - backend:
          service:
            name: gypsum
            port:
              name: http
        path: /
        pathType: Prefix
status:  # traefik
  loadBalancer:
    ingress:
    - ip: 10.10.41.167
```

A comment marks where ownership *changes*; a line without one belongs to the
same managers as the line above it.

### Flags

| Flag | Meaning |
| --- | --- |
| `--color=auto\|always\|never` | Colour the output. `auto` colours only when stdout is a terminal and `NO_COLOR` is unset. |
| `--no-color` | Same as `--color=never`. |
| `--no-legend` | Drop the manager list above each object. |
| `--annotate` / `--no-annotate` | Force the `# manager` comments on or off. On by default when colour is off. |
| `-m`, `--manager <name>` | Highlight only this manager, dim the rest. Repeatable, matches exactly or as a substring. |
| `--show-managed-fields` | Keep the `managedFields` block in the output. |

Set `KUBECTL` to run something other than plain `kubectl`.

## How the colours are decided

`managedFields` holds one `fieldsV1` set per writer. Each is a nested object
whose keys step down through the resource: `f:<name>` for a map key, `i:<n>` for
a list index, `v:<json>` for a scalar in a set, and `k:<json>` for an element of
an associative list picked out by its key fields. A `.` key means the node it
sits in is itself owned, rather than merely being on the way to something owned.

`kulprit` merges every manager's set into one tree and walks it alongside the
object. From that:

- **One owner** — the line gets that manager's colour.
- **Several owners** — the line gets the shared colour and a comment naming
  them. This is normal where a client-side `kubectl apply` and a server-side
  apply have both written a field.
- **A node with nothing named below it** is atomic: the manager owns the whole
  subtree, and everything inside inherits the colour. `f:selector: {}` is the
  common case.
- **A container nobody claims** — a line such as `spec:` — takes the colour of
  the single manager that owns everything below it, if there is exactly one.
  Otherwise it is left uncoloured.
- **No colour at all** means no manager claims the field. `uid`,
  `resourceVersion` and `creationTimestamp` are set by the API server and never
  appear in `managedFields`.

## Notes

- Objects written before server-side apply existed, or by clients that bypass
  it, carry no `managedFields`. `kulprit` says so instead of colouring nothing.
- `managedFields` is stripped from `kubectl get` output by default, so `kulprit`
  asks for it explicitly. It then drops the block from what it prints, since the
  colours say the same thing; `--show-managed-fields` keeps it.
- Keys are sorted alphabetically, except that `name` and `namespace` come first
  at every level. That needs no schema and puts the useful keys at the top, but
  it does mean the order differs from `kubectl get -o yaml`.

## Development

```sh
make build test vet
make dist            # linux and darwin, amd64 and arm64, into dist/
```

The YAML emitter is hand-written so that every line can be tied back to the
field it came from. It was validated against a live cluster by printing every
object in it and parsing the result back: all 1423 came back equal to the JSON
they were rendered from, `--show-managed-fields` and comments included.
