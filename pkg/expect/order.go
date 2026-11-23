package expect

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/intentd/jig/pkg/kubectl"
	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/structured"
)

// checkOrder reads each entry's timestamp and fails unless each is later
// than the one before: A happened before B.
func checkOrder(ctx context.Context, order []spec.OrderEntry, sc spec.ExpectationScope) error {
	var prev time.Time
	var prevName string
	for i, o := range order {
		out, err := kubectl.Get(ctx, kubectl.GetOptions{Kind: o.Kind, Name: o.Name, Namespace: o.Namespace, Format: "json", WorkDir: sc.WorkDir, Env: sc.Env})
		if err != nil {
			return fmt.Errorf("order[%d] %s/%s: %w\n%s", i, o.Kind, o.Name, err, out)
		}
		var doc any
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			return fmt.Errorf("order[%d] %s/%s: %w", i, o.Kind, o.Name, err)
		}
		v, _ := structured.LookupPath(doc, o.At)
		at, err := time.Parse(time.RFC3339, structured.RenderJSONValue(v))
		if err != nil {
			return fmt.Errorf("order[%d] %s/%s: %s is not a timestamp yet (%q)", i, o.Kind, o.Name, o.At, structured.RenderJSONValue(v))
		}
		name := fmt.Sprintf("%s/%s %s", o.Kind, o.Name, o.At)
		if i > 0 && !at.After(prev) {
			return fmt.Errorf("order: %s (%s) is not after %s (%s)", name, at.Format(time.RFC3339), prevName, prev.Format(time.RFC3339))
		}
		prev, prevName = at, name
	}
	return nil
}
