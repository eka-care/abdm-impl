import { test, beforeEach, after } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

// Dynamic imports (not hoisted) so EKA_DB_PATH is set before lib/db.ts opens its file.
const dbPath = path.join(os.tmpdir(), `abdm-test-${Date.now()}.sqlite`);
process.env.EKA_DB_PATH = dbPath;

const { getAccessToken, __resetTokenCacheForTests } = await import("../../server/eka/token.ts");
const { upsertPatient, getPatientByOid, getPatientByAbhaAddress, markKycVerified } = await import(
  "../../server/db.ts"
);

beforeEach(() => {
  __resetTokenCacheForTests();
});

after(() => {
  fs.rmSync(dbPath, { force: true });
});

function mockFetchOnce(status: number, body: unknown) {
  const original = globalThis.fetch;
  globalThis.fetch = (async () => new Response(JSON.stringify(body), { status })) as typeof fetch;
  return () => {
    globalThis.fetch = original;
  };
}

test("getAccessToken fetches and caches the token", async () => {
  let calls = 0;
  const original = globalThis.fetch;
  globalThis.fetch = (async () => {
    calls++;
    return new Response(JSON.stringify({ access_token: "tok-1", expires_in: 3600 }), { status: 200 });
  }) as typeof fetch;
  try {
    assert.equal(await getAccessToken(), "tok-1");
    assert.equal(await getAccessToken(), "tok-1");
    assert.equal(calls, 1, "second call should reuse cache, not refetch");
  } finally {
    globalThis.fetch = original;
  }
});

test("getAccessToken refetches once the cached token has expired", async () => {
  let calls = 0;
  const original = globalThis.fetch;
  globalThis.fetch = (async () => {
    calls++;
    // expires_in of 0 puts expiresAt in the past immediately (30s early-refresh margin).
    return new Response(JSON.stringify({ access_token: `tok-${calls}`, expires_in: 0 }), { status: 200 });
  }) as typeof fetch;
  try {
    assert.equal(await getAccessToken(), "tok-1");
    assert.equal(await getAccessToken(), "tok-2");
    assert.equal(calls, 2);
  } finally {
    globalThis.fetch = original;
  }
});

test("getAccessToken single-flights concurrent callers", async () => {
  let calls = 0;
  const original = globalThis.fetch;
  globalThis.fetch = (async () => {
    calls++;
    await new Promise((r) => setTimeout(r, 10));
    return new Response(JSON.stringify({ access_token: "tok-concurrent", expires_in: 3600 }), { status: 200 });
  }) as typeof fetch;
  try {
    const [a, b] = await Promise.all([getAccessToken(), getAccessToken()]);
    assert.equal(a, "tok-concurrent");
    assert.equal(b, "tok-concurrent");
    assert.equal(calls, 1, "concurrent callers should share one in-flight request");
  } finally {
    globalThis.fetch = original;
  }
});

test("getAccessToken surfaces login failures", async () => {
  const restore = mockFetchOnce(401, { code: 401, error: "invalid client_secret" });
  try {
    await assert.rejects(() => getAccessToken(), /Eka login failed: 401/);
  } finally {
    restore();
  }
});

test("upsertPatient inserts a new patient and truncates mobile to last 4 digits", () => {
  const patient = upsertPatient({
    oid: "oid-1",
    abhaAddress: "test@abdm",
    abhaNumber: "12-3456-7890-1234",
    mobile: "9876543210",
    name: "Test Patient",
  }) as Record<string, unknown>;
  assert.equal(patient.oid, "oid-1");
  assert.equal(patient.mobile_last4, "3210");
  assert.equal(patient.abha_address, "test@abdm");
});

test("upsertPatient updates the existing row by oid instead of duplicating", () => {
  upsertPatient({ oid: "oid-2", abhaAddress: "old@abdm" });
  upsertPatient({ oid: "oid-2", abhaAddress: "new@abdm" });
  const patient = getPatientByOid("oid-2") as Record<string, unknown>;
  assert.equal(patient.abha_address, "new@abdm");
});

test("new patients start with kyc_verified = 0", () => {
  upsertPatient({ oid: "oid-3", abhaAddress: "kyctest@abdm" });
  const patient = getPatientByAbhaAddress("kyctest@abdm") as Record<string, unknown>;
  assert.equal(patient.kyc_verified, 0);
});

test("markKycVerified flips kyc_verified for the matching abha_address", () => {
  upsertPatient({ oid: "oid-4", abhaAddress: "kycdone@abdm" });
  const patient = markKycVerified("kycdone@abdm") as Record<string, unknown>;
  assert.equal(patient.kyc_verified, 1);
  assert.equal(patient.oid, "oid-4");
});
