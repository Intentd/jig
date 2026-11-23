package kubectl

import (
	"context"
	"os/exec"
	"strconv"
	"testing"

	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/assert"
	"github.com/intentd/tools/command"
)

// exitErrorWithCode runs a trivial shell command that exits with code, and
// returns the resulting *command.ExitError — a real one, not a hand-built
// stand-in, so assertApplyOutput's errors.As branch is exercised
// exactly as it would be against a real kubectl failure.
func exitErrorWithCode(t *testing.T, code int) error {
	t.Helper()
	r, err := command.Run(context.Background(), command.Shell("exit "+strconv.Itoa(code)))
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if r.Err() == nil {
		t.Fatalf("command unexpectedly exited 0")
	}
	return r.Err()
}

func TestAssertKubectlApplyOutput_ExpectedSuccess(t *testing.T) {
	err := assertApplyOutput("kubectl apply -f x.yaml", []byte("website.testground.inrun.dev/site-a created"), nil, spec.KubectlApply{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAssertKubectlApplyOutput_UnexpectedFailure(t *testing.T) {
	runErr := exitErrorWithCode(t, 1)
	err := assertApplyOutput("kubectl apply -f x.yaml", []byte("Error from server (Forbidden): admission webhook denied the request"), runErr, spec.KubectlApply{})
	if err == nil {
		t.Fatal("expected error — apply failed but ExitCode: 0 (default) means success was expected")
	}
}

func TestAssertKubectlApplyOutput_ExpectedRejection(t *testing.T) {
	runErr := exitErrorWithCode(t, 1)
	e := spec.KubectlApply{
		ExitCode: 1,
		Assert:   &assert.Assertions{TextAssertion: assert.TextAssertion{Contains: "denied the request"}},
	}
	err := assertApplyOutput("kubectl apply -f x.yaml", []byte("Error from server (Forbidden): admission webhook denied the request"), runErr, e)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAssertKubectlApplyOutput_ExpectedRejectionButSucceeded(t *testing.T) {
	e := spec.KubectlApply{ExitCode: 1}
	err := assertApplyOutput("kubectl apply -f x.yaml", []byte("website.testground.inrun.dev/site-b created"), nil, e)
	if err == nil {
		t.Fatal("expected error — apply succeeded but ExitCode: 1 means rejection was expected")
	}
}

func TestAssertKubectlApplyOutput_WrongExitCode(t *testing.T) {
	runErr := exitErrorWithCode(t, 2)
	e := spec.KubectlApply{ExitCode: 1}
	err := assertApplyOutput("kubectl apply -f x.yaml", []byte("some other failure"), runErr, e)
	if err == nil {
		t.Fatal("expected error — got exit code 2, wanted 1")
	}
}

func TestAssertKubectlApplyOutput_RejectionMessageMismatch(t *testing.T) {
	runErr := exitErrorWithCode(t, 1)
	e := spec.KubectlApply{
		ExitCode: 1,
		Assert:   &assert.Assertions{TextAssertion: assert.TextAssertion{Contains: "spec.domain must be unique"}},
	}
	err := assertApplyOutput("kubectl apply -f x.yaml", []byte("Error from server (Forbidden): some unrelated rule denied the request"), runErr, e)
	if err == nil {
		t.Fatal("expected error — output doesn't contain the expected substring")
	}
}

func TestAssertKubectlApplyOutput_NonExitError(t *testing.T) {
	// A non-ExitError failure (e.g. "kubectl: command not found") should
	// always propagate, regardless of e.ExitCode — there's no exit code to
	// compare against.
	err := assertApplyOutput("kubectl apply -f x.yaml", nil, exec.ErrNotFound, spec.KubectlApply{ExitCode: 1})
	if err == nil {
		t.Fatal("expected error to propagate for a non-ExitError failure")
	}
}
