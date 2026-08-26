// Server-only. Uses Node's built-in sqlite (no extra dependency) for a
// single patients table — plenty for M1's abha_address/abha_number/oid.
import { DatabaseSync } from "node:sqlite";
import fs from "node:fs";
import path from "node:path";

const DB_PATH = process.env.EKA_DB_PATH ?? path.join(process.cwd(), "data", "patients.sqlite");
fs.mkdirSync(path.dirname(DB_PATH), { recursive: true });

const db = new DatabaseSync(DB_PATH);
db.exec(`
  CREATE TABLE IF NOT EXISTS patients (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    oid TEXT UNIQUE NOT NULL,
    abha_address TEXT,
    abha_number TEXT,
    mobile_last4 TEXT,
    name TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
  )
`);
try {
  db.exec(`ALTER TABLE patients ADD COLUMN kyc_verified INTEGER NOT NULL DEFAULT 0`);
} catch (err) {
  if (!(err instanceof Error) || !/duplicate column name/i.test(err.message)) throw err;
}

export type PatientUpsertInput = {
  oid: string;
  abhaAddress?: string | null;
  abhaNumber?: string | null;
  /** Full mobile number in; only the last 4 digits are ever persisted. */
  mobile?: string | null;
  name?: string | null;
};

export function upsertPatient(input: PatientUpsertInput) {
  const mobileLast4 = input.mobile ? input.mobile.slice(-4) : null;
  db.prepare(
    `INSERT INTO patients (oid, abha_address, abha_number, mobile_last4, name)
     VALUES (?, ?, ?, ?, ?)
     ON CONFLICT(oid) DO UPDATE SET
       abha_address = excluded.abha_address,
       abha_number = excluded.abha_number,
       mobile_last4 = excluded.mobile_last4,
       name = excluded.name`
  ).run(input.oid, input.abhaAddress ?? null, input.abhaNumber ?? null, mobileLast4, input.name ?? null);
  return getPatientByOid(input.oid);
}

export function getPatientByOid(oid: string) {
  return db.prepare(`SELECT * FROM patients WHERE oid = ?`).get(oid) ?? null;
}

export function getPatientByAbhaAddress(abhaAddress: string) {
  return db.prepare(`SELECT * FROM patients WHERE abha_address = ?`).get(abhaAddress) ?? null;
}

export function markKycVerified(abhaAddress: string) {
  db.prepare(`UPDATE patients SET kyc_verified = 1 WHERE abha_address = ?`).run(abhaAddress);
  return getPatientByAbhaAddress(abhaAddress);
}

export function __getDbForTests() {
  return db;
}
