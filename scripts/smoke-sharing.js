#!/usr/bin/env node
// Day 7 deliverable (docs/09-roadmap.md): unauthenticated download via
// share link works; expired link rejected; non-member can't revoke.
'use strict';
const crypto = require('crypto');

const BASE = process.env.NIMBUS_BASE_URL || 'http://localhost:8080';
const sha256 = (buf) => crypto.createHash('sha256').update(buf).digest('hex');

async function jsonFetch(url, opts = {}) {
  const res = await fetch(url, { ...opts, headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) } });
  const text = await res.text();
  let body;
  try { body = text ? JSON.parse(text) : {}; } catch { body = text; }
  return { status: res.status, ok: res.ok, body };
}
async function jsonFetchOk(url, opts) {
  const r = await jsonFetch(url, opts);
  if (!r.ok) throw new Error(`${opts?.method || 'GET'} ${url} -> ${r.status}: ${JSON.stringify(r.body)}`);
  return r.body;
}

async function registerAndLogin(email) {
  const password = 'correct-horse-battery-staple';
  await jsonFetchOk(`${BASE}/v1/auth/register`, { method: 'POST', body: JSON.stringify({ email, password }) });
  const { access_token } = await jsonFetchOk(`${BASE}/v1/auth/login`, { method: 'POST', body: JSON.stringify({ email, password }) });
  return access_token;
}

async function uploadSmallFile(access, folderId, name, content) {
  const auth = { Authorization: `Bearer ${access}` };
  const hash = sha256(content);
  await jsonFetchOk(`${BASE}/v1/chunks/check`, { method: 'POST', headers: auth, body: JSON.stringify({ hashes: [hash] }) });
  const { upload_id } = await jsonFetchOk(`${BASE}/v1/uploads`, { method: 'POST', headers: auth, body: JSON.stringify({ folder_id: folderId, name, size_bytes: content.length, mime_type: 'text/plain' }) });
  const { targets } = await jsonFetchOk(`${BASE}/v1/uploads/${upload_id}/chunks/${hash}/init`, { method: 'POST', headers: auth });
  const etags = {};
  for (const t of targets) {
    const res = await fetch(t.put_url, { method: 'PUT', body: content });
    etags[t.node_id] = res.headers.get('etag');
  }
  await jsonFetchOk(`${BASE}/v1/uploads/${upload_id}/chunks/${hash}/commit`, { method: 'POST', headers: auth, body: JSON.stringify({ size_bytes: content.length, etags }) });
  return jsonFetchOk(`${BASE}/v1/uploads/${upload_id}/complete`, { method: 'POST', headers: { ...auth, 'Idempotency-Key': 'x' + upload_id }, body: JSON.stringify({ chunk_order: [hash], size_bytes: content.length, checksum_sha256: hash }) });
}

