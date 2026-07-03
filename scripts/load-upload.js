// Day 12 deliverable (docs/09-roadmap.md): k6 load test proving NFR-2
// (>=50 concurrent uploads). Drives the real chunked-upload flow
// (chunks/check -> init upload -> chunk init -> presigned PUT -> commit ->
// complete) per virtual user, not a coarser proxy endpoint, so it actually
// exercises FR-6..9 under concurrency.
//
// Uses a smaller-than-production chunk size (256 KiB vs. the real 8 MiB
// default) deliberately: this test is about proving *concurrent* upload
// sessions hold up, which smoke-upload.js doesn't check — it already
// covers real 8 MiB multi-chunk behavior sequentially, so re-proving
// chunking here would just add runtime without adding coverage.
//
// Run with (from repo root):
//   docker run --rm --network host -e NIMBUS_BASE_URL=http://localhost:8080 \
//     -v "${PWD}/scripts:/scripts" grafana/k6 run /scripts/load-upload.js
//
// --network host is required, not optional: nimbus-api hands back presigned
// MinIO URLs signed against PublicEndpoint (http://localhost:900x, per
// deploy/docker-compose.yml's NIMBUS_STORAGE_NODES) — those only resolve
// correctly from the same host Docker Compose publishes the MinIO ports
// to. A k6 container on the compose network's internal DNS (nimbus_default)
// can reach nimbus-api by service name, but "localhost:900x" inside that
// container means the container itself, not the host — the PUT step fails
// with connection-refused. This cost real debugging time; don't re-litigate
// it, see docs/00-project-state.md's footgun notes.
import http from 'k6/http';
import crypto from 'k6/crypto';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';

const BASE = __ENV.NIMBUS_BASE_URL || 'http://localhost:8080';
const CHUNK_SIZE = 256 * 1024;
const PASSWORD = 'correct-horse-battery-staple';

const uploadDuration = new Trend('nimbus_upload_duration_ms', true);
const uploadFailures = new Counter('nimbus_upload_failures');

export const options = {
  scenarios: {
    concurrent_uploads: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '10s', target: 60 }, // ramp past the NFR-2 floor of 50
        { duration: '20s', target: 60 }, // hold at 60 concurrent uploaders
        { duration: '10s', target: 0 },  // ramp down
      ],
      gracefulRampDown: '10s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    checks: ['rate>0.99'],
  },
};

function jsonHeaders(access) {
  return { headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${access}` } };
}

// setup() runs once before VUs start: a single shared user/org/folder is
// enough here — NFR-2 is about concurrent *uploads* holding up, not
// multi-tenant isolation, which smoke-folders.sh/smoke-auth.sh already
// cover. The access token's 15 min TTL comfortably outlives this test's
// ~40s run.
export function setup() {
  const email = `k6-load-${Date.now()}@nimbus.test`;
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

  const orgRes = http.post(`${BASE}/v1/orgs`, JSON.stringify({ name: 'k6 Load Test Org' }), jsonHeaders(access));
  const orgId = orgRes.json('id');
  const folderRes = http.post(`${BASE}/v1/orgs/${orgId}/folders`, JSON.stringify({ name: 'k6-uploads' }), jsonHeaders(access));
  const folderId = folderRes.json('id');
  if (!orgId || !folderId) throw new Error('setup: could not create org/folder');

  return { access, folderId };
}

export default function (data) {
  const auth = jsonHeaders(data.access);
  const start = Date.now();
  let ok = true;

  const buf = crypto.randomBytes(CHUNK_SIZE); // unique per iteration -> never dedups, so this measures real writes
  const hash = crypto.sha256(buf, 'hex');
  const name = `k6-load-${__VU}-${__ITER}-${Date.now()}.bin`;

  let res = http.post(`${BASE}/v1/chunks/check`, JSON.stringify({ hashes: [hash] }), auth);
  ok = check(res, { 'chunks/check 200': (r) => r.status === 200 }) && ok;

  res = http.post(
    `${BASE}/v1/uploads`,
    JSON.stringify({ folder_id: data.folderId, name, size_bytes: buf.byteLength, mime_type: 'application/octet-stream' }),
    auth,
  );
  ok = check(res, { 'init upload 201': (r) => r.status === 201 }) && ok;
  const uploadId = res.json('upload_id');

  res = http.post(`${BASE}/v1/uploads/${uploadId}/chunks/${hash}/init`, null, auth);
  ok = check(res, { 'chunk init 200': (r) => r.status === 200 }) && ok;
  const targets = res.json('targets') || [];
  ok = check(targets, { 'chunk init returned targets': (t) => t.length >= 1 }) && ok;

  const etags = {};
  for (const t of targets) {
    const putRes = http.put(t.put_url, buf);
    ok = check(putRes, { 'chunk PUT 200': (r) => r.status === 200 }) && ok;
    etags[t.node_id] = putRes.headers['Etag'] || putRes.headers['ETag'];
  }

  res = http.post(`${BASE}/v1/uploads/${uploadId}/chunks/${hash}/commit`, JSON.stringify({ size_bytes: buf.byteLength, etags }), auth);
  ok = check(res, { 'commit 200': (r) => r.status === 200 }) && ok;

  res = http.post(
    `${BASE}/v1/uploads/${uploadId}/complete`,
    JSON.stringify({ chunk_order: [hash], size_bytes: buf.byteLength, checksum_sha256: hash }),
    { headers: { ...auth.headers, 'Idempotency-Key': `k6-${uploadId}` } },
  );
  ok = check(res, { 'complete 201': (r) => r.status === 201 }) && ok;

  uploadDuration.add(Date.now() - start);
  if (!ok) uploadFailures.add(1);
}
