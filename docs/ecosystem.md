# Ecosystem: data services

Declared under `environment.ecosystem`.

A jig declares the databases, queues and caches its workload needs, and jig provides them: it installs the operators they need, the services themselves, and Secrets with their connection details, before `prepare` deploys anything that reads them.

```yaml
environment:
  ecosystem:
    postgres: { user: shop, database: shop }
    kafka: { topics: [orders, payments] }
    secrets:
      export: { tools: [postgres], name: shop-db, namespace: shop }
      define:
        api-kafka:
          namespace: shop
          keys: { KAFKA_BOOTSTRAP: kafka.bootstrap }

  images:
    - name: api
      image: api:dev
      build: { context: ./api, dockerfile: Dockerfile.prod }
      deploy:
        namespace: shop
        ports: [8090]
        replicas: 2
        envFrom: [shop-db, api-kafka]                                   # the Secrets the ecosystem writes
        env: { POSTGRES_PASSWORD: "${{ ecosystem.postgres.password }}" }  # or one value, by reference

expect:
  - name: the api is up
    after: environment
    timeout: 120s
    resources:
      - { kind: Deployment, name: api, namespace: shop, ready: true }
  - name: the shop database exists
    after: environment
    kubectl:
      exec:
        - name: postgres-1                 # CloudNativePG's first instance
          namespace: data
          command: ["psql", "-tAc", "select 1 from pg_database where datname = 'shop'"]
          equals: "1"
```

The `shop-db` Secret holds `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DATABASE` and `POSTGRES_URL`.

| Service | What runs | Address | Details |
| --- | --- | --- | --- |
| `postgres` | PostgreSQL (CloudNativePG) | `postgres-rw.<ns>:5432` | host, port, user, password, database, url |
| `mariadb` | MariaDB, MySQL protocol (mariadb-operator) | `mariadb.<ns>:3306` | host, port, user, password, database, url (`mysql://`) |
| `rabbitmq` | RabbitMQ (Cluster and Topology Operators) | `rabbitmq.<ns>:5672` | host, port, user, password, vhost, url |
| `redis` | Redis (OT Redis Operator) | `redis.<ns>:6379` | host, port, password, url |
| `kafka` | Kafka (Strimzi), with its `topics` | `kafka-kafka-bootstrap.<ns>:9092` | bootstrap |

- **Settings:** each service takes `user`, `database`, `password`, `storage` and `replicas`, with test-sized defaults (user and database `app`, one replica, `1Gi`). `rabbitmq` adds `vhosts`, `kafka` adds `topics`. Instances live in `ecosystem.namespace` (default `data`).
- **Passwords** are generated per run unless given, so the details are known before anything starts: nothing is read back from the cluster.
- **`secrets.export`** writes every detail of the listed services as `<SERVICE>_<FIELD>` into one Secret. **`secrets.define`** writes Secrets with your own key names, each `service.field`: an app that expects MySQL variables maps `MYSQL_HOST: mariadb.host`.
- **References:** every detail is also a context, read where `${{ env.NAME }}` is: `${{ ecosystem.postgres.url }}` in a command, a step, an expectation or an image's `deploy.env`, and `{{ .ecosystem.postgres.url }}` in templates and CEL. A service you didn't declare, or a field it doesn't have, fails `jig validate`, which lists the fields there are. A value put in `deploy.env` is plain text in the Deployment; `envFrom` keeps it in a Secret.
- **Order:** the Secrets are written after the images are loaded and before any image is deployed: a Deployment always comes after the Secrets it reads.
- **Operators** are installed only for the services declared, at versions pinned per jig release. On an existing cluster, an operator already running is used as it is and left alone. Namespaces jig creates go with the run; existing ones stay.
- **The chart:** the instances come from the `data-services` chart, pinned per jig release (`oci://ghcr.io/rollwright/charts/data-services`). `jig run --ecosystem-chart <oci ref or directory>` installs another, a local checkout for chart work.
- **Readiness:** a run goes on once every instance reports ready (a Postgres cluster's `Ready`, a RabbitMQ cluster's `AllReplicasReady`, the Redis pod), so a workload deployed after it finds its services.
- **Imports on one cluster share one ecosystem:** one plan (one set of passwords), installed once and removed after the last import. An import that declares a different ecosystem on a shared cluster is an error (one instance per service): give it `freshCluster: true`.

See [`examples/ecosystem`](../examples/ecosystem/jig.yaml).

---

Back to the [README](../README.md).
