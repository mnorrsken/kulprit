// Package complete implements the completion protocol kubectl uses for its
// plugins. kubectl runs an executable named kubectl_complete-<plugin> with the
// words typed after the plugin name — flags included, and with the word
// currently being completed last, even when it is empty — and reads back one
// candidate per line, optionally "value<TAB>description", followed by a line
// holding ":" and a directive number.
//
// Almost everything a user types here is really a "kubectl get" argument, so
// most of the work is handing the line to "kubectl __complete get" and passing
// its answer back. kulprit's own flags are stripped out first, because kubectl
// would not know what to make of them.
package complete

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mnorrsken/kulprit/internal/fieldpath"
	"github.com/mnorrsken/kulprit/internal/kubectl"
)

// Cobra's ShellCompDirective values, the two we need. NoFileComp stops the
// shell falling back to filenames when we have nothing to offer.
const (
	directiveDefault    = 0
	directiveNoFileComp = 4
)

// Flag is one of kulprit's own flags, as offered to the shell.
type Flag struct {
	Name        string
	Short       string
	TakesValue  bool
	Values      []string // the complete set of values, when there is one
	Description string
}

// Flags are the flags kulprit handles itself; everything else on the command
// line belongs to kubectl. Undocumented aliases are left out on purpose: parse
// still accepts them, but there is no reason to suggest them.
var Flags = []Flag{
	{Name: "--color", TakesValue: true, Values: []string{"auto", "always", "never"},
		Description: "When to colour the output"},
	{Name: "--no-color", Description: "Never colour the output"},
	{Name: "--no-legend", Description: "Drop the manager list above each object"},
	{Name: "--annotate", Description: "Comment every change of ownership"},
	{Name: "--no-annotate", Description: "Never comment ownership"},
	{Name: "--manager", Short: "-m", TakesValue: true,
		Description: "Highlight only this manager, dim the rest"},
	{Name: "--show-managed-fields", Description: "Keep the managedFields block in the output"},
	{Name: "--version", Description: "Print the version"},
	{Name: "--help", Short: "-h", Description: "Show usage"},
}

// Run writes the candidates for args, which is every word typed after the
// plugin name with the word being completed last.
func Run(w io.Writer, args []string) error {
	toComplete := ""
	if n := len(args); n > 0 {
		toComplete, args = args[n-1], args[:n-1]
	}

	switch f := valueWanted(args); {
	case f != nil && len(f.Values) > 0:
		return emit(w, describe(withPrefix(f.Values, toComplete), ""), directiveNoFileComp)
	case f != nil && f.Name == "--manager":
		return emit(w, describe(withPrefix(managers(args), toComplete), "field manager"), directiveNoFileComp)
	case f != nil:
		// A value we cannot guess; let the shell do nothing rather than
		// offer filenames.
		return emit(w, nil, directiveNoFileComp)
	case strings.HasPrefix(toComplete, "-"):
		return emit(w, flagCandidates(args, toComplete), directiveNoFileComp)
	}

	lines, directive := delegate(append(kubectlArgs(args), toComplete))
	return emit(w, lines, directive)
}

// valueWanted reports the flag whose value is being typed, which is the case
// when the last complete word is one of ours and takes a value.
func valueWanted(args []string) *Flag {
	if len(args) == 0 {
		return nil
	}
	last := args[len(args)-1]
	if strings.Contains(last, "=") {
		return nil // the value is already attached to the flag
	}
	f := lookup(last)
	if f == nil || !f.TakesValue {
		return nil
	}
	return f
}

func lookup(name string) *Flag {
	for i := range Flags {
		if Flags[i].Name == name || (Flags[i].Short != "" && Flags[i].Short == name) {
			return &Flags[i]
		}
	}
	return nil
}

// kubectlArgs drops kulprit's own flags, and the values that go with them, so
// what is left is a command line kubectl understands.
func kubectlArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		name, _, attached := strings.Cut(args[i], "=")
		f := lookup(name)
		if f == nil {
			out = append(out, args[i])
			continue
		}
		if f.TakesValue && !attached {
			i++
		}
	}
	return out
}

// flagCandidates offers kulprit's flags and kubectl get's, minus the output
// flag, which kulprit sets itself and refuses to have overridden.
func flagCandidates(args []string, toComplete string) []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range Flags {
		for _, name := range []string{f.Name, f.Short} {
			if name == "" || !strings.HasPrefix(name, toComplete) {
				continue
			}
			seen[name] = true
			out = append(out, name+"\t"+f.Description)
		}
	}
	inherited, _ := delegate(append(kubectlArgs(args), toComplete))
	for _, l := range inherited {
		name, _, _ := strings.Cut(l, "\t")
		if name == "-o" || name == "--output" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, l)
	}
	return out
}

