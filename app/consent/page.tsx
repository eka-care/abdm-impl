"use client";

import { useEffect, useState } from "react";

const SESSION_KEY = "abdm_session";

type Session = { oid: string; abhaAddress: string; name?: string };

type ConsentSummary = {
  consent_id: string;
  consent_init_id: string;
  status: string;
  hi_types: string[];
  period: { from: string; to: string; expiry: string };
  c_at: string;
};

type ConsentRecord = {
  care_context_id: string;
  fhir_json: string;
  received_at: string;
};

const PURPOSES = [
  "Care management",
  "Self Requested",
  "Public Health",
  "Disease Specific Health Research",
];

const ALL_HI_TYPES = [
  "OPConsultation", "Prescription", "DiagnosticReport", "DischargeSummary",
  "ImmunizationRecord", "HealthDocumentRecord", "WellnessRecord", "Invoice",
];

function today() { return new Date().toISOString().slice(0, 10); }
function nextYear() {
  const d = new Date();
  d.setFullYear(d.getFullYear() + 1);
  return d.toISOString().slice(0, 10);
}

const STATUS_COLOR: Record<string, string> = {
  REQUESTED: "#888",
  GRANTED: "#0a7d2f",
  DENIED: "crimson",
  REVOKED: "#c47a00",
  EXPIRED: "#555",
};

