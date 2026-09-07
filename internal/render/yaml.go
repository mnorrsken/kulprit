package render

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mnorrsken/kulprit/internal/fieldpath"
)

// Line is one rendered line of YAML together with the managers that own the
// field it shows. Owners is empty when nobody claims the field.
type Line struct {
	Text   string
	Owners []int
}

// Emit renders a decoded JSON value as YAML, tagging every line with the
// managers that own it. Keys are sorted alphabetically except that "name" and
// "namespace" come first, which keeps objects readable without needing the
// schema.
func Emit(v any, cur fieldpath.Cursor) []Line {
	e := &emitter{}
	switch t := v.(type) {
	case map[string]any:
		e.emitMap(t, cur, 0)
	case []any:
		e.emitList(t, cur, 0)
	default:
		e.emitScalar(0, "", v, cur.Owners())
	}
	return e.lines
}

type emitter struct {
	lines []Line
	// pending replaces the indentation of the next line, so a list item's
	// dash and the first key of its map land on the same line.
	pending string
}

func (e *emitter) push(indent int, text string, owners []int) {
	prefix := strings.Repeat(" ", indent)
	if e.pending != "" {
		prefix, e.pending = e.pending, ""
	}
	e.lines = append(e.lines, Line{Text: prefix + text, Owners: owners})
}

func (e *emitter) emitMap(m map[string]any, cur fieldpath.Cursor, indent int) {
	for _, k := range sortedKeys(m) {
		e.emitEntry(indent, formatString(k)+":", m[k], cur.Child(k))
	}
}

func (e *emitter) emitList(l []any, cur fieldpath.Cursor, indent int) {
	for i, v := range l {
		e.emitItem(indent, v, cur.Elem(i, v))
	}
}

// emitEntry writes "<label> <value>", where label is a map key with its colon
// or a bare "-" for a list element.
func (e *emitter) emitEntry(indent int, label string, v any, cur fieldpath.Cursor) {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 {
			e.push(indent, label+" {}", cur.Owners())
			return
		}
		e.push(indent, label, cur.DisplayOwners())
		e.emitMap(t, cur, indent+2)
	case []any:
		if len(t) == 0 {
			e.push(indent, label+" []", cur.Owners())
			return
		}
		e.push(indent, label, cur.DisplayOwners())
		e.emitList(t, cur, indent)
	default:
		e.emitScalar(indent, label, v, cur.Owners())
	}
}

func (e *emitter) emitItem(indent int, v any, cur fieldpath.Cursor) {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 {
			e.push(indent, "- {}", cur.Owners())
			return
		}
		e.pending = strings.Repeat(" ", indent) + "- "
		e.emitMap(t, cur, indent+2)
		e.pending = ""
	case []any:
		if len(t) == 0 {
			e.push(indent, "- []", cur.Owners())
			return
		}
		e.pending = strings.Repeat(" ", indent) + "- "
		e.emitList(t, cur, indent+2)
		e.pending = ""
	default:
		e.emitScalar(indent, "-", v, cur.Owners())
	}
}

func (e *emitter) emitScalar(indent int, label string, v any, owners []int) {
	if s, ok := v.(string); ok {
		if head, body, ok := blockScalar(s, indent+2); ok {
			e.push(indent, join(label, head), owners)
			for _, l := range body {
				e.push(0, l, owners)
			}
			return
		}
	}
	e.push(indent, join(label, formatScalar(v)), owners)
}

func join(label, value string) string {
	if label == "" {
		return value
	}
	return label + " " + value
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if ri, rj := keyRank(keys[i]), keyRank(keys[j]); ri != rj {
			return ri < rj
		}
		return keys[i] < keys[j]
	})
	return keys
}

func keyRank(k string) int {
	switch k {
	case "name":
		return 0
	case "namespace":
		return 1
	}
	return 2
}

func formatScalar(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(t)
	case json.Number:
		return t.String()
	case string:
		return formatString(t)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	}
	return formatString(fmt.Sprint(v))
}

func formatString(s string) string {
	if needsQuote(s) {
		return doubleQuote(s)
	}
	return s
}

// reResolved matches the strings YAML 1.1 would read back as something other
// than a string, so they have to be quoted to survive a round trip.
var reResolved = regexp.MustCompile(`(?i)^(` +
	`y|n|yes|no|true|false|on|off|null|~|=` +
	`|[-+]?(0b[01_]+|0x[0-9a-f_]+|0o[0-7_]+|[0-9][0-9_]*)` +
	`|[-+]?(\.[0-9][0-9_]*|[0-9][0-9_]*\.[0-9_]*|[0-9][0-9_]*)(e[-+]?[0-9]+)?` +
	`|[-+]?\.(inf|nan)` +
	`|[0-9]{4}-[0-9]{1,2}-[0-9]{1,2}([t ].*)?` +
	`|[-+]?[0-9][0-9_]*(:[0-5]?[0-9])+(\.[0-9_]*)?` +
	`)$`)

func needsQuote(s string) bool {
	if s == "" || s != strings.TrimSpace(s) {
		return true
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	switch s[0] {
	case '-', '?', ':', ',', '[', ']', '{', '}', '#', '&', '*', '!', '|', '>', '\'', '"', '%', '@', '`':
		return true
	}
	if strings.Contains(s, ": ") || strings.HasSuffix(s, ":") || strings.Contains(s, " #") {
		return true
	}
	return reResolved.MatchString(s)
}

func doubleQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\x%02x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// blockScalar renders a multi-line string as a literal block, which is what
// makes embedded config files and certificates readable. It gives up, leaving
// the caller to quote the string, whenever a block would not round trip:
// control characters, trailing whitespace, a first line that starts indented,
// or more than one trailing newline.
func blockScalar(s string, indent int) (head string, body []string, ok bool) {
	if !strings.Contains(s, "\n") {
		return "", nil, false
	}
	for _, r := range s {
		if (r < 0x20 && r != '\n') || r == 0x7f {
			return "", nil, false
		}
	}
	content := strings.TrimRight(s, "\n")
	switch len(s) - len(content) {
	case 0:
		head = "|-"
	case 1:
		head = "|"
	default:
		return "", nil, false
	}
	if content == "" {
		return "", nil, false
	}
	lines := strings.Split(content, "\n")
	if strings.HasPrefix(lines[0], " ") || strings.HasPrefix(lines[0], "\t") {
		return "", nil, false
	}
	pad := strings.Repeat(" ", indent)
	body = make([]string, len(lines))
	for i, l := range lines {
		if l != strings.TrimRight(l, " \t") {
			return "", nil, false
		}
		if l == "" {
			body[i] = ""
			continue
		}
		body[i] = pad + l
	}
	return head, body, true
}
