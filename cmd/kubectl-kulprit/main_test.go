package main

import (
	"os"
	"testing"

	"github.com/mnorrsken/kulprit/internal/complete"
)

// TestParseHandlesEveryCompletableFlag keeps the two lists of kulprit's flags
// from drifting apart: anything the shell is told about has to be a flag parse
// actually consumes, or completing it would hand kubectl an argument it does
// not know.
func TestParseHandlesEveryCompletableFlag(t *testing.T) {
	quiet(t)
	for _, f := range complete.Flags {
		for _, name := range []string{f.Name, f.Short} {
			if name == "" {
				continue
			}
			t.Run(name, func(t *testing.T) {
				argv := []string{name}
				if f.TakesValue {
					// Use a value the shell would really offer: a flag with a
					// closed set only consumes one of its own values.
					value := "a-value"
					if len(f.Values) > 0 {
						value = f.Values[0]
					}
					argv = append(argv, value)
				}
				argv = append(argv, "deploy")

				opts, _, err := parse(argv)
				if err != nil {
					t.Fatalf("parse(%q) failed: %v", argv, err)
				}
				for _, got := range opts.args {
					if got != "deploy" {
						t.Errorf("parse(%q) passed %q through to kubectl; it should have consumed it",
							argv, got)
					}
				}
			})
		}
	}
}

// TestParseRejectsOutput guards the one kubectl flag kulprit cannot allow,
// since it sets the output format itself.
func TestParseRejectsOutput(t *testing.T) {
	quiet(t)
	for _, name := range []string{"-o", "--output"} {
		if _, _, err := parse([]string{name, "yaml", "deploy"}); err == nil {
			t.Errorf("parse(%s) returned no error, want one", name)
		}
	}
}

// quiet points os.Stdout at /dev/null for the test, so --help and --version do
// not print into the test output.
func quiet(t *testing.T) {
	t.Helper()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = devnull
	t.Cleanup(func() {
		os.Stdout = old
		devnull.Close()
	})
}
