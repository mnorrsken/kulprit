// Package kubectl runs the real kubectl, so kulprit inherits every flag,
// kubeconfig rule and auth plugin it already has.
package kubectl

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// Reported wraps a failure that kubectl has already explained on stderr, so the
// caller can exit with its status without printing a second message.
type Reported struct {
	Code int
}

func (e *Reported) Error() string { return fmt.Sprintf("kubectl exited with status %d", e.Code) }

// Binary is the kubectl to run. KUBECTL overrides it, which is handy when the
// plugin runs against a wrapper such as kubectl-oidc.
func Binary() string {
	if b := os.Getenv("KUBECTL"); b != "" {
		return b
	}
	return "kubectl"
}

// Get runs "kubectl get" with the caller's arguments and returns the JSON it
// wrote. managedFields are requested explicitly because kubectl has stripped
// them from its own output since 1.21, and they are the whole point here.
func Get(args []string) ([]byte, error) {
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
		os.Stderr.WriteString(msg)
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, &Reported{Code: exit.ExitCode()}
		}
		return nil, &Reported{Code: 1}
	}
	if msg := stderr.String(); msg != "" {
		os.Stderr.WriteString(msg)
	}
	return stdout.Bytes(), nil
}
