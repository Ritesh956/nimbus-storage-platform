// Day 15 deliverable (docs/09-roadmap.md, docs/01-srs.md NFR-4): p95 API
// latency < 200ms for metadata operations under load. scripts/load-upload.js
// already load-tests the full chunked-upload flow (NFR-2) but its own p95
// (~735ms, see docs/00-project-state.md) blends 5 API calls + 2 MinIO PUTs
// per iteration — it was never a measurement of NFR-4, just mistaken for
// one. This isolates a single cheap metadata endpoint
// (GET /v1/orgs/{orgId}/folders) at the same concurrency NFR-2 requires
// (60 VUs), so the p95 reported here is actually comparable to the 200ms
// bound.
//
// Run with (from repo root):
//   docker run --rm --network host -e NIMBUS_BASE_URL=http://localhost:8080 \
//     -v "${PWD}/scripts:/scripts" grafana/k6 run /scripts/load-metadata.js
import http from 'k6/http';
import { check } from 'k6';

const BASE = __ENV.NIMBUS_BASE_URL || 'http://localhost:8080';
const PASSWORD = 'correct-horse-battery-staple';

export const options = {
  scenarios: {
    concurrent_metadata_reads: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '10s', target: 60 },
        { duration: '20s', target: 60 },
        { duration: '10s', target: 0 },
      ],
      gracefulRampDown: '10s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    checks: ['rate>0.99'],
    'http_req_duration{endpoint:list_folders}': ['p(95)<200'],
  },
};

function jsonHeaders(access) {
  return { headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${access}` } };
}

// setup() seeds one org with a handful of folders so the list endpoint has
// real (if small) rows to return, not an empty-table best case.
export function setup() {
  const email = `k6-metadata-${Date.now()}@nimbus.test`;
  const registerRes = http.post(`${BASE}/v1/auth/register`, JSON.stringify({ email, password: PASSWORD }), {
    headers: { 'Content-Type': 'application/json' },
  });
  if (registerRes.status !== 201 && registerRes.status !== 200) {
    throw new Error(`setup: register failed: ${registerRes.status} ${registerRes.body}`);
  }

  const loginRes = http.post(`${BASE}/v1/auth/login`, JSON.stringify({ email, password: PASSWORD }), {
    headers: { 'Content-Type': 'application/json' },
  });
  const access = loginRes.json('access_token');
  if (!access) throw new Error(`setup: login failed: ${loginRes.status} ${loginRes.body}`);

  const orgRes = http.post(`${BASE}/v1/orgs`, JSON.stringify({ name: 'k6 Metadata Load Test Org' }), jsonHeaders(access));
  const orgId = orgRes.json('id');
  if (!orgId) throw new Error('setup: could not create org');

  for (let i = 0; i < 10; i++) {
    http.post(`${BASE}/v1/orgs/${orgId}/folders`, JSON.stringify({ name: `folder-${i}` }), jsonHeaders(access));
  }

  return { access, orgId };
}

export default function (data) {
  const res = http.get(`${BASE}/v1/orgs/${data.orgId}/folders`, {
    headers: jsonHeaders(data.access).headers,
    tags: { endpoint: 'list_folders' },
  });
  check(res, { 'list folders 200': (r) => r.status === 200 });
}
