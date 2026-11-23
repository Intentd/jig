package spec

// Guard is a check on the whole run, made once at its end: what went wrong
// that no expectation asked about. Exactly one of Events, Restarts and Logs.
type Guard struct {
	// Name labels it in the report.
	Name string `yaml:"name"`
	// Events fails on events of a type in the namespaces during the run.
	Events *GuardEvents `yaml:"events,omitempty"`
	// Restarts fails on container restarts in the namespaces.
	Restarts *GuardRestarts `yaml:"restarts,omitempty"`
	// Logs fails when the logs of the pods selected contain the text.
	Logs *GuardLogs `yaml:"logs,omitempty"`
}

// GuardEvents is the events a guard fails on.
type GuardEvents struct {
	// Type is Warning (the default) or Normal.
	Type       string   `yaml:"type,omitempty"`
	Namespaces []string `yaml:"namespaces"`
	// Ignore are reasons that are known and harmless (FailedScheduling).
	Ignore []string `yaml:"ignore,omitempty"`
}

// GuardRestarts is where a guard counts container restarts.
type GuardRestarts struct {
	Namespaces []string `yaml:"namespaces"`
	// LabelSelector narrows it to some pods.
	LabelSelector string `yaml:"labelSelector,omitempty"`
}

// GuardLogs is the logs a guard reads.
type GuardLogs struct {
	LabelSelector string `yaml:"labelSelector"`
	Namespace     string `yaml:"namespace,omitempty"`
	Container     string `yaml:"container,omitempty"`
	// NotContains is the text that must not appear.
	NotContains string `yaml:"notContains"`
}
