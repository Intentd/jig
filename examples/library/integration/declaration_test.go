//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/intentd/jig"
	"github.com/intentd/jig/pkg/spec"
	toolsassert "github.com/intentd/tools/assert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// goodGoDeclaration builds the equivalent of testdata/jig.yaml.
// It needs no YAML file or file-loading operation.
func goodGoDeclaration(t *testing.T) *spec.Declaration {
	t.Helper()

	out := filepath.Join(t.TempDir(), "jig-example-commands.txt")

	return &spec.Declaration{
		Name:        "commands",
		Description: "Generate a file, check what it says, and clean it up, without a cluster",

		Env: map[string]string{
			"GREETING": "hello",
			"NAME":     "jig",
			"OUT":      out,
		},

		Environment: &spec.Environment{
			Cluster: spec.Cluster{
				Provider: spec.ProviderNone,
			},
		},

		Prepare: spec.Steps{
			{
				Name: "generate",
				Commands: spec.Commands{
					{
						Run: `echo "${{ env.GREETING }}, $NAME" > "${OUT}"`,
					},
				},
			},
		},

		Expect: spec.Expectations{
			{
				Name:    "the file says hello",
				After:   spec.After("generate"),
				Timeout: "5s",
				Commands: spec.Commands{
					{
						Run: `cat "$OUT"`,
						TextAssertion: toolsassert.TextAssertion{
							Contains: "hello, jig",
						},
					},
				},
			},
		},

		Finally: &spec.Finally{
			SideEffects: spec.SideEffects{
				Commands: spec.Commands{
					{
						Run: `rm -f "$OUT"`,
					},
				},
			},
		},
	}
}

// badGoDeclaration builds the representable invalid parts of badjig.yaml.
//
// The unknown YAML field "retries" cannot be represented by a typed
// spec.Declaration. The invalid timeout can.
func badGoDeclaration(t *testing.T) *spec.Declaration {
	t.Helper()

	decl := goodGoDeclaration(t)
	decl.Expect[0].Timeout = "5"

	return decl
}

func TestJigValidatesGoDeclaration(t *testing.T) {
	decl := goodGoDeclaration(t)

	require.NotNil(t, decl)
	assert.Equal(t, "commands", decl.Name)

	errs := jig.Validate(decl)
	require.Empty(t, errs, "Go declaration should pass validation")

	_, err := jig.Plan(decl)
	require.NoError(t, err)
}

func TestJigRejectsInvalidGoDeclaration(t *testing.T) {
	decl := badGoDeclaration(t)

	errs := jig.Validate(decl)

	require.NotEmpty(t, errs, "invalid Go declaration should fail validation")
	assert.Contains(t, errs[0].Error(), `timeout "5" is not a duration`)
}

func TestJigPlansGoDeclarationAsJSON(t *testing.T) {
	decl := goodGoDeclaration(t)

	result, err := jig.Plan(decl)
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, result.JSON(&buf))

	var plan struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Mode        string         `json:"mode"`
		Environment map[string]any `json:"environment"`

		Prepare []struct {
			Index  int    `json:"index"`
			Name   string `json:"name"`
			Action string `json:"action"`
		} `json:"prepare"`

		Expect []struct {
			Name    string `json:"name"`
			After   string `json:"after"`
			Timeout string `json:"timeout"`
		} `json:"expect"`

		Summary struct {
			Prepare      int `json:"prepare"`
			Expectations int `json:"expectations"`
			Imports      int `json:"imports"`
		} `json:"summary"`
	}

	require.NoError(t, json.Unmarshal(buf.Bytes(), &plan))

	assert.Equal(t, "commands", plan.Name)
	assert.Equal(t,
		"Generate a file, check what it says, and clean it up, without a cluster",
		plan.Description,
	)
	assert.Equal(t, "commands", plan.Mode)
	assert.NotNil(t, plan.Environment)

	require.Len(t, plan.Prepare, 1)
	assert.Equal(t, 0, plan.Prepare[0].Index)
	assert.Equal(t, "generate", plan.Prepare[0].Name)
	assert.Equal(t,
		`run echo "${{ env.GREETING }}, $NAME" > "${OUT}"`,
		plan.Prepare[0].Action,
	)

	require.Len(t, plan.Expect, 1)
	assert.Equal(t, "the file says hello", plan.Expect[0].Name)
	assert.Equal(t, "generate", plan.Expect[0].After)
	assert.Equal(t, "5s", plan.Expect[0].Timeout)

	assert.Equal(t, 1, plan.Summary.Prepare)
	assert.Equal(t, 1, plan.Summary.Expectations)
	assert.Equal(t, 0, plan.Summary.Imports)
}

func TestJigRunsGoDeclaration(t *testing.T) {
	decl := goodGoDeclaration(t)

	require.Empty(t, jig.Validate(decl))

	// Verify that Jig can produce an execution plan.
	_, err := jig.Plan(decl)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := jig.Run(ctx, decl, jig.Options{})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, result.Total(), result.Passed(),
		"all Jig expectations should pass")

	// Verify reporting.
	report := filepath.Join(t.TempDir(), "report.json")

	require.NoError(t, jig.Report(ctx, report, *result))

	info, err := os.Stat(report)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))
}
