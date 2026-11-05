import http from 'k6/http';
import { check, sleep } from 'k6';

const baseURL = __ENV.BASE_URL || 'http://proxy:8080';
const destination = 'https://example.com/load-test';
const updatedDestination = 'https://example.org/load-test';
const headers = { 'Content-Type': 'application/json', 'X-API-Key': __ENV.API_KEY };

const workload = __ENV.WORKLOAD || 'mixed';
if (!['mixed', 'redirect', 'list'].includes(workload)) {
  throw new Error('WORKLOAD must be mixed, redirect or list');
}
const operations = workload === 'mixed'
  ? ['create', 'get', 'list', 'update', 'redirect_get', 'redirect_head', 'delete']
  : workload === 'list' ? ['list'] : ['redirect_get', 'redirect_head'];
const operationThresholds = {};
for (const operation of operations) {
  operationThresholds[`checks{operation:${operation}}`] = ['rate>0.99'];
  operationThresholds[`http_req_duration{operation:${operation}}`] = ['p(95)<500'];
}

const vus = Number(__ENV.VUS || 10);
const duration = __ENV.DURATION || '2m';
const rate = Number(__ENV.RATE || 0);
const warmupRequests = Number(__ENV.WARMUP_REQUESTS || 0);
export const options = {
  batch: 200,
  batchPerHost: 200,
  ...(rate > 0 ? {
    scenarios: {
      offered_load: {
        executor: 'constant-arrival-rate', rate, timeUnit: '1s', duration,
        preAllocatedVUs: vus, maxVUs: Number(__ENV.MAX_VUS || vus * 4),
      },
    },
  } : { vus, duration }),
  tags: { phase: 'workload' },
  thresholds: {
    'http_req_failed{phase:workload}': ['rate<0.01'],
    checks: ['rate>0.99'],
    ...operationThresholds,
    ...(rate > 0 ? { dropped_iterations: ['count==0'] } : {}),
    'http_req_duration{phase:workload}': ['p(95)<500'],
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
    const hotSet = Number(__ENV.HOT_SET || 1000);
    for (let start = 0; start < warmupRequests; start += 200) {
      const batch = [];
      for (let i = start; i < Math.min(start + 200, warmupRequests); i++) {
        batch.push({ method: i % 2 === 0 ? 'GET' : 'HEAD', url: `${baseURL}/loadseed_${1 + i % hotSet}`,
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
    const n = 1 + ((__VU * 997 + __ITER) % Number(__ENV.HOT_SET || 1000));
    const url = `${baseURL}/loadseed_${n}`;
    const operation = __ITER % 2 === 0 ? 'redirect_get' : 'redirect_head';
    const response = operation === 'redirect_get'
      ? http.get(url, { redirects: 0, tags: { name: 'redirect GET', operation } })
      : http.head(url, { redirects: 0, tags: { name: 'redirect HEAD', operation } });
    check(response, {
      'seeded link redirects correctly': (r) => r.status === 302 && r.headers.Location === `https://example.com/load-seed/${n}`,
    }, { operation });
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
  const redirect = http.get(redirectURL, { redirects: 0, tags: { name: 'redirect GET', operation: 'redirect_get' } });
  check(redirect, {
    'GET redirects to updated destination': (r) => r.status === 302 && r.headers.Location === updatedDestination,
  }, { operation: 'redirect_get' });

  const head = http.head(redirectURL, { redirects: 0, tags: { name: 'redirect HEAD', operation: 'redirect_head' } });
  check(head, {
    'HEAD returns redirect headers without a body': (r) => r.status === 302 && r.headers.Location === updatedDestination && !r.body,
  }, { operation: 'redirect_head' });

  const deleted = http.del(linkURL, null, { headers, tags: { name: 'delete link', operation: 'delete' } });
  check(deleted, { 'delete returns 204': (r) => r.status === 204 }, { operation: 'delete' });
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
  data.configuration = { workload, vus, duration, rate, warmupRequests, maxVUs: Number(__ENV.MAX_VUS || vus * 4), hotSet: Number(__ENV.HOT_SET || 1000) };
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
