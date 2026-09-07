package fieldpath

import (
	"reflect"
	"testing"
)

// fields decodes a fieldsV1 literal the way the renderer receives it.
func fields(t *testing.T, s string) map[string]any {
	t.Helper()
	v, err := Decode([]byte(s))
	if err != nil {
		t.Fatalf("decoding fieldsV1: %v", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("fieldsV1 is %T, want an object", v)
	}
	return m
}

func object(t *testing.T, s string) any {
	t.Helper()
	v, err := Decode([]byte(s))
	if err != nil {
		t.Fatalf("decoding object: %v", err)
	}
	return v
}

// walk follows a slash-separated path through the object, moving the cursor in
// step, and returns the cursor at the end. A path step of the form "[n]" is a
// list index.
func walk(t *testing.T, obj any, cur Cursor, path []string) (any, Cursor) {
	t.Helper()
	for _, step := range path {
		if step == "" {
			continue
		}
		if step[0] == '[' {
			l, ok := obj.([]any)
			if !ok {
				t.Fatalf("step %q: value is %T, want a list", step, obj)
			}
			i := int(step[1] - '0')
			obj, cur = l[i], cur.Elem(i, l[i])
			continue
		}
		m, ok := obj.(map[string]any)
		if !ok {
			t.Fatalf("step %q: value is %T, want an object", step, obj)
		}
		obj, cur = m[step], cur.Child(step)
	}
	return obj, cur
}

func TestCursorOwners(t *testing.T) {
	const objJSON = `{
		"metadata": {"name": "x", "labels": {"app": "web"}, "uid": "abc"},
		"spec": {
			"replicas": 2,
			"selector": {"matchLabels": {"app": "web"}},
			"finalizers": ["a", "b"],
			"ports": [{"port": 80, "protocol": "TCP", "name": "http"},
			          {"port": 443, "protocol": "TCP", "name": "https"}],
			"tolerations": [{"key": "one"}, {"key": "two"}]
		}
	}`
	// helm (0) applies most of it; ctrl (1) shares one label and owns the
	// second port and the second toleration.
	helm := `{
		"f:metadata": {"f:labels": {".": {}, "f:app": {}}},
		"f:spec": {
			"f:selector": {},
			"f:finalizers": {"v:\"a\"": {}},
			"f:ports": {"k:{\"port\":80,\"protocol\":\"TCP\"}": {".": {}, "f:name": {}}},
			"f:tolerations": {"i:0": {".": {}, "f:key": {}}}
		}
	}`
	ctrl := `{
		"f:metadata": {"f:labels": {"f:app": {}}},
		"f:spec": {
			"f:replicas": {},
			"f:finalizers": {"v:\"b\"": {}},
			"f:ports": {"k:{\"port\":443,\"protocol\":\"TCP\"}": {".": {}, "f:name": {}}},
			"f:tolerations": {"i:1": {".": {}, "f:key": {}}}
		}
	}`

	set := NewSet()
	set.Add(0, fields(t, helm))
	set.Add(1, fields(t, ctrl))
	obj := object(t, objJSON)

	tests := []struct {
		path    string
		owners  []int
		display []int
	}{
		{"metadata", nil, nil},                  // container of owned and unowned
		{"metadata/name", nil, nil},             // set by the API server
		{"metadata/uid", nil, nil},              //
		{"metadata/labels", []int{0}, []int{0}}, // "." claimed by helm alone
		{"metadata/labels/app", []int{0, 1}, []int{0, 1}},
		{"spec", nil, nil},
		{"spec/replicas", []int{1}, []int{1}},
		{"spec/selector", []int{0}, []int{0}},                 // atomic
		{"spec/selector/matchLabels", []int{0}, []int{0}},     // inherited
		{"spec/selector/matchLabels/app", []int{0}, []int{0}}, // inherited further
		{"spec/finalizers", nil, nil},
		{"spec/finalizers/[0]", []int{0}, []int{0}}, // matched by value
		{"spec/finalizers/[1]", []int{1}, []int{1}},
		{"spec/ports", nil, nil},
		{"spec/ports/[0]", []int{0}, []int{0}}, // matched by key fields
		{"spec/ports/[0]/name", []int{0}, []int{0}},
		{"spec/ports/[0]/protocol", nil, nil}, // named in the key, not owned
		{"spec/ports/[1]", []int{1}, []int{1}},
		{"spec/ports/[1]/name", []int{1}, []int{1}},
		{"spec/tolerations/[0]", []int{0}, []int{0}}, // matched by index
		{"spec/tolerations/[1]", []int{1}, []int{1}},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			_, cur := walk(t, obj, set.Cursor(), split(tt.path))
			if got := cur.Owners(); !reflect.DeepEqual(got, tt.owners) {
				t.Errorf("Owners() = %v, want %v", got, tt.owners)
			}
			if got := cur.DisplayOwners(); !reflect.DeepEqual(got, tt.display) {
				t.Errorf("DisplayOwners() = %v, want %v", got, tt.display)
			}
		})
	}
}

// TestDisplayOwnersFallsBackToTheOnlyManagerBelow covers the container case: a
// field nobody claims, holding fields that all belong to one manager.
func TestDisplayOwnersFallsBackToTheOnlyManagerBelow(t *testing.T) {
	set := NewSet()
	set.Add(3, fields(t, `{"f:spec": {"f:template": {"f:spec": {"f:hostNetwork": {}}}}}`))
	obj := object(t, `{"spec": {"template": {"spec": {"hostNetwork": true}}}}`)

	_, cur := walk(t, obj, set.Cursor(), split("spec/template"))
	if got := cur.Owners(); got != nil {
		t.Errorf("Owners() = %v, want none", got)
	}
	if got, want := cur.DisplayOwners(), []int{3}; !reflect.DeepEqual(got, want) {
		t.Errorf("DisplayOwners() = %v, want %v", got, want)
	}
}

func TestElemMatchesNumbersAcrossFormats(t *testing.T) {
	set := NewSet()
	set.Add(0, fields(t, `{"f:ports": {"k:{\"port\":80}": {"f:name": {}}}}`))
	obj := object(t, `{"ports": [{"port": 80.0, "name": "http"}]}`)

	_, cur := walk(t, obj, set.Cursor(), split("ports/[0]/name"))
	if got, want := cur.Owners(), []int{0}; !reflect.DeepEqual(got, want) {
		t.Errorf("Owners() = %v, want %v", got, want)
	}
}

func split(path string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == '/' {
			out = append(out, path[start:i])
			start = i + 1
		}
	}
	return out
}
