package jig

import (
	"github.com/intentd/jig/pkg/expect"
	"github.com/intentd/jig/pkg/kubectl"
	"github.com/intentd/jig/pkg/plan"
	"github.com/intentd/jig/pkg/spec"
	"gopkg.in/yaml.v3"
)

// A jig's vocabulary, for callers: they import jig, not its packages.
type (
	// The test and its steps.
	Step         = spec.Step
	Steps        = spec.Steps
	ApplyEntry   = spec.ApplyEntry
	ApplyEntries = spec.ApplyEntries
	HelmInstall  = spec.HelmInstall
	HelmInstalls = spec.HelmInstalls
	Wait         = spec.Wait
	Waits        = spec.Waits
	Restart      = spec.Restart
	Restarts     = spec.Restarts
	Command      = spec.Command
	Commands     = spec.Commands
	Paths        = spec.Paths
	Function     = spec.Function
	Functions    = spec.Functions

	// The environment.
	Environment     = spec.Environment
	Provision       = spec.Provision
	Cluster         = spec.Cluster
	ClusterProvider = spec.ClusterProvider
	Import          = spec.Import

	// Expectations and the checks in them.
	After            = spec.After
	Expectation      = spec.Expectation
	Expectations     = spec.Expectations
	ExpectationScope = spec.ExpectationScope
	ResourceCheck    = spec.ResourceCheck
	ExternalCheck    = spec.ExternalCheck
	ExternalChecks   = spec.ExternalChecks
	ExpectInclude    = spec.ExpectInclude
	ExternalInclude  = spec.ExternalInclude
	// ImagesInclude, ApplyInclude and HelmInclude are the files an include:
	// entry in an images, apply or helm list names.
	ImagesInclude         = spec.ImagesInclude
	ApplyInclude          = spec.ApplyInclude
	HelmInclude           = spec.HelmInclude
	Kubectl               = spec.Kubectl
	KubectlGet            = spec.KubectlGet
	KubectlLogs           = spec.KubectlLogs
	LogsPods              = spec.LogsPods
	KubectlDescribe       = spec.KubectlDescribe
	KubectlExec           = spec.KubectlExec
	KubectlApply          = spec.KubectlApply
	KubectlDelete         = spec.KubectlDelete
	KubectlPatch          = spec.KubectlPatch
	KubectlEvents         = spec.KubectlEvents
	KubectlAuth           = spec.KubectlAuth
	KubectlCp             = spec.KubectlCp
	KubectlTop            = spec.KubectlTop
	KubectlLeaderElection = spec.KubectlLeaderElection
	KubectlPortForward    = spec.KubectlPortForward
	KubectlRestart        = spec.KubectlRestart
	KubectlScale          = spec.KubectlScale

	// Side effects.
	OnFailure   = spec.OnFailure
	Finally     = spec.Finally
	SideEffects = spec.SideEffects

	// The ecosystem.
	Ecosystem         = spec.Ecosystem
	DataService       = spec.DataService
	EcosystemPostgres = spec.EcosystemPostgres
	EcosystemMariadb  = spec.EcosystemMariadb
	EcosystemRabbitmq = spec.EcosystemRabbitmq
	EcosystemRedis    = spec.EcosystemRedis
	EcosystemKafka    = spec.EcosystemKafka
	EcosystemSecrets  = spec.EcosystemSecrets
	EcosystemExport   = spec.EcosystemExport
	EcosystemDefine   = spec.EcosystemDefine
)

// PlanFormat is the format for a run's plan: text(default) or json
type PlanFormat = plan.Format

const (
	PlanFormatText PlanFormat = plan.FormatText
	PlanFormatJSON PlanFormat = plan.FormatJSON
)

const (
	AfterEnvironment = spec.AfterEnvironment
	AfterPrepare     = spec.AfterPrepare
	ProviderKind     = spec.ProviderKind
	ProviderNone     = spec.ProviderNone
	LogsPodsAny      = spec.LogsPodsAny
	LogsPodsAll      = spec.LogsPodsAll
)

// ValidLogsPodsValues are kubectl.logs' pods values.
var ValidLogsPodsValues = spec.ValidLogsPodsValues

// ValidClusterProviders returns the supported providers, in display order.
func ValidClusterProviders() []string { return spec.ValidClusterProviders() }

// ApplyShorthand reads an apply written as a path or a list of paths; nil
// when it is neither.
func ApplyShorthand(value *yaml.Node) ApplyEntries { return spec.ApplyShorthand(value) }

// IsExpectIncludeDoc reports whether the file is an expect include.
func IsExpectIncludeDoc(path string) (bool, error) { return spec.IsExpectIncludeDoc(path) }

// IsExternalIncludeDoc reports whether the file is an external check include.
func IsExternalIncludeDoc(path string) (bool, error) { return spec.IsExternalIncludeDoc(path) }

// ValidateKubectl returns every problem with the expectations' kubectl
// checks; relative paths resolve against baseDir.
func ValidateKubectl(expects Expectations, baseDir string) []error {
	return kubectl.Validate(expects, baseDir)
}

// ValidateKubectlBlock returns every problem with one kubectl block at loc;
// post is set for onFailure and finally.
func ValidateKubectlBlock(loc string, k *Kubectl, baseDir string, post bool) []error {
	return kubectl.ValidateBlock(loc, k, baseDir, post)
}

// ValidateExternal returns every problem with the expectations' external
// checks.
func ValidateExternal(expects Expectations) []error { return expect.ValidateExternal(expects) }

// ValidateExternalCalls returns every problem with the external calls at loc.
func ValidateExternalCalls(loc string, calls []ExternalCheck) []error {
	return expect.ValidateExternalCalls(loc, calls)
}
