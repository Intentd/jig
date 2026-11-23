package check

import (
	"fmt"
	"time"

	"github.com/intentd/jig/pkg/spec"
)

// extras reports what keeps stays, order, repeat and guards from running.
func extras(d *spec.Declaration) []error {
	var errs []error
	for i, e := range d.Expect {
		at := fmt.Sprintf("expect[%d] %q", i, e.Name)
		if e.Stays != "" {
			if _, err := time.ParseDuration(e.Stays); err != nil {
				errs = append(errs, fmt.Errorf("%s: stays %q: %w", at, e.Stays, err))
			}
		}
		if len(e.Order) == 1 {
			errs = append(errs, fmt.Errorf("%s: order needs two objects or more", at))
		}
		for j, o := range e.Order {
			if o.Kind == "" || o.Name == "" || o.At == "" {
				errs = append(errs, fmt.Errorf("%s: order[%d]: kind, name and at are required", at, j))
			}
		}
		for j, c := range e.External {
			if c.Repeat < 0 {
				errs = append(errs, fmt.Errorf("%s: external[%d]: repeat must be 0 or more", at, j))
			}
		}
		for j, c := range e.Commands {
			for _, js := range c.JSON {
				if js.Capture != "" && js.Path == "" {
					errs = append(errs, fmt.Errorf("%s: commands[%d]: capture %s needs a path", at, j, js.Capture))
				}
			}
		}
	}
	for i, g := range d.Guards {
		at := fmt.Sprintf("guards[%d] %q", i, g.Name)
		if g.Name == "" {
			errs = append(errs, fmt.Errorf("guards[%d]: name is required", i))
		}
		n := 0
		if g.Events != nil {
			n++
			if len(g.Events.Namespaces) == 0 {
				errs = append(errs, fmt.Errorf("%s: events.namespaces is required", at))
			}
			if ty := g.Events.Type; ty != "" && ty != "Warning" && ty != "Normal" {
				errs = append(errs, fmt.Errorf("%s: events.type %q: want Warning or Normal", at, ty))
			}
		}
		if g.Restarts != nil {
			n++
			if len(g.Restarts.Namespaces) == 0 {
				errs = append(errs, fmt.Errorf("%s: restarts.namespaces is required", at))
			}
		}
		if g.Logs != nil {
			n++
			if g.Logs.LabelSelector == "" || g.Logs.NotContains == "" {
				errs = append(errs, fmt.Errorf("%s: logs.labelSelector and logs.notContains are required", at))
			}
		}
		if n != 1 {
			errs = append(errs, fmt.Errorf("%s: set one of events, restarts and logs", at))
		}
	}
	if len(d.Guards) > 0 && d.Environment.Provider() == spec.ProviderNone {
		errs = append(errs, fmt.Errorf("guards look at a cluster; cluster.provider is none"))
	}
	return errs
}
