// Package kubectl runs the real kubectl, so kulprit inherits every flag,
// kubeconfig rule and auth plugin it already has.
package kubectl

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
)

// Reported wraps a failure that kubectl has already explained on stderr, so the
// caller can exit with its status without printing a second message.
type Reported struct {
	Code int
}

func (e *Reported) Error() string { return fmt.Sprintf("kubectl exited with status %d", e.Code) }

// Binary is the kubectl to run. KUBECTL overrides it, which is handy when the
// plugin runs against a wrapper such as kubectl-oidc. Failing that it falls
// back to oc, so the plugin works on an OpenShift machine that has no kubectl
// installed; oc accepts the same "get -o json" and completes the same way.
var Binary = sync.OnceValue(func() string {
	if b := os.Getenv("KUBECTL"); b != "" {
		return b
	}
	for _, candidate := range []string{"kubectl", "oc"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate
		}
	}
	// Nothing found; name kubectl so the failure says so.
	return "kubectl"
})

// Get runs "kubectl get" with the caller's arguments and returns the JSON it
// wrote. managedFields are requested explicitly because kubectl has stripped
// them from its own output since 1.21, and they are the whole point here.
// Anything kubectl says on stderr is passed on to ours.
func Get(args []string) ([]byte, error) { return get(args, true) }

// GetQuiet is Get with kubectl's stderr swallowed, for callers that must not
// write to the terminal. Shell completion is one: a warning printed while the
// user is pressing Tab lands in the middle of their command line.
func GetQuiet(args []string) ([]byte, error) { return get(args, false) }

func get(args []string, passStderr bool) ([]byte, error) {
	full := append([]string{"get"}, args...)
	full = append(full, "--output=json", "--show-managed-fields")

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(Binary(), full...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if msg == "" {
			return nil, fmt.Errorf("running %s: %w", Binary(), err)
		}
		if passStderr {
			os.Stderr.WriteString(msg)
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, &Reported{Code: exit.ExitCode()}
		}
		return nil, &Reported{Code: 1}
	}
	if msg := stderr.String(); msg != "" && passStderr {
		os.Stderr.WriteString(msg)
	}
	return stdout.Bytes(), nil
}
