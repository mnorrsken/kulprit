package render

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mnorrsken/kulprit/internal/fieldpath"
)

// Options controls how an object is written.
type Options struct {
	// Color turns on the background colours. With it off the output is plain
	// YAML, and Annotate is the only thing that carries ownership.
	Color bool
	// Legend writes the manager list above each object.
	Legend bool
	// Annotate appends a "# manager" comment wherever ownership changes.
	// Fields with more than one owner always get one, since no single colour
	// can name them.
	Annotate bool
	// ShowManagedFields keeps metadata.managedFields in the output instead of
	// dropping it, the way kubectl's flag of the same name does.
	ShowManagedFields bool
	// Only, when set, highlights just these managers and dims everything else.
	Only []string
}

// entry is one managedFields record.
type entry struct {
	operation   string
	apiVersion  string
	subresource string
	time        string
}

func (e entry) String() string {
	parts := []string{e.operation, e.apiVersion}
	if e.subresource != "" {
		parts = append(parts, "("+e.subresource+")")
	}
	if e.time != "" {
		parts = append(parts, e.time)
	}
	return strings.Join(parts, " ")
}

type manager struct {
	name    string
	entries []entry
}

// Print writes doc, which is a decoded "kubectl get -o json" document, and
// returns how many objects it wrote. A List is unwrapped and its items are
// separated by a YAML document marker.
func Print(w io.Writer, doc any, opts Options) (int, error) {
	obj, ok := doc.(map[string]any)
	if !ok {
		return 0, fmt.Errorf("expected a JSON object at the top level, got %T", doc)
	}
	items, isList := listItems(obj)
	if !isList {
		return 1, printObject(w, obj, opts)
	}
	for i, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			return i, fmt.Errorf("list item %d is not an object", i)
		}
		if i > 0 {
			if _, err := fmt.Fprintln(w, "---"); err != nil {
				return i, err
			}
		}
		if err := printObject(w, m, opts); err != nil {
			return i, err
		}
	}
	return len(items), nil
}

func listItems(obj map[string]any) ([]any, bool) {
	kind, _ := obj["kind"].(string)
	if !strings.HasSuffix(kind, "List") {
		return nil, false
	}
	items, ok := obj["items"].([]any)
	return items, ok
}

func printObject(w io.Writer, obj map[string]any, opts Options) error {
	managers, set := ownership(obj)
	if !opts.ShowManagedFields {
		if meta, ok := obj["metadata"].(map[string]any); ok {
			delete(meta, "managedFields")
		}
	}

	only := selection(managers, opts.Only)
	if opts.Legend {
		if err := writeLegend(w, obj, managers, only, opts); err != nil {
			return err
		}
	}

	var prev []int
	first := true
	for _, line := range Emit(obj, set.Cursor()) {
		text := paintLine(line, managers, only, opts)
		if shouldAnnotate(line.Owners, prev, first, opts) {
			text += comment(names(managers, line.Owners), opts.Color)
		}
		if _, err := fmt.Fprintln(w, text); err != nil {
			return err
		}
		prev, first = line.Owners, false
	}
	return nil
}

// ownership reads metadata.managedFields into the manager list, in the order
// the API server reports them, and the merged ownership tree. Entries that name
// the same manager share one index, and therefore one colour.
func ownership(obj map[string]any) ([]manager, *fieldpath.Set) {
	set := fieldpath.NewSet()
	var managers []manager
	index := map[string]int{}

	meta, _ := obj["metadata"].(map[string]any)
	raw, _ := meta["managedFields"].([]any)
	for _, r := range raw {
		mf, ok := r.(map[string]any)
		if !ok {
			continue
		}
		name, _ := mf["manager"].(string)
		if name == "" {
			name = "(unnamed)"
		}
		i, seen := index[name]
		if !seen {
			i = len(managers)
			index[name] = i
			managers = append(managers, manager{name: name})
		}
		e := entry{}
		e.operation, _ = mf["operation"].(string)
		e.apiVersion, _ = mf["apiVersion"].(string)
		e.subresource, _ = mf["subresource"].(string)
		e.time, _ = mf["time"].(string)
		managers[i].entries = append(managers[i].entries, e)

		if fields, ok := mf["fieldsV1"].(map[string]any); ok {
			set.Add(i, fields)
		}
	}
	return managers, set
}