// managers lists the field managers of whatever the command line already names,
// which is what --manager is about to be given. It stays silent on any error:
// the user is mid-keystroke and a half-typed command line failing to resolve is
// the normal case, not something to report.
func managers(args []string) []string {
	ka := kubectlArgs(args)
	if len(ka) == 0 {
		return nil
	}
	data, err := kubectl.GetQuiet(ka)
	if err != nil {
		return nil
	}
	doc, err := fieldpath.Decode(data)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, obj := range objects(doc) {
		meta, _ := obj["metadata"].(map[string]any)
		entries, _ := meta["managedFields"].([]any)
		for _, e := range entries {
			mf, ok := e.(map[string]any)
			if !ok {
				continue
			}
			if name, _ := mf["manager"].(string); name != "" {
				seen[name] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func objects(doc any) []map[string]any {
	obj, ok := doc.(map[string]any)
	if !ok {
		return nil
	}
	kind, _ := obj["kind"].(string)
	items, isList := obj["items"].([]any)
	if !strings.HasSuffix(kind, "List") || !isList {
		return []map[string]any{obj}
	}
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// delegate asks kubectl to complete "kubectl get" with these arguments and
// returns its candidates and directive.
func delegate(args []string) ([]string, int) {
	full := append([]string{"__complete", "get"}, args...)
	var stdout bytes.Buffer
	cmd := exec.Command(kubectl.Binary(), full...)
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, directiveNoFileComp
	}

	var lines []string
	directive := directiveDefault
	sc := bufio.NewScanner(&stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "" || strings.HasPrefix(line, "[Debug]"):
		case strings.HasPrefix(line, ":"):
			if d, err := strconv.Atoi(strings.TrimSpace(line[1:])); err == nil {
				directive = d
			}
		default:
			lines = append(lines, line)
		}
	}
	return lines, directive
}

func withPrefix(values []string, prefix string) []string {
	var out []string
	for _, v := range values {
		if strings.HasPrefix(v, prefix) {
			out = append(out, v)
		}
	}
	return out
}

func describe(values []string, description string) []string {
	if description == "" {
		return values
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = v + "\t" + description
	}
	return out
}

func emit(w io.Writer, lines []string, directive int) error {
	for _, l := range lines {
		if _, err := fmt.Fprintln(w, l); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, ":%d\n", directive)
	return err
}

// HelperName is the executable kubectl runs to complete "kubectl kulprit". The
// lookup happens inside the kubectl process, so it has to be a real file on
// PATH: a shell function is invisible to it, whatever the shell exports.
const HelperName = "kubectl_complete-kulprit"

// helperScript is the helper in the form every other plugin ships it, used
// where a symlink cannot be made and by --print for packagers.
const helperScript = `#!/usr/bin/env sh
# kubectl runs this when completing "kubectl kulprit ...". Hand the words it
# passes to the plugin's own completion.
exec kubectl kulprit __complete "$@"
`

// HelperScript returns the shell form of the helper.
func HelperScript() string { return helperScript }

// Install puts the completion helper in dir. It prefers a symlink to the
// plugin binary, which answers to the helper name directly and saves running a
// shell and a second kubectl, and falls back to the script where symlinks are
// not available. It reports the path written and which of the two it used.
func Install(dir, binary string) (path, kind string, err error) {
	path = filepath.Join(dir, HelperName)

	// A file under this name is ours by definition, so replacing it is safe;
	// it has to go first either way, since a symlink cannot be overwritten.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", "", fmt.Errorf("replacing %s: %w", path, err)
	}

	if err := os.Symlink(binary, path); err == nil {
		return path, "symlink", nil
	}
	if err := os.WriteFile(path, []byte(helperScript), 0o755); err != nil {
		return "", "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, "script", nil
}

// Notes is what the user still has to do for the shell they named, written for
// a person rather than for eval. Callers put it on stderr, so that sourcing
// this command by habit does nothing rather than something surprising.
func Notes(shell, path, dir string, onPath bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "installed %s\n", path)
	if !onPath {
		fmt.Fprintf(&b, "\nwarning: %s is not on PATH, so kubectl will not find it.\n", dir)
	}
	switch shell {
	case "bash":
		b.WriteString("\nbash also needs kubectl's own completion loaded:\n" +
			"    source <(kubectl completion bash)\n" +
			"and bash-completion v2 on bash 4.4 or newer; the bash 3.2 that ships\n" +
			"with macOS is not enough.\n")
	case "zsh":
		b.WriteString("\nzsh also needs kubectl's own completion loaded:\n" +
			"    source <(kubectl completion zsh)\n")
	case "fish":
		b.WriteString("\nfish also needs kubectl's own completion loaded:\n" +
			"    kubectl completion fish | source\n")
	case "":
		b.WriteString("\nkubectl's own completion has to be loaded for your shell; see\n" +
			"    kubectl completion --help\n")
	}
	b.WriteString("\nThen \"kubectl kulprit <TAB>\" completes resources, names, flags, and\n" +
		"the field managers of the object for -m.\n")
	return b.String()
}

// Shells are the shells Notes knows about.
var Shells = []string{"bash", "zsh", "fish"}

// OnPath reports whether dir is one of the entries in PATH.
func OnPath(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p == "" {
			continue
		}
		if q, err := filepath.Abs(p); err == nil && q == abs {
			return true
		}
	}
	return false
}
