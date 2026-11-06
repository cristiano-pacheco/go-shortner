import http from 'k6/http';
import { check, sleep } from 'k6';

const baseURL = __ENV.BASE_URL || 'http://proxy:8080';
const destination = 'https://example.com/load-test';
const updatedDestination = 'https://example.org/load-test';
const headers = { 'Content-Type': 'application/json', 'X-API-Key': __ENV.API_KEY };

const workload = __ENV.WORKLOAD || 'mixed';
if (!['mixed', 'read-heavy', 'redirect', 'list'].includes(workload)) {
  throw new Error('WORKLOAD must be mixed, read-heavy, redirect or list');
}
function integerSetting(name, fallback, minimum, maximum = Number.MAX_SAFE_INTEGER) {
  const value = Number(__ENV[name] || fallback);
  if (!Number.isSafeInteger(value) || value < minimum || value > maximum) {
    throw new Error(`${name} must be an integer between ${minimum} and ${maximum}`);
  }
  return value;
}

const redirectRepeats = integerSetting('REDIRECT_REPEATS', workload === 'read-heavy' ? 20 : workload === 'redirect' ? 1 : 2, workload === 'redirect' || workload === 'list' ? 1 : 2, 1000);
const redirectBatchSize = integerSetting('REDIRECT_BATCH_SIZE', 1, 1, 200);
const hotSet = integerSetting('HOT_SET', 1000, 1);
const redirectMethod = __ENV.REDIRECT_METHOD || '';
if (!['', 'GET', 'HEAD'].includes(redirectMethod) || (redirectMethod && workload !== 'redirect')) {
  throw new Error('REDIRECT_METHOD must be GET or HEAD and is only supported for WORKLOAD=redirect');
}
const operations = workload === 'mixed' || workload === 'read-heavy'
  ? ['create', 'get', 'list', 'update', 'redirect_get', 'redirect_head', 'delete']
  : workload === 'list' ? ['list'] : redirectMethod ? [`redirect_${redirectMethod.toLowerCase()}`] : ['redirect_get', 'redirect_head'];
const operationThresholds = {};
for (const operation of operations) {
  operationThresholds[`checks{operation:${operation}}`] = ['rate>0.99'];
  operationThresholds[`http_req_duration{operation:${operation}}`] = ['p(99)<500'];
}

const vus = integerSetting('VUS', 10, 1);
const duration = __ENV.DURATION || '2m';
const rate = integerSetting('RATE', 0, 0);
const rateStages = __ENV.RATE_STAGES ? JSON.parse(__ENV.RATE_STAGES) : null;
if (rateStages && (!Array.isArray(rateStages) || rateStages.length === 0 || rate === 0 ||
  rateStages.some((stage) => !Number.isSafeInteger(stage.target) || stage.target < 1 || !/^\d+s$/.test(stage.duration)))) {
  throw new Error('RATE_STAGES requires RATE > 0 and stages with positive integer targets and durations in seconds');
}
const warmupRequests = integerSetting('WARMUP_REQUESTS', 0, 0);
const maxVUs = integerSetting('MAX_VUS', vus * 4, rate > 0 ? vus : 1);
const requestsPerIteration = workload === 'list' ? 1 : workload === 'redirect' ? redirectRepeats : 5 + redirectRepeats;
export const options = {
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
  batch: 200,
  batchPerHost: 200,
  ...(rate > 0 ? {
    scenarios: {
      offered_load: {
        executor: rateStages ? 'ramping-arrival-rate' : 'constant-arrival-rate',
        ...(rateStages ? { startRate: rate, stages: rateStages } : { rate, duration }),
        timeUnit: '1s',
        preAllocatedVUs: vus, maxVUs,
      },
    },
  } : { vus, duration }),
  tags: { phase: 'workload' },
  thresholds: {
    'http_req_failed{phase:workload}': ['rate<0.01'],
    checks: ['rate>0.99'],
    ...operationThresholds,
    ...(rate > 0 ? { dropped_iterations: ['count==0'] } : {}),
    'http_req_duration{phase:workload}': ['p(99)<500'],
  },
};

function json(response) {
  try {
    return response.json();
  } catch (_) {
    return {};
  }
}

export function setup() {
  const urls = [`${baseURL}/health`];
  if (__ENV.PUBLIC_HEALTH_URL) {
    urls.push(__ENV.PUBLIC_HEALTH_URL);
  }
  for (const url of urls) {
    let ready = false;
    for (let attempt = 0; attempt < 30; attempt++) {
      const response = http.get(url, { tags: { name: 'readiness', phase: 'readiness' } });
      if (response.status === 200) {
        ready = true;
        break;
      }
      sleep(1);
    }
    if (!ready) {
      throw new Error(`Server did not become ready: ${url}`);
    }
  }
  if (warmupRequests > 0 && workload === 'redirect') {
    // Prime the working set AND upstream connections before offered-rate timing.
    // These requests are excluded from workload latency/error thresholds.
    for (let start = 0; start < warmupRequests; start += 200) {
      const batch = [];
      for (let i = start; i < Math.min(start + 200, warmupRequests); i++) {
        batch.push({ method: redirectMethod || (i % 2 === 0 ? 'GET' : 'HEAD'), url: `${baseURL}/loadseed_${1 + i % hotSet}`,
          params: { redirects: 0, tags: { name: 'warmup', phase: 'warmup' } } });
      }
      for (const response of http.batch(batch)) {
        if (response.status !== 302) {
          throw new Error(`Redirect warmup failed: HTTP ${response.status}`);
        }
      }
    }
  }
}

