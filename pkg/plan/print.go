package plan

import (
	"fmt"
	"io"
	"strings"

	jigecosystem "github.com/intentd/jig/pkg/ecosystem"
	"github.com/intentd/tools/term"
)

// printImagePlans renders the resolved image plans.
func printImagePlans(w io.Writer, images []imagePlan) {
	if len(images) == 0 {
		return
	}

	line := func(format string, a ...any) {
		fmt.Fprintf(w, "          %s\n", term.Gray(fmt.Sprintf(format, a...)))
	}

	fmt.Fprintf(w, "    %s\n", term.Gray("environment.images :"))

	for i, img := range images {
		description := img.Image
		if img.Build != nil {
			description = fmt.Sprintf(
				"build %s (%s) → %s",
				img.Build.Context,
				img.Build.Dockerfile,
				img.Image,
			)
		}

		fmt.Fprintf(w, "      [%d] %s %s\n",
			i,
			description,
			term.Gray("("+strings.Join(img.Operations, ", ")+")"),
		)

		if row := img.Row; row != nil {
			line("from    : images[%d] forEach, row %d (%s; needs: [%s] means every row)",
				row.Entry, row.Index, img.Name, row.Base)
		}

		if git := img.Git; git != nil {
			pull := ""
			if git.Pull != "" {
				pull = ", pull " + git.Pull
			}
			line("git     : %s at %s%s", git.URL, git.At, pull)
		}

		deploy := img.Deploy
		if deploy == nil {
			continue
		}

		line("deploy  : %s (namespace: %s)", deploy.Name, deploy.Namespace)

		for _, port := range deploy.Ports {
			if !port.Exposed {
				line("port    : %d (container only: no Service, no forward)", port.Port)
				continue
			}

			line("service : %s.%s.svc.cluster.local:%d",
				deploy.Name, deploy.Namespace, port.Port)

			if port.Forwarded {
				line("forward : localhost:%d → %d", port.Host, port.Port)
			}
		}

		for _, init := range deploy.Init {
			line("init    : %s: %s (%s)",
				init.Name,
				strings.Join(init.Command, " "),
				init.Image,
			)
		}

		if len(deploy.Needs) > 0 {
			line("needs   : %s (deployed and ready first)",
				strings.Join(deploy.Needs, ", "))
		}

		if len(deploy.Env) > 0 {
			line("env     : %s", strings.Join(deploy.Env, ", "))
		}

		for _, secret := range deploy.EnvFrom {
			if secret.WrittenByEcosystem {
				line("envFrom : %s (the ecosystem writes it)", secret.Name)
				continue
			}

			fmt.Fprintf(w,
				"          %s envFrom %s: the ecosystem doesn't write it in %s; a manifest must, before the deployment starts\n",
				term.Yellow("⚠"),
				secret.Name,
				secret.Namespace,
			)
		}
	}
}

// printEcosystemPlan renders the resolved ecosystem plan.
func printEcosystemPlan(w io.Writer, e *ecosystemPlan) {
	if e == nil {
		return
	}

	gray := func(format string, a ...any) {
		fmt.Fprintf(w, "    %s\n", term.Gray(fmt.Sprintf(format, a...)))
	}

	gray("ecosystem (namespace %s):", e.Namespace)

	for i, service := range e.Services {
		fmt.Fprintf(w, "      [%d] %-9s %s\n",
			i,
			service.Name,
			term.Gray(service.Address),
		)
	}

	if len(e.Operators) > 0 {
		gray("operators (pinned; one already running is left alone): %s",
			strings.Join(e.Operators, ", "))
	}

	for _, secret := range e.Secrets {
		gray("secret %s (%s): %s",
			secret.Name,
			secret.Namespace,
			strings.Join(secret.Keys, ", "),
		)
	}
}

// ecosystemSecretNames are the export and define Secrets the ecosystem
// writes, as namespace/name.
func ecosystemSecretNames(e *ecosystem) (map[string]bool, error) {
	out := map[string]bool{}
	p, err := jigecosystem.New(e, jigecosystem.Placeholder)
	if err != nil {
		return out, fmt.Errorf("ecosystem: %w", err)
	}
	if p == nil {
		return out, nil
	}
	for _, sec := range p.Outputs {
		out[sec.Namespace+"/"+sec.Name] = true
	}
	return out, nil
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// action names what a step does.
func action(s Step) string {
	var parts []string
	for _, e := range s.Apply {
		if e.Generated {
			parts = append(parts, "apply "+e.Path+", generated at run time")
		} else {
			parts = append(parts, "apply "+e.Path)
		}
	}
	if s.Helm != nil {
		parts = append(parts, "helm "+s.Helm.Chart)
	}
	for _, p := range s.Delete {
		parts = append(parts, "delete "+p)
	}
	for _, c := range s.Commands {
		parts = append(parts, "run "+c.Run)
	}
	if len(s.Wait) > 0 {
		parts = append(parts, fmt.Sprintf("wait for %d", len(s.Wait)))
	}
	for _, r := range s.Restart {
		parts = append(parts, "restart "+strings.ToLower(r.Kind)+"/"+r.Name)
	}
	if s.Keep {
		parts = append(parts, "kept for later imports")
	}
	return strings.Join(parts, ", ")
}
