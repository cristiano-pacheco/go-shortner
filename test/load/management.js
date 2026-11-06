import http from 'k6/http';
import { check, sleep } from 'k6';
import exec from 'k6/execution';
import { SharedArray } from 'k6/data';
import { Counter } from 'k6/metrics';

const baseURL = __ENV.BASE_URL || 'http://proxy:8080';
const apiKey = __ENV.API_KEY;
if (!apiKey) throw new Error('API_KEY is required');
const scenario = __ENV.SCENARIO || 'get';
const allowed = ['create', 'get', 'list', 'list_deep', 'update', 'delete', 'mixed'];
if (!allowed.includes(scenario)) throw new Error(`SCENARIO must be one of ${allowed.join(', ')}`);

function integer(name, fallback, minimum) {
  const value = Number(__ENV[name] === undefined ? fallback : __ENV[name]);
  if (!Number.isSafeInteger(value) || value < minimum) throw new Error(`${name} must be an integer >= ${minimum}`);
  return value;
}

const seconds = integer('DURATION_SECONDS', 300, 1);
const rate = integer('RATE', scenario === 'mixed' ? 200 : 1010, 1);
const warmupRequests = integer('WARMUP_REQUESTS', 1000, 0);
const seed = integer('SEED', 42, 0);
const vus = integer('VUS', 200, 1);
const maxVUs = integer('MAX_VUS', 1000, vus);
const dataFile = __ENV.DATA_FILE || '/results/management-dataset.json';
const operations = scenario === 'mixed' ? ['create', 'get', 'list', 'update', 'delete'] : [scenario];
const has = (operation) => operations.includes(operation);
function field(name) {
  return new SharedArray(`management-${name}`, () => {
    const value = JSON.parse(open(dataFile))[name];
    if (!Array.isArray(value)) throw new Error(`${name} must be an array in ${dataFile}`);
    return value;
  });
}
const metadata = new SharedArray('management-metadata', () => {
  const total = JSON.parse(open(dataFile)).visible_count;
  if (!Number.isSafeInteger(total) || total < 1) throw new Error('dataset total must be a positive integer');
  return [total];
});
const total = metadata[0];
const ids = has('get') ? field('read_ids') : [];
const updateIds = has('update') ? field('update_ids') : [];
const deleteIds = has('delete') ? field('delete_ids') : [];
if (has('get') && ids.length === 0) throw new Error('ids must not be empty');
if (has('update') && (updateIds.length < rate * seconds + warmupRequests)) {
  throw new Error('insufficient update IDs for the configured measurement and warmup');
}
if (has('delete') && (deleteIds.length < rate * seconds + warmupRequests)) {
  throw new Error('insufficient delete IDs for the configured measurement and warmup');
}

