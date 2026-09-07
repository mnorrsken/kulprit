// Command kubectl-kulprit prints a Kubernetes object as YAML, colouring every
// field by the manager that owns it according to metadata.managedFields.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mnorrsken/kulprit/internal/fieldpath"
	"github.com/mnorrsken/kulprit/internal/kubectl"
	"github.com/mnorrsken/kulprit/internal/render"
	"github.com/mnorrsken/kulprit/internal/version"
)

func main() {
	err := run(os.Args[1:])
	if err == nil {
		return
	}
	// kubectl has already said what went wrong; just carry its status out.
	var reported *kubectl.Reported
	if errors.As(err, &reported) {
		os.Exit(reported.Code)
	}
	fmt.Fprintf(os.Stderr, "%s: %v\n", invocation(), err)
	os.Exit(1)
}

// invocation is how the user reached us: "kubectl kulprit" when kubectl
// dispatched to the plugin, otherwise the binary's own name. Only the exact
// plugin name counts, so a release artifact that still carries its version and
// platform in the filename reports itself as it was run.
func invocation() string {
	base := filepath.Base(os.Args[0])
	if base == "kubectl-kulprit" {
		return "kubectl kulprit"
	}
	return base
}

type options struct {
	color    string
	legend   bool
	annotate *bool
	only     []string
	showMF   bool
	args     []string
}

func run(argv []string) error {
	opts, done, err := parse(argv)
	if err != nil || done {
		return err
	}

	data, err := kubectl.Get(opts.args)
	if err != nil {
		return err
	}
	doc, err := fieldpath.Decode(data)
	if err != nil {
		return fmt.Errorf("parsing kubectl output: %w", err)
	}

	color := useColor(opts.color)
	annotate := !color
	if opts.annotate != nil {
		annotate = *opts.annotate
	}

	out := bufio.NewWriter(os.Stdout)
	n, err := render.Print(out, doc, render.Options{
		Color:             color,
		Legend:            opts.legend,
		Annotate:          annotate,
		ShowManagedFields: opts.showMF,
		Only:              opts.only,
	})
	if ferr := out.Flush(); err == nil {
		err = ferr
	}
	if err != nil {
		return err
	}
	if n == 0 {
		fmt.Fprintln(os.Stderr, "No resources found.")
	}
	return nil
}

// parse pulls kulprit's own flags out of the command line and leaves the rest
// for kubectl, so every "kubectl get" flag keeps working. It reports done when
// it has already written the output the user asked for.
func parse(argv []string) (options, bool, error) {
	opts := options{color: "auto", legend: true}

	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		name, value, hasValue := strings.Cut(arg, "=")

		// next consumes a separate value token for a flag written without "=".
		next := func() (string, error) {
			if hasValue {
				return value, nil
			}
			if i+1 >= len(argv) {
				return "", fmt.Errorf("flag %s needs a value", name)
			}
			i++
			return argv[i], nil
		}

		switch name {
		case "-h", "--help":
			usage(os.Stdout)
			return opts, true, nil
		case "--version":
			fmt.Printf("%s %s (%s)\n", invocation(), version.Version, version.Commit)
			return opts, true, nil
		case "--color":
			v := "always"
			if hasValue {
				v = value
			}
			switch v {
			case "auto", "always", "never":
				opts.color = v
			default:
				return opts, false, fmt.Errorf("--color must be auto, always or never, got %q", v)
			}
		case "--no-color":
			opts.color = "never"
		case "--legend":
			opts.legend = !hasValue || value == "true"
		case "--no-legend":
			opts.legend = false
		case "--annotate":
			b := !hasValue || value == "true"
			opts.annotate = &b
		case "--no-annotate":
			b := false
			opts.annotate = &b
		case "-m", "--manager", "--only":
			v, err := next()
			if err != nil {
				return opts, false, err
			}
			opts.only = append(opts.only, v)
		case "--show-managed-fields":
			opts.showMF = !hasValue || value == "true"
		case "-o", "--output":
			return opts, false, fmt.Errorf("%s is set by kulprit and cannot be overridden", name)
		default:
			opts.args = append(opts.args, arg)
		}
	}

	if len(opts.args) == 0 {
		usage(os.Stderr)
		return opts, true, fmt.Errorf("no resource given")
	}
	return opts, false, nil
}

// useColor decides whether to emit escape codes, honouring NO_COLOR and a
// non-terminal stdout.
func useColor(mode string) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func usage(w *os.File) {
	fmt.Fprintf(w, `%[1]s - show which field manager owns which part of a Kubernetes object.

Usage:
  %[1]s <resource> [name] [kubectl get flags...]

It runs "kubectl get" for you, so every flag that command takes still works:
-n, --context, -l, --all-namespaces and the rest are passed straight through.
The object is printed as YAML with each line's background set by the manager
that owns that field, according to metadata.managedFields.

Flags:
  --color=auto|always|never  Colour the output. auto (the default) colours only
                             when stdout is a terminal and NO_COLOR is unset.
  --no-color                 Same as --color=never.
  --no-legend                Drop the manager list printed above each object.
  --annotate                 Append a "# manager" comment wherever ownership
                             changes. On by default when colour is off, since
                             it is then the only thing carrying the ownership.
  --no-annotate              Never append those comments.
  -m, --manager <name>       Highlight only this manager and dim the rest.
                             Repeatable; matches exactly or as a substring.
  --show-managed-fields      Keep the managedFields block in the output.
  --version                  Print the version.
  -h, --help                 This text.

Examples:
  %[1]s deployment argocd-server -n argocd
  %[1]s cm/my-config -n default -m helm
  %[1]s ingress -A --no-color > owners.yaml
`, invocation())
}
