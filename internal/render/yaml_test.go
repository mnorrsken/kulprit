package render

import (
	"strings"
	"testing"

	"github.com/mnorrsken/kulprit/internal/fieldpath"
)

func TestFormatString(t *testing.T) {
	tests := []struct{ in, want string }{
		// Plain where plain is safe.
		{"nginx", "nginx"},
		{"nginx:1.25", "nginx:1.25"},
		{"app.kubernetes.io/name", "app.kubernetes.io/name"},
		{"8.8.8.8", "8.8.8.8"},
		{"1.25.0", "1.25.0"},
		{"25%", "25%"},
		{"web server", "web server"},
		{"a:b", "a:b"},

		// Quoted because YAML would read them back as something else.
		{"", `""`},
		{"24", `"24"`},
		{"0755", `"0755"`},
		{"1.5", `"1.5"`},
		{"1e3", `"1e3"`},
		{"true", `"true"`},
		{"False", `"False"`},
		{"yes", `"yes"`},
		{"n", `"n"`},
		{"off", `"off"`},
		{"null", `"null"`},
		{"~", `"~"`},
		{"=", `"="`},
		{".inf", `".inf"`},
		{"2026-02-25T20:27:03Z", `"2026-02-25T20:27:03Z"`},
		{"12:30", `"12:30"`},

		// Quoted because of structure characters or whitespace.
		{"-1", `"-1"`},
		{"- item", `"- item"`},
		{"#comment", `"#comment"`},
		{"*star", `"*star"`},
		{"{a}", `"{a}"`},
		{"key: value", `"key: value"`},
		{"trailing:", `"trailing:"`},
		{"a #b", `"a #b"`},
		{" padded", `" padded"`},
		{"padded ", `"padded "`},
		{"tab\there", `"tab\there"`},
		// A plain scalar processes no escapes: quotes and backslashes stay bare.
		{"quote\"and\\slash", "quote\"and\\slash"},
	}
	for _, tt := range tests {
		if got := formatString(tt.in); got != tt.want {
			t.Errorf("formatString(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestBlockScalar(t *testing.T) {
	tests := []struct {
		name string
		in   string
		head string
		body []string
		ok   bool
	}{
		{"strip", "one\ntwo", "|-", []string{"  one", "  two"}, true},
		{"clip", "one\ntwo\n", "|", []string{"  one", "  two"}, true},
		{"blank line kept bare", "one\n\ntwo", "|-", []string{"  one", "", "  two"}, true},
		{"single line is not a block", "one", "", nil, false},
		{"two trailing newlines", "one\n\n", "", nil, false},
		{"indented first line", "  one\ntwo", "", nil, false},
		{"trailing space", "one \ntwo", "", nil, false},
		{"carriage return", "one\r\ntwo", "", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			head, body, ok := blockScalar(tt.in, 2)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			if head != tt.head {
				t.Errorf("head = %q, want %q", head, tt.head)
			}
			if strings.Join(body, "\n") != strings.Join(tt.body, "\n") {
				t.Errorf("body = %q, want %q", body, tt.body)
			}
		})
	}
}

func TestEmit(t *testing.T) {
	obj, err := fieldpath.Decode([]byte(`{
		"kind": "Deployment",
		"metadata": {"name": "web", "labels": {"app": "web"}, "annotations": {}},
		"spec": {
			"replicas": 3,
			"containers": [{"image": "nginx:1.25", "name": "web", "ports": [80, 443]}],
			"finalizers": [],
			"script": "line one\nline two\n"
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	set := fieldpath.NewSet()
	set.Add(0, mustFields(t, `{"f:spec": {"f:replicas": {}}}`))
	set.Add(1, mustFields(t, `{"f:spec": {"f:containers": {"k:{\"name\":\"web\"}": {".": {}, "f:image": {}, "f:name": {}}}}}`))

	want := `kind: Deployment
metadata:
  name: web
  annotations: {}
  labels:
    app: web
spec:
  containers:
  - name: web
    image: nginx:1.25
    ports:
    - 80
    - 443
  finalizers: []
  replicas: 3
  script: |
    line one
    line two`

	var got []string
	owners := map[string][]int{}
	for _, l := range Emit(obj, set.Cursor()) {
		got = append(got, l.Text)
		owners[strings.TrimSpace(l.Text)] = l.Owners
	}
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("Emit() =\n%s\n\nwant\n%s", g, want)
	}

	for text, want := range map[string][]int{
		"replicas: 3":       {0},
		"- name: web":       {1},
		"image: nginx:1.25": {1},
		"kind: Deployment":  nil,
		"- 80":              nil,
		"containers:":       {1}, // the only manager below it
	} {
		got := owners[text]
		if len(got) != len(want) {
			t.Errorf("owners of %q = %v, want %v", text, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("owners of %q = %v, want %v", text, got, want)
				break
			}
		}
	}
}

func mustFields(t *testing.T, s string) map[string]any {
	t.Helper()
	v, err := fieldpath.Decode([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return v.(map[string]any)
}
