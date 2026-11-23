package gitsource

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/intentd/tools/command"
)

// worktree checks rev out of repo into a temporary worktree, detached, and
// returns it with what removes it.
func worktree(ctx context.Context, repo, rev string) (string, func(), error) {
	tmp, err := os.MkdirTemp("", "jig-git-*")
	if err != nil {
		return "", nil, err
	}
	dir := filepath.Join(tmp, "src")
	if _, err := git(ctx, repo, "worktree", "add", "--detach", dir, rev); err != nil {
		os.RemoveAll(tmp)
		return "", nil, fmt.Errorf("checking out %s in %s: %w", rev, repo, err)
	}
	return dir, func() {
		_, _ = git(context.Background(), repo, "worktree", "remove", "--force", dir)
		os.RemoveAll(tmp)
	}, nil
}

// git runs git in dir ("" is the current directory), with its error output
// in the error.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	r, err := command.Run(ctx, command.Args("git", args...))
	if err == nil {
		err = r.Err()
	}
	if err != nil {
		if msg := strings.TrimSpace(r.Stderr); msg != "" {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
		}
		return "", err
	}
	return r.Stdout, nil
}
