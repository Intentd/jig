package operator

import "github.com/intentd/tools/cluster"

// The operators, pinned. Each runs in a namespace of its own, so one a run
// installed is easy to find and removed with it.
var (
	// CertManager issues the RabbitMQ operators' webhook certificates.
	CertManager = Operator{
		Name: "cert-manager", Namespace: "cert-manager",
		Chart: &cluster.HelmRelease{
			Repo: "https://charts.jetstack.io", Chart: "cert-manager", Version: "v1.21.3",
			Release: "cert-manager", Namespace: "cert-manager", CreateNamespace: true,
			Values: map[string]any{"crds.enabled": true},
		},
		CRD:         "certificates.cert-manager.io",
		Deployments: []string{"cert-manager", "cert-manager-webhook", "cert-manager-cainjector"},
	}

	// CloudNativePG runs PostgreSQL clusters.
	CloudNativePG = Operator{
		Name: "cloudnative-pg", Namespace: "cnpg-system",
		Chart: &cluster.HelmRelease{
			Repo: "https://cloudnative-pg.github.io/charts", Chart: "cloudnative-pg", Version: "0.29.1",
			Release: "cloudnative-pg", Namespace: "cnpg-system", CreateNamespace: true,
		},
		CRD:         "clusters.postgresql.cnpg.io",
		Deployments: []string{"cloudnative-pg"},
	}

	// MariadbCRDs are mariadb-operator's CRDs, a chart of their own.
	MariadbCRDs = Operator{
		Name: "mariadb-operator-crds", Namespace: "mariadb-system",
		Chart: &cluster.HelmRelease{
			Repo: "https://helm.mariadb.com/mariadb-operator", Chart: "mariadb-operator-crds", Version: "26.10.2",
			Release: "mariadb-operator-crds", Namespace: "mariadb-system", CreateNamespace: true,
		},
		CRD: "mariadbs.k8s.mariadb.com",
	}

	// MariadbOperator runs MariaDB servers; its own CRDs are off (MariadbCRDs).
	MariadbOperator = Operator{
		Name: "mariadb-operator", Namespace: "mariadb-system",
		Chart: &cluster.HelmRelease{
			Repo: "https://helm.mariadb.com/mariadb-operator", Chart: "mariadb-operator", Version: "26.10.2",
			Release: "mariadb-operator", Namespace: "mariadb-system", CreateNamespace: true,
			Values: map[string]any{"crds.enabled": false},
		},
		Deployments: []string{"mariadb-operator", "mariadb-operator-webhook"},
	}

	// RabbitmqCluster runs RabbitMQ clusters (a release manifest: no chart).
	RabbitmqCluster = Operator{
		Name: "rabbitmq-cluster-operator", Namespace: "rabbitmq-system",
		Manifest:    "https://github.com/rabbitmq/cluster-operator/releases/download/v2.23.1/cluster-operator.yml",
		CRD:         "rabbitmqclusters.rabbitmq.com",
		Deployments: []string{"rabbitmq-cluster-operator"},
	}

	// RabbitmqTopology manages vhosts, users and permissions.
	RabbitmqTopology = Operator{
		Name: "rabbitmq-topology-operator", Namespace: "rabbitmq-system",
		Manifest:    "https://github.com/rabbitmq/messaging-topology-operator/releases/download/v1.20.4/messaging-topology-operator-with-certmanager.yaml",
		CRD:         "users.rabbitmq.com",
		Deployments: []string{"messaging-topology-operator"},
	}

	// Redis runs standalone Redis instances (OT Redis Operator).
	Redis = Operator{
		Name: "redis-operator", Namespace: "redis-system",
		Chart: &cluster.HelmRelease{
			Repo: "https://ot-container-kit.github.io/helm-charts", Chart: "redis-operator", Version: "0.27.0",
			Release: "redis-operator", Namespace: "redis-system", CreateNamespace: true,
		},
		CRD:         "redis.redis.redis.opstreelabs.in",
		Deployments: []string{"redis-operator"},
	}

	// Strimzi runs Kafka clusters and topics, watching every namespace.
	Strimzi = Operator{
		Name: "strimzi", Namespace: "strimzi-system",
		Chart: &cluster.HelmRelease{
			Repo: "https://strimzi.io/charts/", Chart: "strimzi-kafka-operator", Version: "1.3.1",
			Release: "strimzi", Namespace: "strimzi-system", CreateNamespace: true,
			Values: map[string]any{"watchAnyNamespace": true},
		},
		CRD:         "kafkas.kafka.strimzi.io",
		Deployments: []string{"strimzi-cluster-operator"},
	}
)
