# Writing a jig

A jig is one file: a Kubernetes environment and the checks that prove it correct. Key by key: the environment it runs in, the steps that prepare it, the checks, and imports.

## Environment

The cluster and what the workload depends on. Nothing here is the thing under test.

- **`cluster`:** `provider: kind` creates a cluster and deletes it after the run; `provider: none` runs without one, for commands only. `name` names it. Run against an existing context instead and jig deletes nothing it did not create.
- **`images`:** built and loaded into the cluster before anything references them. An entry is a local image (`shop-api:dev`, or just the reference as a string) or a `build` that produces one: `context` resolves against the jig's directory and `dockerfile` (default `Dockerfile`) against the context, as in Docker Compose; a build without `image` is named `jig-<name>-<index>`. An entry can also `deploy` a Deployment and a Service for the common case. All images are checked before any is loaded, so a missing one fails before the cluster is touched. `jig run --images` narrows the list and never adds to it; with none declared, it is the list (the CI case, where the just-built image is the point).
- **Services from their repositories**, as Docker Compose runs them: an image can come from `git` (a remote `url`, cloned once into `~/.jig/cache/git` and fetched as `pull` says, `Always` by default or `IfNotPresent`; or a local path, built from its working tree as it is, or from a temporary worktree at a `branch` or `ref`). `build.context` and `dockerfile` then resolve inside the checkout. `needs` names the images whose Deployments must be ready first; images with nothing between them deploy together. `build.args` passes the Dockerfile's `ARG`s (`--build-arg`, values expanded like any other, `${{ env.VERSION }}`) and `build.target` picks a stage, as in Compose. jig installs git when an image uses it. `deploy.env` sets variables (`DATABASE_URL: ${{ ecosystem.postgres.url }}`, `AUTH_URL: http://auth.default:8080`) and `deploy.envFrom` loads Secrets whole. Images are deployed after the ecosystem's Secrets.

  ```yaml
  images:
    - name: auth
      git: { url: https://github.com/acme/auth.git, branch: feat/sign-up }
      build: { context: . }
      deploy: { ports: [8080], envFrom: [shop-deps] }
    - name: api
      git: { url: ../api }            # the working tree, uncommitted changes included
      build: { context: . }
      needs: [auth]
      deploy:
        ports: [8090]
        env: { AUTH_URL: "http://auth.default:8080", DATABASE_URL: "${{ ecosystem.postgres.url }}" }
  ```
- **`apply`, `helm`, `wait`:** cluster furniture, such as namespaces, a storage class or an ingress controller.
- **`ecosystem`:** the data services the workload uses, declared instead of installed by hand. See [ecosystem.md](ecosystem.md).

## Prepare

An ordered list of steps. Each has one action and an optional `name`, which expectations refer to.

| Action | Does |
| --- | --- |
| `apply` | Applies a manifest: one path, a list, or entries with their own `wait`. |
| `helm` | `helm upgrade --install`: a first run installs, a later one upgrades. |
| `delete` | Deletes what the files declare. It doesn't wait for finalizers: the expectations after it wait for what the deletion brings about. |
| `commands` | Runs commands from the jig's directory; an exit code other than the expected one (0 by default) fails the step. |