export default function ConsentPage() {
  const [session, setSession] = useState<Session | null>(null);
  const [consents, setConsents] = useState<ConsentSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [records, setRecords] = useState<ConsentRecord[]>([]);
  const [recordsLoading, setRecordsLoading] = useState(false);
  const [expandedRecord, setExpandedRecord] = useState<string | null>(null);

  // Create form state
  const [form, setForm] = useState({
    purpose: "Care management",
    dateFrom: "2020-01-01",
    dateTo: nextYear(),
    expiry: nextYear(),
    hiTypes: ALL_HI_TYPES as string[],
  });
  const [creating, setCreating] = useState(false);
  const [createResult, setCreateResult] = useState<string | null>(null);
  const [createError, setCreateError] = useState<string | null>(null);

  useEffect(() => {
    try {
      const raw = localStorage.getItem(SESSION_KEY);
      if (raw) setSession(JSON.parse(raw));
    } catch { /* ignore */ }
  }, []);

  useEffect(() => {
    if (session) fetchConsents();
  }, [session]); // eslint-disable-line react-hooks/exhaustive-deps

  async function fetchConsents() {
    if (!session) return;
    setLoading(true);
    try {
      const res = await fetch("/api/m3/consent/list", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ oid: session.oid, partner_pt_id: session.oid, abha_address: session.abhaAddress }),
      });
      const data = await res.json().catch(() => ({}));
      setConsents(data.consents ?? []);
    } finally {
      setLoading(false);
    }
  }

  async function createConsent() {
    if (!session) return;
    setCreating(true);
    setCreateResult(null);
    setCreateError(null);
    try {
      const res = await fetch("/api/m3/consent", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          oid: session.oid,
          partner_pt_id: session.oid,
          abha_address: session.abhaAddress,
          purpose: form.purpose,
          record_types: form.hiTypes,
          date_from: form.dateFrom,
          date_to: form.dateTo,
          expiry: form.expiry,
        }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        setCreateError(data.error ?? `HTTP ${res.status}`);
        return;
      }
      setCreateResult(data.consent_init_id);
      fetchConsents();
    } catch (e) {
      setCreateError(e instanceof Error ? e.message : "Failed");
    } finally {
      setCreating(false);
    }
  }

  async function selectConsent(id: string) {
    setSelectedId(id);
    setRecords([]);
    setExpandedRecord(null);
    setRecordsLoading(true);
    try {
      const res = await fetch(`/api/m3/consent/${id}/records`);
      const data = await res.json().catch(() => ({}));
      setRecords(data.records ?? []);
    } finally {
      setRecordsLoading(false);
    }
  }

  function toggleHiType(t: string) {
    setForm(f => ({
      ...f,
      hiTypes: f.hiTypes.includes(t) ? f.hiTypes.filter(x => x !== t) : [...f.hiTypes, t],
    }));
  }

  if (!session) {
    return (
      <main style={{ maxWidth: 600, margin: "0 auto", padding: 40 }}>
        <h1>Consent Management</h1>
        <p style={{ color: "#888" }}>
          Please <a href="/abha">log in with ABHA</a> first.
        </p>
      </main>
    );
  }

  return (
    <main style={{ maxWidth: 900, margin: "0 auto", padding: 24, fontFamily: "monospace" }}>
      <h1 style={{ margin: "0 0 4px" }}>Consent Management</h1>
      <p style={{ color: "#888", margin: "0 0 24px", fontSize: 13 }}>
        Logged in as <strong>{session.abhaAddress}</strong>{session.name ? ` · ${session.name}` : ""}
      </p>

      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 24 }}>

        {/* ── Create consent ── */}
        <section style={{ border: "1px solid #333", borderRadius: 8, padding: 20 }}>
          <h2 style={{ margin: "0 0 16px", fontSize: 16 }}>Create Consent Request</h2>

          <label style={labelStyle}>Purpose</label>
          <select value={form.purpose} onChange={e => setForm(f => ({ ...f, purpose: e.target.value }))} style={inputStyle}>
            {PURPOSES.map(p => <option key={p}>{p}</option>)}
          </select>

          <label style={labelStyle}>Date from</label>
          <input type="date" value={form.dateFrom} onChange={e => setForm(f => ({ ...f, dateFrom: e.target.value }))} style={inputStyle} />

          <label style={labelStyle}>Date to</label>
          <input type="date" value={form.dateTo} onChange={e => setForm(f => ({ ...f, dateTo: e.target.value }))} style={inputStyle} />

          <label style={labelStyle}>Consent expiry</label>
          <input type="date" value={form.expiry} onChange={e => setForm(f => ({ ...f, expiry: e.target.value }))} style={inputStyle} />

          <label style={labelStyle}>Record types</label>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 6, marginBottom: 16 }}>
            {ALL_HI_TYPES.map(t => (
              <label key={t} style={{ fontSize: 11, cursor: "pointer", display: "flex", alignItems: "center", gap: 3 }}>
                <input type="checkbox" checked={form.hiTypes.includes(t)} onChange={() => toggleHiType(t)} />
                {t}
              </label>
            ))}
          </div>

          <button onClick={createConsent} disabled={creating} style={btnStyle}>
            {creating ? "Requesting…" : "Request Consent"}
          </button>

          {createResult && (
            <p style={{ marginTop: 10, fontSize: 12, color: "#0a7d2f" }}>
              Consent requested — init ID: <code>{createResult}</code>
              <br />Patient will receive an approval request on their PHR app.
            </p>
          )}
          {createError && (
            <p style={{ marginTop: 10, fontSize: 12, color: "crimson" }}>Error: {createError}</p>
          )}
        </section>

        {/* ── Consent list ── */}
        <section style={{ border: "1px solid #333", borderRadius: 8, padding: 20 }}>
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
            <h2 style={{ margin: 0, fontSize: 16 }}>Consent Requests</h2>
            <button onClick={fetchConsents} style={{ ...btnStyle, padding: "3px 10px", fontSize: 11 }}>Refresh</button>
          </div>

          {loading && <p style={{ color: "#888", fontSize: 13 }}>Loading…</p>}
          {!loading && consents.length === 0 && (
            <p style={{ color: "#888", fontSize: 13 }}>No consents yet.</p>
          )}

          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            {consents.map(c => {
              const id = c.consent_id || c.consent_init_id;
              const isSelected = selectedId === id;
              return (
                <div
                  key={id}
                  onClick={() => selectConsent(id)}
                  style={{
                    padding: "10px 12px",
                    borderRadius: 6,
                    border: isSelected ? "1px solid #666" : "1px solid #2a2a2a",
                    cursor: "pointer",
                    background: isSelected ? "#1a1a1a" : "transparent",
                  }}
                >
                  <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 4 }}>
                    <code style={{ fontSize: 11 }}>{id.slice(0, 12)}…</code>
                    <span style={{ fontSize: 11, color: STATUS_COLOR[c.status] ?? "#888", fontWeight: 600 }}>
                      {c.status}
                    </span>
                  </div>
                  <div style={{ fontSize: 11, color: "#888" }}>
                    {c.period?.from?.slice(0, 10)} → {c.period?.to?.slice(0, 10)}
                  </div>
                </div>
              );
            })}
          </div>
        </section>
      </div>

      {/* ── Fetched records ── */}
      {selectedId && (
        <section style={{ marginTop: 24, border: "1px solid #333", borderRadius: 8, padding: 20 }}>
          <h2 style={{ margin: "0 0 12px", fontSize: 16 }}>
            Received FHIR Records
            <span style={{ fontSize: 12, color: "#888", marginLeft: 8 }}>consent {selectedId.slice(0, 12)}…</span>
          </h2>

          {recordsLoading && <p style={{ color: "#888", fontSize: 13 }}>Loading records…</p>}

          {!recordsLoading && records.length === 0 && (
            <p style={{ color: "#888", fontSize: 13 }}>
              No records yet. Waiting for patient approval + HIP to push data via{" "}
              <code>abha.hiu_data_push</code> webhook.
            </p>
          )}

          {records.map(r => (
            <div key={r.care_context_id} style={{ marginBottom: 12, border: "1px solid #2a2a2a", borderRadius: 6 }}>
              <div
                onClick={() => setExpandedRecord(expandedRecord === r.care_context_id ? null : r.care_context_id)}
                style={{ padding: "10px 12px", cursor: "pointer", display: "flex", justifyContent: "space-between" }}
              >
                <code style={{ fontSize: 12 }}>{r.care_context_id}</code>
                <span style={{ fontSize: 11, color: "#888" }}>{r.received_at?.slice(0, 19)} ▸</span>
              </div>
              {expandedRecord === r.care_context_id && (
                <pre style={{
                  margin: 0, padding: "10px 12px",
                  borderTop: "1px solid #2a2a2a",
                  fontSize: 11, overflowX: "auto", maxHeight: 400,
                  background: "#fff",
                }}>
                  {(() => { try { return JSON.stringify(JSON.parse(r.fhir_json), null, 2); } catch { return r.fhir_json; } })()}
                </pre>
              )}
            </div>
          ))}
        </section>
      )}
    </main>
  );
}

const labelStyle: React.CSSProperties = { display: "block", fontSize: 12, color: "#888", marginBottom: 4 };
const inputStyle: React.CSSProperties = { display: "block", width: "100%", marginBottom: 12, padding: "6px 8px", background: "#111", border: "1px solid #333", color: "#eee", borderRadius: 4, fontSize: 13, boxSizing: "border-box" };
const btnStyle: React.CSSProperties = { padding: "7px 16px", cursor: "pointer", background: "#1a1a1a", border: "1px solid #555", color: "#eee", borderRadius: 4, fontSize: 13 };