export default function () {
  if (workload === 'redirect') {
    // A bounded, seeded hot set: no writes or redirects to external hosts.
    const n = 1 + ((__VU * 997 + __ITER) % hotSet);
    const url = `${baseURL}/loadseed_${n}`;
    redirectBurst(url, `https://example.com/load-seed/${n}`);
    return;
  }
  if (workload === 'list') {
    listLinks();
    return;
  }
  const create = http.post(`${baseURL}/api/v1/links`, JSON.stringify({ destination }), {
    headers, tags: { name: 'create link', operation: 'create' },
  });
  const link = json(create).data || {};
  const created = check(create, {
    'create returns 201 and a link': (r) => r.status === 201 && link.id > 0 && !!link.code && link.destination === destination,
  }, { operation: 'create' });
  if (!created) {
    return;
  }

  const linkURL = `${baseURL}/api/v1/links/${link.id}`;
  const get = http.get(linkURL, { headers, tags: { name: 'get link', operation: 'get' } });
  const fetched = json(get).data || {};
  check(get, {
    'get returns the created link': (r) => r.status === 200 && fetched.id === link.id && fetched.code === link.code && fetched.destination === destination,
  }, { operation: 'get' });

  listLinks();

  const update = http.patch(linkURL, JSON.stringify({ destination: updatedDestination, expires_at: null }), {
    headers, tags: { name: 'update link', operation: 'update' },
  });
  const updated = json(update).data || {};
  check(update, {
    'update changes the destination': (r) => r.status === 200 && updated.id === link.id && updated.code === link.code && updated.destination === updatedDestination,
  }, { operation: 'update' });

  const redirectURL = `${baseURL}/${link.code}`;
  // Repeated reads happen after the update and before deletion. Every response
  // must point to the updated destination, including concurrent cache fills.
  redirectBurst(redirectURL, updatedDestination);

  const deleted = http.del(linkURL, null, { headers, tags: { name: 'delete link', operation: 'delete' } });
  check(deleted, { 'delete returns 204': (r) => r.status === 204 }, { operation: 'delete' });
}

function redirectBurst(url, expectedDestination) {
  for (let start = 0; start < redirectRepeats; start += redirectBatchSize) {
    const requests = [];
    for (let offset = start; offset < Math.min(start + redirectBatchSize, redirectRepeats); offset++) {
      const method = redirectMethod || ((__ITER * redirectRepeats + offset) % 2 === 0 ? 'GET' : 'HEAD');
      const operation = method === 'GET' ? 'redirect_get' : 'redirect_head';
      requests.push({ method, url, params: { redirects: 0, tags: { name: `redirect ${method}`, operation } } });
    }
    // A batch of one uses the original sequential path for baseline comparisons.
    const responses = requests.length === 1
      ? [http.request(requests[0].method, url, null, requests[0].params)]
      : http.batch(requests);
    responses.forEach((response, index) => {
      const request = requests[index];
      check(response, {
        'redirect has the expected status and destination': (r) => r.status === 302 && r.headers.Location === expectedDestination,
        ...(request.method === 'HEAD' ? { 'HEAD has no response body': (r) => !r.body } : {}),
      }, { operation: request.params.tags.operation });
    });
  }
}

function listLinks() {
  const list = http.get(`${baseURL}/api/v1/links?page=1&page_size=20`, {
    headers, tags: { name: 'list links', operation: 'list' },
  });
  const page = json(list).data || {};
  check(list, {
    'list returns a page': (r) => r.status === 200 && Array.isArray(page.items) && page.page === 1 && page.page_size === 20 && page.total >= 1,
  }, { operation: 'list' });
}

export function handleSummary(data) {
  data.configuration = { workload, vus, duration, rate, rateStages, warmupRequests, maxVUs, hotSet, redirectMethod, redirectRepeats, redirectBatchSize, requestsPerIteration };
  const escaped = JSON.stringify(data, null, 2)
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  const rows = Object.entries(data.metrics).map(([name, metric]) => {
    const values = Object.entries(metric.values || {})
      .map(([key, value]) => `${key}: ${typeof value === 'number' ? Number(value.toFixed(3)) : value}`)
      .join(' · ');
    const thresholds = Object.entries(metric.thresholds || {})
      .map(([condition, result]) => `${condition}: ${result.ok ? 'PASS' : 'FAIL'}`).join(' · ');
    const safe = (text) => text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    return `<tr><th>${safe(name)}</th><td>${safe(values)}</td><td>${safe(thresholds)}</td></tr>`;
  }).join('');
  const html = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>k6 load test results</title><style>body{font:16px system-ui;max-width:1200px;margin:2rem auto;padding:0 1rem;color:#202b3b}table{border-collapse:collapse;width:100%}th,td{text-align:left;vertical-align:top;padding:.65rem;border-bottom:1px solid #ccc}pre{overflow:auto;background:#f4f4f4;padding:1rem} .table{overflow:auto}</style></head><body><h1>k6 load test results</h1><p>Durations are in milliseconds. Thresholds are evaluated by k6.</p><div class="table"><table><thead><tr><th>Metric</th><th>Values</th><th>Thresholds</th></tr></thead><tbody>${rows}</tbody></table></div><details><summary>Complete summary JSON</summary><pre>${escaped}</pre></details></body></html>`;
  return { '/results/index.html': html, '/results/summary.json': JSON.stringify(data, null, 2), stdout: `k6 summary written to /results/index.html\n` };
}
