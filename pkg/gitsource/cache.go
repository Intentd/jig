package gitsource

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/intentd/tools/cluster"
	"github.com/intentd/tools/fileio"
)

// CacheDir is where a remote repository is cloned:
// ~/.jig/cache/git/<host>/<org>/<repo>.
func CacheDir(url string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	host, path, err := hostPath(url)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".jig", "cache", "git", host, filepath.FromSlash(path)), nil
}

var (
	scpLike = regexp.MustCompile(`^[\w.-]+@([^:]+):(.+)$`)
	urlLike = regexp.MustCompile(`^[a-z+]+://(?:[^@/]+@)?([^/:]+)(?::\d+)?/(.+)$`)
)

// hostPath splits a remote URL into its host and repository path, without
// a trailing .git.
func hostPath(url string) (string, string, error) {
	m := urlLike.FindStringSubmatch(url)
	if m == nil {
		m = scpLike.FindStringSubmatch(url)
	}
	if m == nil {
		return "", "", fmt.Errorf("git.url %q: not a repository URL", url)
	}
	return m[1], strings.TrimSuffix(strings.TrimSuffix(m[2], "/"), ".git"), nil
}

// remote is a remote repository: cloned into its cache dir when missing,
// fetched unless pull is IfNotPresent and the clone is there, then checked
// out at the branch, ref or default branch in a worktree.
func remote(ctx context.Context, g cluster.Git) (string, func(), error) {
	cache, err := CacheDir(g.URL)
	if err != nil {
		return "", nil, err
	}
	if err := fileio.CheckLocal(filepath.Join(cache, ".git")); err != nil {
		if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
			return "", nil, err
		}
		if _, err := git(ctx, "", "clone", "--no-checkout", g.URL, cache); err != nil {
			return "", nil, fmt.Errorf("cloning %s: %w", g.URL, err)
		}
	} else if g.Pull != cluster.PullIfNotPresent {
		if _, err := git(ctx, cache, "fetch", "--prune", "--tags", "origin"); err != nil {
			return "", nil, fmt.Errorf("fetching %s: %w", g.URL, err)
		}
	}
	rev := "origin/HEAD"
	switch {
	case g.Ref != "":
		rev = g.Ref
	case g.Branch != "":
		rev = "origin/" + g.Branch
	}
	return worktree(ctx, cache, rev)
}
