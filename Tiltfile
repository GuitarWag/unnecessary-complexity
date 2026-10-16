# Local dev: Kafka, Postgres and Kafka Connect (Debezium) run in Docker Compose. The Go
# services and the frontend dev servers run on the host and restart when their code changes.
#
#   tilt up                              everything
#   tilt up -- --frontend=minimal        only shell + mf-url-input
#   tilt up -- --frontend=none           backends only
#   tilt down                            stop everything (compose volumes are kept)

config.define_string('frontend')
cfg = config.parse()
frontend = cfg.get('frontend', 'all')

ROOT = config.main_dir
DATA = ROOT + '/.run/data'
local('mkdir -p ' + DATA, quiet=True)

# ----- Infra -----

docker_compose('compose.yaml', project_name='url-shortener-dev')
dc_resource('kafka', labels=['infra'])
dc_resource('postgres', labels=['infra'])
dc_resource('connect', resource_deps=['kafka'], labels=['infra'])

# apache/kafka does not auto-create topics for consumers. Same list as the
# kafka-topics Job in k8s/base/kafka/kafka.yaml.
local_resource(
    'kafka-topics',
    cmd='until docker compose exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --list >/dev/null 2>&1; ' +
        'do sleep 2; done; for t in short_urls.created clicks.recorded; do ' +
        'docker compose exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 ' +
        '--create --if-not-exists --topic "$t" --partitions 1 --replication-factor 1; done',
    resource_deps=['kafka'],
    labels=['infra'],
)

# The shortener creates the outbox publication at startup, and the connector needs
# it. PUT is create-or-update, so an edit to the JSON re-registers the connector.
local_resource(
    'debezium-connector',
    cmd='curl -fsS --retry 60 --retry-delay 2 --retry-all-errors -X PUT ' +
        '-H "Content-Type: application/json" --data @k8s/base/debezium/shortener-outbox.json ' +
        'http://localhost:8083/connectors/shortener-outbox/config',
    deps=['k8s/base/debezium/shortener-outbox.json'],
    resource_deps=['connect', 'shortener'],
    links=[link('http://localhost:8083/connectors/shortener-outbox/status', 'connector status')],
    labels=['infra'],
)

# ----- Go services -----

def go_service(name, port, env, deps=[], probe_path=None, links=[]):
    if probe_path:
        readiness = probe(http_get=http_get_action(port=port, path=probe_path))
    else:
        readiness = probe(tcp_socket=tcp_socket_action(port=port))
    local_resource(
        name,
        serve_cmd='go run ./cmd/server',
        serve_dir='services/' + name,
        serve_env=env,
        deps=['services/' + name, 'services/proto', 'services/platform-events'],
        ignore=['**/*_test.go'],
        resource_deps=deps,
        readiness_probe=readiness,
        links=links,
        labels=['backend'],
    )

go_service('shortener', 50051, {
    'SHORTENER_ADDR': ':50051',
    'SHORTENER_DATABASE_URL': 'postgres://postgres:postgres@localhost:5432/shortener?sslmode=disable',
    'SHORTENER_BATCH_SIZE': '100',
    'SHORTENER_BATCH_TIMEOUT': '10ms',
}, deps=['postgres'])

go_service('resolver', 50052, {
    'RESOLVER_ADDR': ':50052',
    'RESOLVER_HTTP_ADDR': ':50080',
    'RESOLVER_DB': DATA + '/resolver.db',
    'RESOLVER_KAFKA_BROKERS': 'localhost:9092',
}, deps=['kafka-topics'])

go_service('analytics', 50053, {
    'ANALYTICS_ADDR': ':50053',
    'ANALYTICS_DB': DATA + '/analytics.db',
    'ANALYTICS_KAFKA_BROKERS': 'localhost:9092',
}, deps=['kafka-topics'])

go_service('loadgen', 50054, {
    'LOADGEN_ADDR': ':50054',
    'LOADGEN_TARGET': 'http://localhost:8080',
    'LOADGEN_RESOLVER_TARGET': 'http://localhost:50080',
})

FRONTEND_PORTS = {
    'shell': 5173,
    'mf-url-input': 5174,
    'mf-create-button': 5175,
    'mf-copy-button': 5176,
    'mf-url-list': 5177,
    'mf-analytics-chart': 5178,
    'mf-load-test': 5179,
}

go_service('gateway', 8080, {
    'GATEWAY_ADDR': ':8080',
    'GATEWAY_SHORTENER_ADDR': 'localhost:50051',
    'GATEWAY_RESOLVER_ADDR': 'localhost:50052',
    'GATEWAY_ANALYTICS_ADDR': 'localhost:50053',
    'GATEWAY_LOADGEN_ADDR': 'localhost:50054',
    'GATEWAY_ALLOWED_ORIGINS': ','.join(['http://localhost:%d' % p for p in FRONTEND_PORTS.values()]),
}, deps=['shortener', 'resolver', 'analytics', 'loadgen'], probe_path='/healthz',
   links=[link('http://localhost:8080/healthz', 'healthz')])

# ----- Frontend -----

if frontend == 'all':
    apps = list(FRONTEND_PORTS.keys())
elif frontend == 'minimal':
    apps = ['shell', 'mf-url-input']
elif frontend == 'none':
    apps = []
else:
    fail('--frontend must be all, minimal or none, got: ' + frontend)

if apps:
    local_resource(
        'pnpm-install',
        cmd='pnpm install --prefer-offline',
        dir='frontend',
        deps=['frontend/package.json', 'frontend/pnpm-lock.yaml'],
        labels=['frontend'],
    )

# rsbuild has its own hot reload, so these resources watch no files.
for app in apps:
    local_resource(
        app,
        serve_cmd='pnpm --filter @url-shortener/%s run dev' % app,
        serve_dir='frontend',
        resource_deps=['pnpm-install'],
        readiness_probe=probe(tcp_socket=tcp_socket_action(port=FRONTEND_PORTS[app])),
        links=[link('http://localhost:%d' % FRONTEND_PORTS[app], app)],
        labels=['frontend'],
    )
