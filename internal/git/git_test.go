package git

import (
	"os/exec"
	"testing"
)

func TestCheckout(t *testing.T) {
	r := Repo{Dir: t.TempDir()}
	if r.IsRepo() {
		t.Skip("temp dir is inside a git repo")
	}
	if _, err := r.CheckoutCmd("x"); err != ErrNotRepo {
		t.Fatalf("err = %v", err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=a@b", "-c", "user.name=a", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = r.Dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	cmd, err := r.CheckoutCmd("eng-1-thing")
	if err != nil || cmd.Args[2] != "-b" {
		t.Fatalf("new branch cmd = %v, %v", cmd, err)
	}
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if b, _ := r.CurrentBranch(); b != "eng-1-thing" {
		t.Fatalf("branch = %q", b)
	}
	cmd, _ = r.CheckoutCmd("main")
	if cmd.Args[2] != "main" {
		t.Fatalf("existing branch cmd = %v", cmd.Args)
	}
}