const scenarios = {};
const thresholds = { 'checks{phase:measurement}': ['rate==1'], 'http_req_failed{phase:measurement}': ['rate==0'], dropped_iterations: ['count==0'] };
for (const operation of operations) {
  scenarios[operation] = { executor: 'constant-arrival-rate', exec: 'runOperation', rate, timeUnit: '1s', duration: `${seconds}s`, preAllocatedVUs: vus, maxVUs, gracefulStop: '30s', tags: { operation, phase: 'measurement' } };
  thresholds[`http_req_duration{phase:measurement,operation:${operation}}`] = ['p(99)<=100'];
}
export const options = { scenarios, thresholds, setupTimeout: '30m', summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'] };
const started = new Counter('management_requests_started');
const completed = new Counter('management_requests_completed');
const windowStart = new Counter('management_window_start');
const outsideWindow = new Counter('management_iterations_outside_window');
const headers = { 'Content-Type': 'application/json', 'X-API-Key': apiKey };
const destination = 'https://example.com/load-test';
const updatedDestination = 'https://example.org/load-test';
const deepPage = Math.floor(total * 0.9 / 20) + 1;

function body(response) {
  try { return response.json().data || {}; } catch (_) { return {}; }
}

// A permutation of array positions avoids relying on contiguous database IDs.
// The preparation step supplies uniformly distributed IDs in seeded order.
function readIndex(index) {
  return (index + seed) % ids.length;
}

function request(operation, index, phase) {
  const tags = { operation, phase, name: `management ${operation}` };
  const params = { headers, tags, redirects: 0 };
  const isWarmup = phase === 'warmup';
  let response;
  let expectedId;
  let page;
  started.add(1, tags);
  switch (operation) {
    case 'create':
      response = http.post(`${baseURL}/api/v1/links`, JSON.stringify({ destination }), params);
      break;
    case 'get':
      expectedId = Number(ids[readIndex(isWarmup ? Math.floor(index * ids.length / warmupRequests) : index)]);
      response = http.get(`${baseURL}/api/v1/links/${expectedId}`, params);
      break;
    case 'list':
    case 'list_deep':
      page = operation === 'list' ? 1 : deepPage;
      response = http.get(`${baseURL}/api/v1/links?page=${page}&page_size=20`, params);
      break;
    case 'update':
      expectedId = Number(updateIds[index + (isWarmup ? 0 : warmupRequests)]);
      response = http.patch(`${baseURL}/api/v1/links/${expectedId}`, JSON.stringify({ destination: updatedDestination, expires_at: null }), params);
      break;
    case 'delete':
      expectedId = Number(deleteIds[index + (isWarmup ? 0 : warmupRequests)]);
      response = http.del(`${baseURL}/api/v1/links/${expectedId}`, null, params);
      break;
  }
  completed.add(1, tags);
  const data = body(response);
  const valid = check(response, {
    'management response status and content': (r) => {
      if (operation === 'create') return r.status === 201 && data.id > 0 && !!data.code && data.destination === destination;
      if (operation === 'get') return r.status === 200 && data.id === expectedId && !!data.code && typeof data.destination === 'string';
      if (operation === 'update') return r.status === 200 && data.id === expectedId && !!data.code && data.destination === updatedDestination;
      if (operation === 'delete') return r.status === 204;
      return r.status === 200 && Array.isArray(data.items) && data.page === page && data.page_size === 20 &&
        Number.isSafeInteger(data.total) && data.total >= 0 && (scenario === 'mixed' || data.total === total) &&
        data.items.length === Math.min(20, Math.max(0, data.total - (page - 1) * 20));
    },
  }, tags);
  if (isWarmup && !valid) throw new Error(`Warmup ${operation} failed with HTTP ${response.status}`);
}

export function setup() {
  let ready = false;
  for (let attempt = 0; attempt < 30; attempt++) {
    if (http.get(`${baseURL}/health`, { tags: { phase: 'readiness', name: 'readiness' } }).status === 200) {
      ready = true;
      break;
    }
    sleep(1);
  }
  if (!ready) throw new Error('Management server did not become ready');
  console.log(`Warmup: ${warmupRequests} requests per operation, seed ${seed}, dataset ${dataFile}`);
  for (const operation of operations) {
    for (let i = 0; i < warmupRequests; i++) request(operation, i, 'warmup');
  }
}

export function runOperation() {
  if (exec.scenario.iterationInTest === 0) {
    windowStart.add(1, { phase: 'measurement', operation: exec.scenario.name, start_ms: String(exec.scenario.startTime), end_ms: String(exec.scenario.startTime + seconds * 1000) });
  }
  // The arrival-rate executor can start an extra iteration at the end boundary.
  // It is not part of the measured offer and must not consume a reserved ID.
  if (Date.now() >= exec.scenario.startTime + seconds * 1000 || exec.scenario.iterationInTest >= rate * seconds) {
    outsideWindow.add(1, { operation: exec.scenario.name, phase: 'measurement' });
    return;
  }
  request(exec.scenario.name, exec.scenario.iterationInTest, 'measurement');
}

export function handleSummary(data) {
  data.configuration = { scenario, ratePerOperation: rate, durationSeconds: seconds, warmupRequestsPerOperation: warmupRequests, seed, total, deepPage, requestsPerIteration: 1, preAllocatedVUs: vus, maxVUs };
  return { '/results/summary.json': JSON.stringify(data, null, 2), stdout: 'Management summary written to /results/summary.json\n' };
}
