// Package gitsource checks out the repository an image is built from
// (environment.images[].git): a remote one, cloned once into a cache and
// fetched as git.pull says, or a local one, used as it is. A branch or ref
// is checked out in a temporary worktree, removed after the build, so
// neither the cache nor a developer's checkout changes.
package gitsource

import (
	"context"
	"fmt"
	"path/filepath"

	kelecosystem "github.com/intentd/kel/ecosystem"
	"github.com/intentd/tools/cluster"
)

// IsRemote reports whether url names a remote repository (https://…,
// git@host:org/repo.git); anything else is a local path.
func IsRemote(url string) bool { return kelecosystem.IsValidGitRepository(url) }

// LocalPath is a local git.url resolved from dir, the directory of the file
// that declares it.
func LocalPath(dir, url string) string {
	if filepath.IsAbs(url) {
		return url
	}
	return filepath.Join(dir, url)
}

// Checkout returns the directory to build g from, and what removes what it
// made. dir is the directory of the file that declares g, for a local path.
func Checkout(ctx context.Context, g cluster.Git, dir string) (string, func(), error) {
	if IsRemote(g.URL) {
		return remote(ctx, g)
	}
	return local(ctx, g, LocalPath(dir, g.URL))
}

// local is a local repository: its working tree as it is, uncommitted
// changes included, or a worktree at the branch or ref.
func local(ctx context.Context, g cluster.Git, repo string) (string, func(), error) {
	if _, err := git(ctx, repo, "rev-parse", "--git-dir"); err != nil {
		return "", nil, fmt.Errorf("git.url %s: not a git repository: %w", repo, err)
	}
	rev := g.Ref
	if rev == "" {
		rev = g.Branch
	}
	if rev == "" {
		return repo, func() {}, nil
	}
	return worktree(ctx, repo, rev)
}