// selection turns the --only manager names into a set of indices. A nil result
// means no filter: everything is highlighted.
func selection(managers []manager, only []string) map[int]bool {
	if len(only) == 0 {
		return nil
	}
	sel := map[int]bool{}
	for i, m := range managers {
		for _, want := range only {
			if strings.EqualFold(m.name, want) || strings.Contains(m.name, want) {
				sel[i] = true
			}
		}
	}
	return sel
}

func selected(owners []int, only map[int]bool) bool {
	if only == nil {
		return len(owners) > 0
	}
	for _, o := range owners {
		if only[o] {
			return true
		}
	}
	return false
}

func paintLine(line Line, managers []manager, only map[int]bool, opts Options) string {
	if !opts.Color {
		return line.Text
	}
	if !selected(line.Owners, only) {
		if only != nil {
			return paint(line.Text, dim)
		}
		return line.Text
	}
	if len(line.Owners) > 1 {
		return paint(line.Text, bgCode(sharedColor))
	}
	return paint(line.Text, managerColor(line.Owners[0]))
}

func shouldAnnotate(owners, prev []int, first bool, opts Options) bool {
	if len(owners) == 0 {
		return false
	}
	if !opts.Annotate && !(opts.Color && len(owners) > 1) {
		return false
	}
	return first || !sameOwners(owners, prev)
}

func sameOwners(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func names(managers []manager, owners []int) string {
	out := make([]string, 0, len(owners))
	for _, o := range owners {
		if o < len(managers) {
			out = append(out, managers[o].name)
		}
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func comment(text string, color bool) string {
	s := "  # " + text
	if color {
		return dim + s + reset
	}
	return s
}

func writeLegend(w io.Writer, obj map[string]any, managers []manager, only map[int]bool, opts Options) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", describe(obj))
	if len(managers) == 0 {
		b.WriteString("# no managedFields: this object predates server-side apply or was written without it\n#\n")
		_, err := io.WriteString(w, b.String())
		return err
	}

	width := len("(shared)")
	for _, m := range managers {
		if len(m.name) > width {
			width = len(m.name)
		}
	}
	pad := strings.Repeat(" ", width)

	for i, m := range managers {
		mark := ""
		if opts.Color {
			mark = swatch(managerColor(i)) + " "
			if only != nil && !only[i] {
				mark = swatch("") + " "
			}
		}
		for j, e := range m.entries {
			name := pad
			if j == 0 {
				name = m.name + pad[len(m.name):]
			} else if opts.Color {
				mark = swatch("") + " "
			}
			fmt.Fprintf(&b, "# %s%s  %s\n", mark, name, e)
		}
	}
	if opts.Color {
		fmt.Fprintf(&b, "# %s %s  owned by more than one manager\n",
			swatch(bgCode(sharedColor)), "(shared)"+pad[len("(shared)"):])
		fmt.Fprintf(&b, "# %s %s  not tracked in managedFields\n",
			swatch(""), "(unowned)"+strings.Repeat(" ", max(0, width-len("(unowned)"))))
	}
	b.WriteString("#\n")
	_, err := io.WriteString(w, b.String())
	return err
}

// describe names the object the way kubectl would refer to it.
func describe(obj map[string]any) string {
	kind, _ := obj["kind"].(string)
	if kind == "" {
		kind = "object"
	}
	meta, _ := obj["metadata"].(map[string]any)
	name, _ := meta["name"].(string)
	out := strings.ToLower(kind)
	if name != "" {
		out += "/" + name
	}
	if ns, _ := meta["namespace"].(string); ns != "" {
		out += " -n " + ns
	}
	return out
}
