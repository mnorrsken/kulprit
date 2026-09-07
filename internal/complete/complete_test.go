package complete

import (
	"os"
	"os/exec"
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

func TestHelperNameFollowsTheCLI(t *testing.T) {
	// kubectl builds the helper name from its own root command, not from the
	// plugin's filename, so oc looks for a different file even though it runs
	// the same kubectl-kulprit binary.
	tests := map[string]string{
		"kubectl": "kubectl_complete-kulprit",
		"oc":      "oc_complete-kulprit",
	}
	for cli, want := range tests {
		if got := HelperName(cli); got != want {
			t.Errorf("HelperName(%q) = %q, want %q", cli, got, want)
		}
		if !IsHelperName(want) {
			t.Errorf("IsHelperName(%q) = false, want true", want)
		}
	}
	for _, notOurs := range []string{"kubectl-kulprit", "_complete-kulprit", "kubectl_complete-other", ""} {
		if IsHelperName(notOurs) {
			t.Errorf("IsHelperName(%q) = true, want false", notOurs)
		}
	}
}

func TestDefaultCLIsAlwaysIncludesKubectl(t *testing.T) {
	clis := DefaultCLIs()
	if len(clis) == 0 || clis[0] != "kubectl" {
		t.Fatalf("DefaultCLIs() = %v, want kubectl first", clis)
	}
	for _, cli := range clis[1:] {
		if _, err := exec.LookPath(cli); err != nil {
			t.Errorf("DefaultCLIs() offered %q, which is not on PATH", cli)
		}
	}
}

func TestInstallWritesAHelperTheCLICanRun(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "kubectl-kulprit")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Installing for two CLIs has to leave two separate helpers behind.
	for _, cli := range []string{"kubectl", "oc"} {
		path, kind, err := Install(dir, binary, cli)
		if err != nil {
			t.Fatalf("Install(%s): %v", cli, err)
		}
		if want := filepath.Join(dir, HelperName(cli)); path != want {
			t.Errorf("installed at %s, want %s", path, want)
		}
		if kind != "symlink" && kind != "script" {
			t.Errorf("kind = %q, want symlink or script", kind)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s is not executable (mode %v)", path, info.Mode())
		}
		// Installing again must replace it rather than fail, which is what
		// happens on every upgrade.
		if _, _, err := Install(dir, binary, cli); err != nil {
			t.Fatalf("second Install(%s): %v", cli, err)
		}
	}
	for _, cli := range []string{"kubectl", "oc"} {
		if _, err := os.Lstat(filepath.Join(dir, HelperName(cli))); err != nil {
			t.Errorf("helper for %s is missing after installing both: %v", cli, err)
		}
	}
}

func TestHelperScriptIsRunnableShellForItsCLI(t *testing.T) {
	for _, cli := range []string{"kubectl", "oc"} {
		s := HelperScript(cli)
		if !strings.HasPrefix(s, "#!") {
			t.Errorf("%s helper has no shebang, so it cannot be exec'd", cli)
		}
		if !strings.Contains(s, "exec "+cli+" "+PluginName+" __complete") {
			t.Errorf("%s helper does not invoke %s: %q", cli, cli, s)
		}
		if !strings.HasSuffix(s, "\n") {
			t.Errorf("%s helper does not end in a newline", cli)
		}
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

func TestNotesWarnOnlyWhenOffPath(t *testing.T) {
	for _, shell := range append(Shells, "") {
		if got := Notes(shell, []string{"kubectl"}, "/bin", true); strings.Contains(got, "not on PATH") {
			t.Errorf("Notes(%q) warned about PATH when the directory was on it", shell)
		}
	}
	if !strings.Contains(Notes("zsh", []string{"kubectl"}, "/x", false), "not on PATH") {
		t.Error("Notes did not warn when the directory was off PATH")
	}
	// The example should be written for the CLI actually being set up.
	if got := Notes("zsh", []string{"oc"}, "/x", true); !strings.Contains(got, "oc kulprit") {
		t.Errorf("Notes for oc does not mention \"oc kulprit\": %q", got)
	}
}
