package runner

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/cluster"
	"github.com/intentd/tools/command"
)

// ensureTools checks which external tools the jig's expectations need and
// installs any that are missing. Called once at runner start before assertions run.
func ensureTools(s *spec.Declaration) error {
	// git, when an image is built from a repository: before the images,
	// so ensureTools runs ahead of them too.
	if usesGit(s) {
		if err := ensureTool("git"); err != nil {
			return err
		}
	}
	needCurl, needJQ, needYQ, needMetrics := scanToolRequirements(s)
	if needCurl {
		if err := ensureTool("curl"); err != nil {
			return err
		}
	}
	if needJQ {
		if err := ensureTool("jq"); err != nil {
			return err
		}
	}
	if needYQ {
		if err := ensureTool("yq"); err != nil {
			return err
		}
	}
	if needMetrics {
		if err := cluster.EnsureMetricsServer(); err != nil {
			return err
		}
	}
	return nil
}

// usesGit reports whether an image is built from a repository.
func usesGit(s *spec.Declaration) bool {
	if s.Environment == nil {
		return false
	}
	for _, img := range s.Environment.Images {
		if img.Git != nil {
			return true
		}
	}
	return false
}

// scanToolRequirements walks the expectations and reports which tools they need.
func scanToolRequirements(s *spec.Declaration) (needCurl, needJQ, needYQ, needMetrics bool) {
	for _, exp := range s.Expect {
		for _, c := range exp.External {
			if c.JQ != "" {
				needJQ = true
			}
		}
		if exp.Kubectl == nil {
			continue
		}
		for _, e := range exp.Kubectl.PortForward {
			if e.RequestPath() != "" {
				needCurl = true
			}
			if e.JQ != "" {
				needJQ = true
			}
			if e.YQ != "" {
				needYQ = true
			}
		}
		for _, e := range exp.Kubectl.Get {
			if e.JQ != "" {
				needJQ = true
			}
			if e.YQ != "" {
				needYQ = true
			}
		}
		for _, e := range exp.Kubectl.Logs {
			if e.JQ != "" {
				needJQ = true
			}
		}
		for _, e := range exp.Kubectl.Exec {
			if e.JQ != "" {
				needJQ = true
			}
			if e.YQ != "" {
				needYQ = true
			}
		}
		for _, e := range exp.Kubectl.Cp {
			if e.JQ != "" {
				needJQ = true
			}
			if e.YQ != "" {
				needYQ = true
			}
		}
		if len(exp.Kubectl.Top) > 0 {
			needMetrics = true
		}
	}
	return
}

// ensureTool checks if tool is on PATH. If not, installs it and shows a spinner.
func ensureTool(name string) error {
	if _, err := exec.LookPath(name); err == nil {
		return nil
	}

	s := startSpinner(fmt.Sprintf("Installing %s (required by the test)...", name))

	if err := installTool(name); err != nil {
		s.Failure()
		return fmt.Errorf("could not install %q: %w", name, err)
	}

	s.Success()
	return nil
}

// installTool installs a known tool using the system package manager.
func installTool(name string) error {
	switch runtime.GOOS {
	case "linux":
		return installLinux(name)
	case "darwin":
		return installDarwin(name)
	default:
		return fmt.Errorf("%q not found and automatic install is not supported on %s — install it manually", name, runtime.GOOS)
	}
}

func installLinux(name string) error {
	// Prefer apt-get, fall back to apk (Alpine).
	if _, err := exec.LookPath("apt-get"); err == nil {
		return installWith(command.Args("apt-get", "install", "-y", "-qq", name))
	}
	if _, err := exec.LookPath("apk"); err == nil {
		return installWith(command.Args("apk", "add", "--no-cache", name))
	}
	return fmt.Errorf("%q not found and no supported package manager (apt-get, apk) detected", name)
}

func installDarwin(name string) error {
	if _, err := exec.LookPath("brew"); err != nil {
		return fmt.Errorf("%q not found and brew is not installed — run: brew install %s", name, name)
	}
	return installWith(command.Args("brew", "install", name))
}

// installWith runs a package manager's install; the error carries its output.
func installWith(c command.Command) error {
	r, err := command.Run(context.Background(), c)
	if err != nil {
		return err
	}
	return r.Err()
}
