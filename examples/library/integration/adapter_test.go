//go:build integration

package integration

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/intentd/jig"
	"github.com/intentd/jig/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSpec is the consumer's application-facing representation of a jig.
// It deliberately does not expose Jig's declaration types.
type TestSpec struct {
	Name        string
	Description string
	Greeting    string
	Recipient   string
	OutputFile  string
	Steps       []TestStep
	Checks      []TestCheck
	Cleanup     []string
}

type TestStep struct {
	Name    string
	Command string
}

type TestCheck struct {
	Name    string
	After   string
	Timeout time.Duration
	Command string
}

func toJigDeclaration(s TestSpec) *spec.Declaration {
	decl := &spec.Declaration{
		Name:        s.Name,
		Description: s.Description,
		Environment: &spec.Environment{
			Cluster: spec.Cluster{
				Provider: spec.ProviderNone,
			},
		},
	}

	for _, step := range s.Steps {
		decl.Prepare = append(decl.Prepare, spec.Step{
			Name: step.Name,
			Commands: spec.Commands{
				{Run: step.Command},
			},
		})
	}

	for _, check := range s.Checks {
		decl.Expect = append(decl.Expect, spec.Expectation{
			Name:    check.Name,
			After:   spec.After(check.After),
			Timeout: check.Timeout.String(),
			Commands: spec.Commands{
				{Run: check.Command},
			},
		})
	}

	if len(s.Cleanup) > 0 {
		finally := &spec.Finally{}
		for _, command := range s.Cleanup {
			finally.Commands = append(finally.Commands, spec.Command{
				Run: command,
			})
		}
		decl.Finally = finally
	}

	return decl
}

func TestJigFromConsumerSpec(t *testing.T) {
	output := filepath.Join(t.TempDir(), "greeting.txt")

	input := TestSpec{
		Name:        "commands",
		Description: "Generate a file, check what it says, and clean up, without a cluster",
		Greeting:    "hello",
		Recipient:   "jig",
		OutputFile:  output,
		Steps: []TestStep{
			{
				Name: "generate",
				Command: fmt.Sprintf(
					"printf '%%s, %%s\\n' %s %s > %s",
					shellQuote("hello"),
					shellQuote("jig"),
					shellQuote(output),
				),
			},
		},
		Checks: []TestCheck{
			{
				Name:    "the file says hello",
				After:   "generate",
				Timeout: 5 * time.Second,
				Command: fmt.Sprintf(
					"test \"$(cat %s)\" = %s",
					shellQuote(output),
					shellQuote("hello, jig"),
				),
			},
		},
		Cleanup: []string{
			fmt.Sprintf("rm -f %s", shellQuote(output)),
		},
	}

	// Adapt the consumer's spec into a Jig declaration.
	decl := toJigDeclaration(input)

	// Validate and build a plan before executing.
	errs := jig.Validate(decl)
	require.Empty(t, errs)

	_, err := jig.Plan(decl)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := jig.Run(ctx, decl, jig.Options{})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, result.Total(), result.Passed())
}

// shellQuote quotes a value for use as one shell argument.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
