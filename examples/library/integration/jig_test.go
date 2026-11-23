//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/intentd/jig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	validDeclaration   = "../testdata/jig.yaml"
	invalidDeclaration = "../testdata/badjig.yaml"
)

func TestJigLoadsValidDeclaration(t *testing.T) {
	decl, err := jig.FromFile(validDeclaration)

	require.NoError(t, err)
	require.NotNil(t, decl)

	assert.Equal(t, "commands", decl.Name)
}

func TestJigRejectsInvalidDeclaration(t *testing.T) {
	_, err := jig.FromFile(invalidDeclaration)

	require.Error(t, err, "invalid YAML declaration should be rejected")
}

func TestJigValidatesDeclaration(t *testing.T) {
	decl, err := jig.FromFile(validDeclaration)
	require.NoError(t, err)

	errs := jig.Validate(decl)

	assert.Empty(t, errs, "valid declaration should pass validation")
}

func TestJigValidatesFailsInvalidDeclaration(t *testing.T) {
	decl, err := jig.FromFile(invalidDeclaration)
	require.Error(t, err)
	assert.ErrorContains(t, err, "additional properties 'retries' not allowed")

	errs := jig.Validate(decl)
	if len(errs) == 0 {
		t.Fatalf("invalid declaration: should fail")
	}
}

func TestJigRunsCommandsScenario(t *testing.T) {
	decl, err := jig.FromFile(validDeclaration)
	require.NoError(t, err)

	errs := jig.Validate(decl)
	require.Empty(t, errs, "declaration must be valid before execution")

	// Verify that Jig can produce an execution plan.
	_, err = jig.Plan(decl)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := jig.Run(ctx, decl, jig.Options{})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, result.Total(), result.Passed(), "all Jig expectations should pass")

	// Verify reporting.
	report := filepath.Join(t.TempDir(), "report.json")

	require.NoError(t, jig.Report(ctx, report, *result))

	info, err := os.Stat(report)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))
}
