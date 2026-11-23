package spec

// DataService is the configuration every data service shares: where the
// instance lives, how it authenticates, and how big it is.
//
// It is embedded by each dependency type.
type DataService struct {
	// Namespace for this instance. Default: the ecosystem's namespace.
	Namespace string `yaml:"namespace,omitempty"`
	// User is the application user (a login role for Postgres). Default: "app".
	User string `yaml:"user,omitempty"`
	// Database is the user's database. Default: "app".
	Database string `yaml:"database,omitempty"`
	// Password is the user's (or Redis's) password. Default: generated per
	// run.
	Password string `yaml:"password,omitempty"`
	// Storage is the volume size. Default: "1Gi".
	Storage string `yaml:"storage,omitempty"`
	// Replicas is the replica count. Default: 1.
	Replicas int `yaml:"replicas,omitempty"`
}

// Ecosystem declares the data services a run needs and the Secrets that carry
// their connection details to the workload.
//
// jig installs the operators the declared services need, then the
// data-services chart with values built from the declaration, waits for each
// instance, and writes the Secrets. Names and addresses are fixed (one
// instance per service, named for it), so nothing is read back.
type Ecosystem struct {
	// Namespace is where the instances live. Default: "data".
	Namespace string `yaml:"namespace,omitempty"`

	// Postgres, MariaDB (MySQL protocol), RabbitMQ, Redis and Kafka declare
	// the services. Each is optional; only what is declared is installed.
	Postgres *EcosystemPostgres `yaml:"postgres,omitempty"`
	Mariadb  *EcosystemMariadb  `yaml:"mariadb,omitempty"`
	Rabbitmq *EcosystemRabbitmq `yaml:"rabbitmq,omitempty"`
	Redis    *EcosystemRedis    `yaml:"redis,omitempty"`
	Kafka    *EcosystemKafka    `yaml:"kafka,omitempty"`

	// Secrets declares how the resolved connection details reach the
	// workload: export writes the tool's canonical variables, define writes
	// Secrets the test names with the app's own keys.
	Secrets *EcosystemSecrets `yaml:"secrets,omitempty"`
}

// EcosystemPostgres declares a Postgres instance.
type EcosystemPostgres struct {
	DataService `yaml:",inline"`
}

// EcosystemMariadb declares a MariaDB instance; it speaks the MySQL protocol.
type EcosystemMariadb struct {
	DataService `yaml:",inline"`
}

// EcosystemRabbitmq declares a RabbitMQ instance.
type EcosystemRabbitmq struct {
	DataService `yaml:",inline"`
	// Vhosts lists the vhosts to create, the user with full permissions on
	// each. Default: [app].
	Vhosts []string `yaml:"vhosts,omitempty"`
}

// EcosystemRedis declares a Redis instance.
type EcosystemRedis struct {
	DataService `yaml:",inline"`
}

// EcosystemKafka declares a Kafka instance.
type EcosystemKafka struct {
	DataService `yaml:",inline"`
	// Topics lists the topics to create.
	Topics []string `yaml:"topics,omitempty"`
}

// EcosystemSecrets declares how dependency connection details reach the
// workload. export and define are independent and composable in one spec.
type EcosystemSecrets struct {
	// Export writes the tool's canonical variables (POSTGRES_URL, REDIS_HOST,
	// …) for the named tools into one Secret.
	Export *EcosystemExport `yaml:"export,omitempty"`

	// Define writes Secrets the test names, with the app's own keys and
	// resolver paths.
	Define map[string]EcosystemDefine `yaml:"define,omitempty"`
}

// EcosystemExport writes the canonical variables for the named tools.
type EcosystemExport struct {
	// Tools names the dependencies to export. Omitted: all declared.
	Tools []string `yaml:"tools,omitempty"`
	// Name is the Secret name. Default: "ecosystem".
	Name string `yaml:"name,omitempty"`
	// Namespace is the Secret namespace. Default: "default".
	Namespace string `yaml:"namespace,omitempty"`
}

// EcosystemDefine is a Secret the test names.
type EcosystemDefine struct {
	// Namespace is the Secret namespace. Default: "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Keys maps the app's variable names to connection details, each
	// tool.field:
	//
	//	keys:
	//	  DATABASE_URL: postgres.url
	//	  KAFKA_BOOTSTRAP: kafka.bootstrap
	Keys map[string]string `yaml:"keys"`
}
