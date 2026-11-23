package plan

// Result is the machine-readable representation of a declaration's plan.
//
// Errors that prevent a plan from being constructed are returned separately
// by buildPlan. Non-fatal issues are included in Warnings.
type Result struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Mode        string            `json:"mode"`
	Cluster     string            `json:"cluster,omitempty"`
	Environment *environmentPlan  `json:"environment,omitempty"`
	Prepare     []preparePlan     `json:"prepare,omitempty"`
	Imports     []importPlan      `json:"imports,omitempty"`
	Expect      []expectationPlan `json:"expect,omitempty"`
	Guards      []string          `json:"guards,omitempty"`
	Warnings    warnings          `json:"warnings,omitempty"`
	Summary     summary           `json:"summary"`
}

type environmentPlan struct {
	Apply     []string       `json:"apply,omitempty"`
	Helm      int            `json:"helmCharts,omitempty"`
	Wait      int            `json:"waitResources,omitempty"`
	Images    []imagePlan    `json:"images,omitempty"`
	Ecosystem *ecosystemPlan `json:"ecosystem,omitempty"`
}

type imagePlan struct {
	Name       string           `json:"name,omitempty"`
	Image      string           `json:"image"`
	Operations []string         `json:"operations"`
	Build      *imageBuildPlan  `json:"build,omitempty"`
	Row        *imageRowPlan    `json:"row,omitempty"`
	Git        *imageGitPlan    `json:"git,omitempty"`
	Deploy     *imageDeployPlan `json:"deploy,omitempty"`
}

type imageBuildPlan struct {
	Context    string `json:"context"`
	Dockerfile string `json:"dockerfile"`
}

type imageRowPlan struct {
	Entry int    `json:"entry"`
	Index int    `json:"index"`
	Name  string `json:"name"`
	Base  string `json:"base"`
}

type imageGitPlan struct {
	URL  string `json:"url"`
	At   string `json:"at"`
	Pull string `json:"pull,omitempty"`
}

type imageEnvFromPlan struct {
	Name               string `json:"name"`
	Namespace          string `json:"namespace"`
	WrittenByEcosystem bool   `json:"writtenByEcosystem"`
}

type imageDeployPlan struct {
	Name      string             `json:"name"`
	Namespace string             `json:"namespace"`
	Ports     []imagePortPlan    `json:"ports,omitempty"`
	Init      []imageInitPlan    `json:"init,omitempty"`
	Needs     []string           `json:"needs,omitempty"`
	Env       []string           `json:"env,omitempty"`
	EnvFrom   []imageEnvFromPlan `json:"envFrom,omitempty"`
}

type imagePortPlan struct {
	Port      int32 `json:"port"`
	Exposed   bool  `json:"exposed"`
	Forwarded bool  `json:"forwarded,omitempty"`
	Host      int32 `json:"host,omitempty"`
}

type imageInitPlan struct {
	Name    string   `json:"name"`
	Command []string `json:"command,omitempty"`
	Image   string   `json:"image,omitempty"`
}

type ecosystemPlan struct {
	Namespace string                 `json:"namespace"`
	Services  []ecosystemServicePlan `json:"services,omitempty"`
	Operators []string               `json:"operators,omitempty"`
	Secrets   []ecosystemSecretPlan  `json:"secrets,omitempty"`
}

type ecosystemServicePlan struct {
	Name    string `json:"name"`
	Address string `json:"address,omitempty"`
}

type ecosystemSecretPlan struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace"`
	Keys      []string `json:"keys,omitempty"`
}

type preparePlan struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Action string `json:"action"`
}

type importPlan struct {
	Path         string            `json:"path"`
	FreshCluster bool              `json:"freshCluster,omitempty"`
	Wait         string            `json:"wait,omitempty"`
	Prepare      []preparePlan     `json:"prepare,omitempty"`
	Expect       []expectationPlan `json:"expectations,omitempty"`
}

type expectationPlan struct {
	Name    string `json:"name"`
	After   After  `json:"after"`
	Timeout string `json:"timeout"`
}

type warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type warnings = []warning

type summary struct {
	Prepare      int  `json:"prepare"`
	Expectations int  `json:"expectations"`
	Imports      int  `json:"imports"`
	Aggregator   bool `json:"aggregator,omitempty"`
}
