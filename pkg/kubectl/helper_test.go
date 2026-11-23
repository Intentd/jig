package kubectl

import (
	"strings"
	"testing"

	"k8s.io/utils/ptr"

	"github.com/intentd/tools/assert"
)

func TestAssertOutput(t *testing.T) {
	cases := []struct {
		name    string
		a       *assert.Assertions
		out     string
		wantErr bool
		wantIn  string // substring expected in the error, when wantErr
	}{
		{
			name: "contains passes",
			a:    &assert.Assertions{TextAssertion: assert.TextAssertion{Contains: "Ready"}},
			out:  "phase: Ready",
		},
		{
			name:    "contains fails",
			a:       &assert.Assertions{TextAssertion: assert.TextAssertion{Contains: "Ready"}},
			out:     "phase: Pending",
			wantErr: true,
			wantIn:  "phase: Pending",
		},
		{
			name: "notExists passes on empty",
			a:    &assert.Assertions{TextAssertion: assert.TextAssertion{NotExists: ptr.To(true)}},
			out:  "",
		},
		{
			name:    "notExists fails on non-empty",
			a:       &assert.Assertions{TextAssertion: assert.TextAssertion{NotExists: ptr.To(true)}},
			out:     "some-value",
			wantErr: true,
		},
		{
			name: "equals passes",
			a:    &assert.Assertions{TextAssertion: assert.TextAssertion{Equals: "site-a-0"}},
			out:  "site-a-0",
		},
		{
			name:    "equals fails",
			a:       &assert.Assertions{TextAssertion: assert.TextAssertion{Equals: "site-a-0"}},
			out:     "site-b-0",
			wantErr: true,
			wantIn:  "site-b-0",
		},
		{
			name: "json assertion passes",
			a:    &assert.Assertions{JSON: assert.JSONAssertions{{Path: ".phase", TextAssertion: assert.TextAssertion{Equals: "Ready"}}}},
			out:  `{"phase":"Ready"}`,
		},
		{
			name:    "json assertion fails",
			a:       &assert.Assertions{JSON: assert.JSONAssertions{{Path: ".phase", TextAssertion: assert.TextAssertion{Equals: "Ready"}}}},
			out:     `{"phase":"Pending"}`,
			wantErr: true,
			wantIn:  "json .phase",
		},
		{
			name:    "invalid json fails",
			a:       &assert.Assertions{JSON: assert.JSONAssertions{{Path: ".phase"}}},
			out:     "not-json",
			wantErr: true,
			wantIn:  "not valid JSON",
		},
		{
			name: "text list passes",
			a:    &assert.Assertions{Text: assert.TextAssertions{{Contains: "site"}, {NotContains: "error"}}},
			out:  "site-a-0",
		},
		{
			name:    "text list names the failing entry",
			a:       &assert.Assertions{Text: assert.TextAssertions{{Contains: "site"}, {Contains: "b"}}},
			out:     "site-a-0",
			wantErr: true,
			wantIn:  "text[1]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := assertOutput(tc.out, tc.a.TextAssertion, tc.a, "test")
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantIn != "" && !strings.Contains(err.Error(), tc.wantIn) {
				t.Fatalf("error %q does not contain %q", err, tc.wantIn)
			}
		})
	}
}
