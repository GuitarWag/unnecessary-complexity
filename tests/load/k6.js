import http from 'k6/http';
import { check } from 'k6';

const GATEWAY = __ENV.GATEWAY || 'http://localhost:18080';
// Optional: point read traffic at the resolver's direct HTTP redirect listener,
// bypassing the gateway hop. Writes still go through the gateway.
const READ_TARGET = __ENV.READ_TARGET || GATEWAY;
const SCENARIO = __ENV.SCENARIO || 'read';
const SEED_COUNT = parseInt(__ENV.SEED_COUNT || '500', 10);
const READ_RATE = parseInt(__ENV.READ_RATE || '5000', 10);
const WRITE_RATE = parseInt(__ENV.WRITE_RATE || '1000', 10);
const MIXED_RATE = parseInt(__ENV.MIXED_RATE || '4000', 10);
const DURATION = __ENV.DURATION || '30s';
const READ_RATIO = parseFloat(__ENV.READ_RATIO || '0.9');

const allScenarios = {
  read: {
    executor: 'constant-arrival-rate',
    rate: READ_RATE,
    timeUnit: '1s',
    duration: DURATION,
    preAllocatedVUs: 200,
    maxVUs: 2000,
    exec: 'readStorm',
  },
  write: {
    executor: 'constant-arrival-rate',
    rate: WRITE_RATE,
    timeUnit: '1s',
    duration: DURATION,
    preAllocatedVUs: 100,
    maxVUs: 1000,
    exec: 'writeSpike',
  },
  mixed: {
    executor: 'constant-arrival-rate',
    rate: MIXED_RATE,
    timeUnit: '1s',
    duration: DURATION,
    preAllocatedVUs: 200,
    maxVUs: 2000,
    exec: 'mixed',
  },
};

const selected = SCENARIO === 'all'
  ? allScenarios
  : { [SCENARIO]: allScenarios[SCENARIO] };

if (!selected[Object.keys(selected)[0]]) {
  throw new Error(`Unknown SCENARIO=${SCENARIO}; expected read|write|mixed|all`);
}

export const options = {
  scenarios: selected,
  thresholds: {
    http_req_failed: ['rate<0.05'],
    http_req_duration: ['p(95)<200', 'p(99)<500'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(95)', 'p(99)', 'p(99.9)', 'max'],
};

const SHORTEN_URL = `${GATEWAY}/shortener.v1.ShortenerService/Shorten`;
const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } };

function shorten(longUrl) {
  return http.post(SHORTEN_URL, JSON.stringify({ longUrl }), JSON_HEADERS);
}

export function setup() {
  const needsCodes = SCENARIO === 'read' || SCENARIO === 'mixed' || SCENARIO === 'all';
  if (!needsCodes) return { codes: [] };

  console.log(`seeding ${SEED_COUNT} short codes against ${GATEWAY}`);
  const codes = [];
  const stamp = Date.now();
  for (let i = 0; i < SEED_COUNT; i++) {
    const res = shorten(`https://example.com/load/${stamp}-${i}`);
    if (res.status !== 200) {
      throw new Error(`seed shorten failed at i=${i}: status=${res.status} body=${res.body}`);
    }
    codes.push(JSON.parse(res.body).url.code);
  }
  console.log(`seeded ${codes.length} codes; first=${codes[0]} last=${codes[codes.length - 1]}`);

  // Wait for the resolver to catch up via Kafka before we start hammering reads.
  // Poll the *last* seeded code (worst case for propagation) against READ_TARGET.
  const propagationDeadline = Date.now() + 30000;
  let ready = false;
  while (Date.now() < propagationDeadline) {
    const probe = http.get(`${READ_TARGET}/${codes[codes.length - 1]}`, { redirects: 0 });
    if (probe.status === 302) {
      ready = true;
      break;
    }
  }
  if (!ready) {
    throw new Error('resolver did not catch up within 30s of seeding');
  }
  console.log('resolver caught up; starting load');
  return { codes };
}

export function readStorm(data) {
  const code = data.codes[Math.floor(Math.random() * data.codes.length)];
  const res = http.get(`${READ_TARGET}/${code}`, { redirects: 0 });
  check(res, { 'redirect 302': (r) => r.status === 302 });
}

export function writeSpike() {
  const longUrl = `https://example.com/load/${__VU}-${__ITER}-${Math.random()}`;
  const res = shorten(longUrl);
  check(res, { 'shorten 200': (r) => r.status === 200 });
}

export function mixed(data) {
  if (Math.random() < READ_RATIO && data.codes.length > 0) {
    readStorm(data);
  } else {
    writeSpike();
  }
}
