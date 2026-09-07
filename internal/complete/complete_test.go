package complete

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestKubectlArgsDropsOurFlags(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"nothing of ours", []string{"deploy", "-n", "argocd"}, []string{"deploy", "-n", "argocd"}},
		{"separate value", []string{"-m", "helm", "deploy"}, []string{"deploy"}},
		{"attached value", []string{"--manager=helm", "deploy"}, []string{"deploy"}},
		{"short flag", []string{"deploy", "--color", "never"}, []string{"deploy"}},
		{"attached color", []string{"deploy", "--color=never"}, []string{"deploy"}},
		{"valueless flags", []string{"--no-legend", "deploy", "--no-color"}, []string{"deploy"}},
		{"kubectl flag that looks like a value", []string{"-m", "helm", "-n", "argocd", "cm"}, []string{"-n", "argocd", "cm"}},
		{"trailing flag with no value yet", []string{"deploy", "-m"}, []string{"deploy"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := kubectlArgs(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("kubectlArgs(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestValueWanted(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string // flag name, or "" for none
	}{
		{"empty", nil, ""},
		{"after a value-taking flag", []string{"deploy", "-m"}, "--manager"},
		{"after its long form", []string{"deploy", "--manager"}, "--manager"},
		{"after --color", []string{"--color"}, "--color"},
		{"value already attached", []string{"--color=auto"}, ""},
		{"after a flag with no value", []string{"--no-legend"}, ""},
		{"after a kubectl flag", []string{"-n"}, ""},
		{"after a plain word", []string{"deploy"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ""
			if f := valueWanted(tt.in); f != nil {
				got = f.Name
			}
			if got != tt.want {
				t.Errorf("valueWanted(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEmitProtocol(t *testing.T) {
	var b strings.Builder
	if err := emit(&b, []string{"one\tfirst", "two"}, directiveNoFileComp); err != nil {
		t.Fatal(err)
	}
	want := "one\tfirst\ntwo\n:4\n"
	if b.String() != want {
		t.Errorf("emit() = %q, want %q", b.String(), want)
	}

	b.Reset()
	if err := emit(&b, nil, directiveDefault); err != nil {
		t.Fatal(err)
	}
	if b.String() != ":0\n" {
		t.Errorf("emit() with no candidates = %q, want %q", b.String(), ":0\n")
	}
}

func TestObjectsUnwrapsALists(t *testing.T) {
	single := map[string]any{"kind": "Deployment"}
	if got := objects(single); len(got) != 1 {
		t.Errorf("objects(single) returned %d, want 1", len(got))
	}
	list := map[string]any{
		"kind":  "DeploymentList",
		"items": []any{map[string]any{"kind": "Deployment"}, map[string]any{"kind": "Deployment"}},
	}
	if got := objects(list); len(got) != 2 {
		t.Errorf("objects(list) returned %d, want 2", len(got))
	}
	if got := objects("not an object"); got != nil {
		t.Errorf("objects(string) = %v, want nil", got)
	}
}

func TestFlagsHaveDescriptions(t *testing.T) {
	for _, f := range Flags {
		if !strings.HasPrefix(f.Name, "--") {
			t.Errorf("flag %q should be in long form", f.Name)
		}
		if f.Description == "" {
			t.Errorf("flag %s has no description; the shell shows it to the user", f.Name)
		}
		if f.Short != "" && !strings.HasPrefix(f.Short, "-") {
			t.Errorf("flag %s has a malformed short form %q", f.Name, f.Short)
		}
	}
}

func TestInstallWritesAHelperKubectlCanRun(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "kubectl-kulprit")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	path, kind, err := Install(dir, binary)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if want := filepath.Join(dir, HelperName); path != want {
		t.Errorf("installed at %s, want %s", path, want)
	}
	if kind != "symlink" && kind != "script" {
		t.Errorf("kind = %q, want symlink or script", kind)
	}

	// Whatever form it took, kubectl has to find it as an executable file.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("%s is not executable (mode %v)", path, info.Mode())
	}

	// Installing again must replace it rather than fail, which is what
	// happens on every upgrade.
	if _, _, err := Install(dir, binary); err != nil {
		t.Fatalf("second Install: %v", err)
	}
}

func TestHelperScriptIsRunnableShell(t *testing.T) {
	s := HelperScript()
	if !strings.HasPrefix(s, "#!") {
		t.Error("helper has no shebang, so it cannot be exec'd")
	}
	if !strings.Contains(s, "__complete") {
		t.Error("helper does not call __complete")
	}
	if !strings.HasSuffix(s, "\n") {
		t.Error("helper does not end in a newline")
	}
}

func TestOnPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", strings.Join([]string{"/nowhere", dir}, string(os.PathListSeparator)))
	if !OnPath(dir) {
		t.Errorf("OnPath(%s) = false, want true", dir)
	}
	if OnPath("/definitely/not/on/path") {
		t.Error("OnPath reported a directory that is not on PATH")
	}
}

func TestNotesNameTheShellAndThePath(t *testing.T) {
	for _, shell := range append(Shells, "") {
		got := Notes(shell, "/bin/kubectl_complete-kulprit", "/bin", true)
		if !strings.Contains(got, "/bin/kubectl_complete-kulprit") {
			t.Errorf("Notes(%q) does not say where it installed", shell)
		}
		if strings.Contains(got, "not on PATH") {
			t.Errorf("Notes(%q) warned about PATH when the directory was on it", shell)
		}
	}
	if !strings.Contains(Notes("zsh", "/x", "/x", false), "not on PATH") {
		t.Error("Notes did not warn when the directory was off PATH")
	}
}
