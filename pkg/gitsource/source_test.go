package gitsource

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/intentd/tools/cluster"
	"github.com/intentd/tools/fileio"
	"github.com/intentd/tools/structured/structuredtest"
)

// A remote repository is cached by host and path, without .git, whatever
// form its URL takes.
func TestCacheDir(t *testing.T) {
	t.Setenv("HOME", "/home/dev")
	for url, want := range map[string]string{
		"https://github.com/acme/auth.git":    "/home/dev/.jig/cache/git/github.com/acme/auth",
		"https://github.com/acme/auth":        "/home/dev/.jig/cache/git/github.com/acme/auth",
		"git@github.com:acme/auth.git":        "/home/dev/.jig/cache/git/github.com/acme/auth",
		"ssh://git@gitlab.io:22/team/api.git": "/home/dev/.jig/cache/git/gitlab.io/team/api",
	} {
		got, err := CacheDir(url)
		require.NoError(t, err, url)
		assert.Equal(t, filepath.FromSlash(want), got, url)
	}
	assert.True(t, IsRemote("git@github.com:acme/auth.git"))
	assert.False(t, IsRemote("../auth"))
	assert.Equal(t, filepath.Join("/repos", "auth"), LocalPath("/repos/shop", "../auth"))
}

// A local repository builds from its working tree as it is, uncommitted
// changes included; at a branch, from a worktree that goes with cleanup,
// the developer's checkout untouched.
func TestCheckout_Local(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ctx := context.Background()
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	run("init", "-q", "-b", "main")
	structuredtest.WriteFile(t, filepath.Join(repo, "Dockerfile"), "FROM scratch\n")
	run("add", ".")
	run("commit", "-q", "-m", "one")
	run("branch", "release")
	structuredtest.WriteFile(t, filepath.Join(repo, "UNCOMMITTED"), "x\n")

	dir, cleanup, err := Checkout(ctx, cluster.Git{URL: repo}, "")
	require.NoError(t, err)
	assert.Equal(t, repo, dir, "the working tree as it is")
	assert.NoError(t, fileio.CheckLocal(filepath.Join(dir, "UNCOMMITTED")))
	cleanup()

	dir, cleanup, err = Checkout(ctx, cluster.Git{URL: repo, Branch: "release"}, "")
	require.NoError(t, err)
	assert.NotEqual(t, repo, dir)
	assert.NoError(t, fileio.CheckLocal(filepath.Join(dir, "Dockerfile")))
	assert.Error(t, fileio.CheckLocal(filepath.Join(dir, "UNCOMMITTED")), "a worktree has only what was committed")
	cleanup()
	assert.Error(t, fileio.CheckLocal(dir), "the worktree goes with cleanup")
	assert.NoError(t, fileio.CheckLocal(filepath.Join(repo, "UNCOMMITTED")), "the developer's checkout is untouched")

	_, _, err = Checkout(ctx, cluster.Git{URL: t.TempDir()}, "")
	assert.ErrorContains(t, err, "not a git repository")
}
