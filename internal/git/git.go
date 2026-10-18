package git

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

var ErrNotRepo = errors.New("not in a git repository")

// Repo is a git working tree rooted at (or containing) Dir.
type Repo struct{ Dir string }

func (r Repo) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			return "", errors.New(msg)
		}
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

func (r Repo) IsRepo() bool {
	out, err := r.run("rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

func (r Repo) CurrentBranch() (string, error) {
	if !r.IsRepo() {
		return "", ErrNotRepo
	}
	return r.run("rev-parse", "--abbrev-ref", "HEAD")
}

func (r Repo) BranchExists(name string) bool {
	_, err := r.run("rev-parse", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

// CheckoutCmd returns the command that switches to branch, creating it when
// it does not exist yet.
func (r Repo) CheckoutCmd(branch string) (*exec.Cmd, error) {
	if !r.IsRepo() {
		return nil, ErrNotRepo
	}
	args := []string{"checkout", branch}
	if !r.BranchExists(branch) {
		args = []string{"checkout", "-b", branch}
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	return cmd, nil
}