Any step can also take `wait` (resources to wait for after the action), `restart` (Deployments, StatefulSets or DaemonSets to roll out and wait for: for a workload reading a ConfigMap or Secret the step changed, which neither `kubectl apply` nor `helm upgrade` restarts), and `keep` (see [Imports](jig.md#imports)).

Prepare is a list, not fixed blocks, because order is the jig's: a chart may need manifests first, and a jig may act on the workload between checks.

### Deploying an image: `deploy`

`deploy` makes a Deployment and a Service for an image, written as Compose writes a service:

```yaml
deploy:
  ports: [8080, "8081:9090", { port: 9100, expose: false }]   # container port; host:container; container only
  replicas: 2
  env: { LOG_LEVEL: debug }
  envFile: .env                     # a Secret, loaded with envFrom
  envFrom: [shop-db]                # Secrets loaded whole
  healthcheck:
    test: ["CMD", "curl", "-f", "http://localhost:8080/health"]   # → an httpGet probe
    interval: 10s
    as: [startup, readiness]        # readiness when as is left out
  init:
    - command: ["./migrate", "up"]  # runs to completion before the app starts, the app's image
```

- **`ports`:** `8080` is the container port, forwarded to the same localhost port; `"8081:8080"` forwards localhost 8081 to it (Compose's order). The long form takes `port`, `host`, `forward: false` (a Service port, no forward) and `expose: false` (a container port only: no Service port, no forward).
- **`healthcheck`:** Compose's (`test`, `interval`, `timeout`, `retries`, `start_period`) as Kubernetes probes. A `curl`/`wget` of a localhost URL becomes an `httpGet` probe, so it works on images without curl; other `CMD`s run as `exec`, `CMD-SHELL` through `sh -c`. One check for several probes (`as`), or one per probe (`readiness:`, `liveness:`, `startup:`), not both.
- **`init`:** containers run to completion, in order, before the app's (Kubernetes' `initContainers`, where Compose needs a separate service and `depends_on`). Each needs a `command`; `image` defaults to the app's and is never built here (add an `images` entry for another); `env`, `envFrom` and `envFile` as the app's.
- **`envFile`:** one file or a list, as Compose's `env_file`: the variables become a Secret (`<name>-env`), a later file winning, values taken as written.

### One image, several rows: `forEach`

An image entry with `forEach` stands for one image per row, its values templates that read the row as `.item` (and `.index`, and `.<as>` when `as` names it):

```yaml
images:
  - name: "api-{{ .item.ref }}"
    forEach:
      items:
        - { ref: main,        port: 8080 }
        - { ref: release/1.3, port: 8081 }
    image: "api:{{ .item.port }}"
    git:    { url: https://github.com/org/api, ref: "{{ .item.ref }}" }
    build:  { context: . }
    deploy: { ports: ["{{ .item.port }}"] }
```

- **Rows:** `items` as written, or a `matrix` of values (every combination; keys in name order, the first varying slowest), less the `exclude` rows it matches, plus the `include` rows as written.
- **Names:** a templated `name` is resolved and slugified (`api-release-1-3`); without one, rows are `<base>-<entry>-<row>`, the base being `name`, or the repository's for an image from git. `needs: [api]` means every row of `api`.
- **When:** rows are made when the jig is read, so `jig validate` lists them. Only the row is there then: run values come in through `${{ env.… }}` and `${{ ecosystem.… }}`, which stay for the run.

### Includes for images, apply and helm

Wherever images, apply entries or Helm releases are listed (in `environment`, or in a step), an entry can be `include: <file>`, and the file's entries are expanded in its place. The file holds them under the same key, so one file works everywhere:

```yaml
environment:
  images:
    - include: ./e2e/images.yaml           # images: [ ... ]
  helm:
    - include: ./e2e/helm/cert-manager.yaml  # helm: [ ... ]
prepare:
  - name: chart
    helm: { include: ./e2e/helm/shop.yaml }  # helm: [ one release ]
```

- Paths in the file (a build context, a manifest, a local chart, a values file) resolve from the file, not from the jig that includes it.
- An entry with `include` sets nothing else, and a step's `helm` is one release, so its file must hold exactly one.
- An include file can include others; a file that includes itself is an error naming the chain.

The phases themselves take no include, so a jig always shows its environment, prepare and checks.

## Assert

`expect` is a list of named expectations. Each polls until it passes or its `timeout` runs out.

| Field | Meaning |
| --- | --- |
| `after` | When it runs: `environment`, `prepare` (the default: after the last step), or a step's name (right after that step). |
| `resources` | Resource checks: kind, name, namespace, count, readiness, field assertions. |
| `commands` | Commands with an expected exit code and assertions on their output. |
| `kubectl` | `get`, `logs`, `describe`, `exec`, `port-forward` and more, without writing the command. |
| `external` | Calls from the machine running jig (HTTP, Prometheus, Postgres, Redis, Mongo, Kafka) with assertions on what they return. |
| `when`, `or` | Conditions for running it at all. |
| `include` | A file of expectations, expanded in place. An include file can include others (one folder per area, say), each path resolved from the file that names it; a file that includes itself is an error. See [`examples/commands`](../examples/commands/checks.yaml). |
| `onFailure` | Diagnostics when it fails; `stop: true` ends the run after them. |
| `finally` | Side effects after it, whatever happened: undo what it changed. |

A failing expectation is recorded and later ones still run, unless `onFailure.stop` says otherwise. The jig's own `onFailure` runs once when the run fails, and its `finally` once at the end of every run.

### Asking about an object: `json:`, `expr:` and `match:`

Every assertion takes the same operators (`equals`, `prefix`, `contains`, `regex`, `greaterThan`, `between`, `in`, `exists`, and their negations) and `expr:`, an expression that must come out true, written as everywhere else: `${ … }` is CEL, `{{ … }}` a template, both with KEL's functions. `value` is what is asserted on (a number stays a number) and, under `json:`, `object` is the whole document.

```yaml
kubectl:
  get:
    - kind: Deployment
      name: shop-api
      namespace: shop
      json:
        - { path: .spec.replicas, equals: "2" }
        - { path: .status.readyReplicas, expr: "${ value == object.spec.replicas }" }
```

`match:` writes the same in the object's own shape, as KUTTL and Chainsaw tests do, and is turned into `json:` assertions, one per leaf: a literal is `equals`, a `${ … }` or `{{ … }}` value is `expr:`, lists are matched element by element, and keys not given are ignored.

```yaml
kubectl:
  get:
    - kind: Deployment
      name: shop-api
      namespace: shop
      match:
        spec:
          replicas: 2
          template:
            spec:
              containers:
                - name: api
                  image: '${ value.startsWith("shop-api:") }'
        status:
          readyReplicas: '${ value >= 2 }'
```

With a `labelSelector`, every object must match (`objects: all`, the default) or one (`objects: any`). A failure names the object and the path: `shop-api-7d9: json .spec.template.spec.containers[0].image: "shop-api:old" fails value.startsWith("shop-api:")`. `match` stands alone in its `get`; `jig validate` compiles every `expr:`.

### Over time and across the run

| Field | Where | Does |
| --- | --- | --- |
| `capture: NAME` | a `json:` entry | Saves the value at its path for later expectations: `${{ env.NAME }}`. |
| `stays: 2m` | expectation | After passing, the checks must keep passing for the window. |
| `settles: 15s` | `kubectl.get` (one object) | The object's `resourceVersion` stops changing for the window. |
| `snapshot: ./file.yaml` | `kubectl.get` (one object) | The object, without status and identity fields, equals the file. Written on the first run; `{file, update: true}` rewrites it on drift. |
| `order:` | expectation | Objects' timestamps come in the order listed: `[{kind, name, namespace, at: .metadata.creationTimestamp}, …]`. |
| `repeat: 50` | external call | Makes the call 50 times; the assertions read `{count, errors, p50, p95, p99, max, mean}` (ms). |
| `guards:` | jig | Checked at the end of the run: `events` (Warning events), `restarts`, `logs` (`notContains`). |

```yaml
expect:
  - name: before the upgrade
    after: install
    kubectl:
      get:
        - { kind: Deployment, name: api, namespace: shop, json: [{ path: .metadata.uid, capture: API_UID }] }
  - name: the upgrade kept the Deployment, and it stays ready
    after: upgrade
    stays: 1m
    kubectl:
      get:
        - kind: Deployment
          name: api
          namespace: shop
          settles: 10s
          json: [{ path: .metadata.uid, equals: "${{ env.API_UID }}" }]

guards:
  - name: no Warning events
    events: { namespaces: [shop] }
```

### Env

`env` declares variables for the run, on the jig and on each expectation. A value reads them as in GitHub Actions:

```yaml
env:
  HELLO: world

expect:
  - name: greets
    env:
      NAME: jig
    commands:
      - run: echo "$HELLO ${NAME}"                   # the shell expands both
    external:
      - { name: hello, url: "http://web/${{ env.HELLO }}" }  # ${{ env.NAME }}, as in Actions
    resources:
      - { kind: ConfigMap, name: "greeting-${HELLO}", namespace: default }
```

| Form | Expands in | From |
| --- | --- | --- |
| `${{ env.NAME }}` | Every value, commands included (before their shell runs, as in GitHub Actions) | The expectation's env, then the jig's, then the process environment. A name none of them sets fails the expectation, or the step, before it runs. |
| `${NAME}` | Every value but commands | The same, by jig |
| `$NAME`, `${NAME}` | Commands | Their shell: the env is exported to every command. Outside commands a bare `$` passes through, so `$patch` and passwords survive. |
| `{{ .env.NAME }}` | Templates | The same values |
| `${ env.NAME }` | CEL | The same values |

Templates (`{{ … }}`) and CEL (`${ … }`) use [KEL](https://github.com/intentd/kel)'s functions, the jig's `functions:` block, and any the caller supplies.

## Imports

`imports` runs other jigs after this one. Each is a whole run with its own environment, steps, expectations and result, listed under the parent's report.

```yaml
name: shop-suite
description: Every shop jig, on one cluster

imports:
  - ./api/jig.yaml                 # a path alone
  - path: ./worker/jig.yaml
    wait: 5s                       # pause before it starts
  - path: ./upgrade/jig.yaml
    freshCluster: true             # its own cluster
```

A jig with only `imports` is a suite: it lends its cluster to the imports that share one.

- `freshCluster: true` gives an import its own cluster; `context: <name>` runs it in that kubeconfig context, a cluster jig neither creates nor deletes (one or the other, not both). An import's env is its parent's, with its own winning. Otherwise it shares one: a parent that only imports lends its cluster; a parent that ran its own checks gives its imports a separate `<name>-imports` cluster; an existing context is used as is.
- A shared import undoes everything it did, Helm releases included, except the steps marked `keep: true`. What such a step applied and the release it installed stay for the imports after it: they upgrade the release with their own values, and the parent removes it all once, after the last import. Run on its own, the jig tears the step down as usual.

  ```yaml
  prepare:
    - name: platform
      helm: { chart: ./charts/platform, namespace: platform }
      keep: true      # the imports after this one reuse it
    - name: app
      helm: { chart: ./charts/app }
  ```

  Inrun marks its bundle and its own release this way, so each of its imports upgrades one Inrun.
- `wait` pauses before an import starts.

---

Back to the [README](../README.md).
