// Command kubectl-kulprit prints a Kubernetes object as YAML, colouring every
// field by the manager that owns it according to metadata.managedFields.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mnorrsken/kulprit/internal/complete"
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

// completionHelperName is the executable name kubectl runs to complete
// "kubectl kulprit". Installing the binary under this name as well is all the
// setup completion needs.
const completionHelperName = "kubectl_complete-kulprit"

func isCompletionHelper() bool {
	return filepath.Base(os.Args[0]) == completionHelperName
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
	// kubectl's completion reaches us through kubectl_complete-kulprit, which
	// calls "kubectl kulprit __complete <words>". It is not a user-facing
	// subcommand, so it is handled before the flags are looked at.
	if isCompletionHelper() {
		argv = append([]string{"__complete"}, argv...)
	}
	if len(argv) > 0 && argv[0] == "__complete" {
		out := bufio.NewWriter(os.Stdout)
		err := complete.Run(out, argv[1:])
		if ferr := out.Flush(); err == nil {
			err = ferr
		}
		return err
	}

	if len(argv) > 0 && argv[0] == "completion" {
		return installCompletion(argv[1:])
	}

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
			// Both "--color=never" and "--color never" work. A bare --color
			// means always, so the next word is only taken as the value when
			// it is one, and "--color deploy" still names a resource.
			v := "always"
			switch {
			case hasValue:
				v = value
			case i+1 < len(argv) && isColorMode(argv[i+1]):
				i++
				v = argv[i]
			}
			if !isColorMode(v) {
				return opts, false, fmt.Errorf("--color must be auto, always or never, got %q", v)
			}
			opts.color = v
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

func isColorMode(s string) bool {
	return s == "auto" || s == "always" || s == "never"
}

// installCompletion puts the completion helper next to this binary. kubectl
// looks for it by name on PATH and runs it as a program, so it has to exist as
// a file; there is no way to register it from the shell.
//
// The report goes to stderr and stdout is left empty, so that reaching for the
// usual "source <(... completion zsh)" does nothing rather than trying to run
// the report as shell commands.
func installCompletion(argv []string) error {
	shell := ""
	print := false
	for _, arg := range argv {
		switch arg {
		case "--print":
			print = true
		case "bash", "zsh", "fish":
			shell = arg
		case "-h", "--help":
			usage(os.Stdout)
			return nil
		default:
			return fmt.Errorf("completion takes %s or --print, got %q",
				strings.Join(complete.Shells, ", "), arg)
		}
	}

	if print {
		_, err := io.WriteString(os.Stdout, complete.HelperScript())
		return err
	}

	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding this binary: %w", err)
	}
	dir := filepath.Dir(binary)
	path, kind, err := complete.Install(dir, binary)
	if err != nil {
		return err
	}
	if kind == "symlink" {
		fmt.Fprintf(os.Stderr, "%s -> %s\n", complete.HelperName, filepath.Base(binary))
	}
	_, err = io.WriteString(os.Stderr, complete.Notes(shell, path, dir, complete.OnPath(dir)))
	return err
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
  --color auto|always|never  Colour the output. auto (the default) colours only
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

Shell completion:
  %[1]s completion [bash|zsh|fish]
                             Install the helper kubectl needs to complete this
                             plugin, next to this binary. Name your shell to be
                             told what else it wants. --print writes the helper
                             to stdout instead, for packagers.

Examples:
  %[1]s deployment argocd-server -n argocd
  %[1]s cm/my-config -n default -m helm
  %[1]s ingress -A --no-color > owners.yaml
`, invocation())
}