async function main() {
  const ownerAccess = await registerAndLogin(`sharing-owner-${Date.now()}@nimbus.dev`);
  const strangerAccess = await registerAndLogin(`sharing-stranger-${Date.now()}@nimbus.dev`);
  const ownerAuth = { Authorization: `Bearer ${ownerAccess}` };

  const { id: orgId } = await jsonFetchOk(`${BASE}/v1/orgs`, { method: 'POST', headers: ownerAuth, body: JSON.stringify({ name: 'Sharing Smoke Org' }) });
  const { id: folderId } = await jsonFetchOk(`${BASE}/v1/orgs/${orgId}/folders`, { method: 'POST', headers: ownerAuth, body: JSON.stringify({ name: 'Public' }) });
  const content = Buffer.from('the quick brown fox jumps over the lazy dog\n'.repeat(1000));
  const { file_id } = await uploadSmallFile(ownerAccess, folderId, 'notes.txt', content);
  console.log(`uploaded file_id=${file_id}`);

  console.log('== create share link (expires in 1 hour) ==');
  const link = await jsonFetchOk(`${BASE}/v1/files/${file_id}/share`, {
    method: 'POST', headers: ownerAuth, body: JSON.stringify({ expires_at: new Date(Date.now() + 3600_000).toISOString() }),
  });
  console.log(`  token=${link.token}`);

  console.log('== omitting expires_at is rejected (no more "no expiry") ==');
  const missingExpiry = await jsonFetch(`${BASE}/v1/files/${file_id}/share`, { method: 'POST', headers: ownerAuth, body: JSON.stringify({}) });
  if (missingExpiry.status !== 400) throw new Error(`expected 400 for missing expires_at, got ${missingExpiry.status}`);
  console.log('  OK: missing expires_at correctly rejected (400)');

  console.log('== a past expires_at is rejected ==');
  const pastExpiry = await jsonFetch(`${BASE}/v1/files/${file_id}/share`, {
    method: 'POST', headers: ownerAuth, body: JSON.stringify({ expires_at: new Date(Date.now() - 60_000).toISOString() }),
  });
  if (pastExpiry.status !== 400) throw new Error(`expected 400 for past expires_at, got ${pastExpiry.status}`);
  console.log('  OK: past expires_at correctly rejected (400)');

  console.log('== an expires_at beyond the 7-day max is rejected ==');
  const tooFarExpiry = await jsonFetch(`${BASE}/v1/files/${file_id}/share`, {
    method: 'POST', headers: ownerAuth, body: JSON.stringify({ expires_at: new Date(Date.now() + 8 * 24 * 3600_000).toISOString() }),
  });
  if (tooFarExpiry.status !== 400) throw new Error(`expected 400 for expires_at beyond 7 days, got ${tooFarExpiry.status}`);
  console.log('  OK: expires_at beyond 7 days correctly rejected (400)');

  console.log('== resolve share link WITHOUT any Authorization header ==');
  const resolved = await jsonFetchOk(`${BASE}/v1/shares/${link.token}`);
  if (resolved.file.name !== 'notes.txt') throw new Error('unexpected file name in share response');
  console.log(`  OK: file=${resolved.file.name} size=${resolved.file.size_bytes}`);

  console.log('== download the shared content unauthenticated and verify checksum ==');
  const parts = [];
  for (const c of resolved.download_plan.chunks.sort((a, b) => a.sequence - b.sequence)) {
    const res = await fetch(c.targets[0]);
    if (!res.ok) throw new Error(`unauthenticated chunk download failed: ${res.status}`);
    parts.push(Buffer.from(await res.arrayBuffer()));
  }
  const downloaded = Buffer.concat(parts);
  if (sha256(downloaded) !== sha256(content)) throw new Error('shared download checksum mismatch!');
  console.log('  OK: checksum matches');

  console.log('== a link expires naturally and is then rejected ==');
  const shortLived = await jsonFetchOk(`${BASE}/v1/files/${file_id}/share`, {
    method: 'POST', headers: ownerAuth, body: JSON.stringify({ expires_at: new Date(Date.now() + 2_000).toISOString() }),
  });
  await new Promise((r) => setTimeout(r, 3_000));
  const expiredResolve = await jsonFetch(`${BASE}/v1/shares/${shortLived.token}`);
  if (expiredResolve.status !== 403) throw new Error(`expected 403 for expired link, got ${expiredResolve.status}`);
  console.log('  OK: expired link correctly rejected (403)');

  console.log('== non-member cannot revoke the share link ==');
  const strangerAuth = { Authorization: `Bearer ${strangerAccess}` };
  const strangerDelete = await jsonFetch(`${BASE}/v1/shares/${link.token}`, { method: 'DELETE', headers: strangerAuth });
  if (strangerDelete.status !== 403) throw new Error(`expected 403 for non-member revoke, got ${strangerDelete.status}`);
  console.log('  OK: non-member revoke correctly rejected (403)');

  console.log('== owner revokes the share link ==');
  const ownerDelete = await jsonFetch(`${BASE}/v1/shares/${link.token}`, { method: 'DELETE', headers: ownerAuth });
  if (ownerDelete.status !== 204) throw new Error(`expected 204, got ${ownerDelete.status}`);
  const afterRevoke = await jsonFetch(`${BASE}/v1/shares/${link.token}`);
  if (afterRevoke.status !== 404) throw new Error(`expected 404 after revoke, got ${afterRevoke.status}`);
  console.log('  OK: revoked link now 404s');

  console.log('\nALL SHARING SMOKE CHECKS PASSED');
}

main().catch((err) => { console.error('FAIL:', err.message); process.exit(1); });
