"use client";

import { useEffect, useRef, useState } from "react";
import AbhaWidget, { type AbhaMethod, type AbhaSuccessProfile } from "@/components/AbhaWidget";

const METHODS: { value: AbhaMethod; label: string }[] = [
  { value: "login_or_create_abha", label: "Login or Create ABHA" },
  { value: "login_with_abha", label: "Login with ABHA" },
  { value: "create_abha_with_mobile", label: "Create ABHA (Mobile OTP)" },
  { value: "login_abha_with_mobile", label: "Login with Mobile OTP" },
];

type Mode = { kind: "method"; method: AbhaMethod } | { kind: "kyc"; identifier: string };

// Normalised session stored in localStorage
type Session = {
  oid: string;
  abhaAddress: string;
  abhaNumber?: string;
  name?: string;
  mobile?: string;
};

type LinkState =
  | { status: "idle" }
  | { status: "uploading" }
  | { status: "success"; careContextId: string; ccStatus?: string }
  | { status: "error"; message: string };

const SESSION_KEY = "abdm_session";

function extractSession(profile: AbhaSuccessProfile): Session | null {
  const oid = profile.oid;
  const abhaAddress = profile["health-ids"]?.[0] ?? profile.abha_address;
  if (!oid || !abhaAddress) return null;
  return {
    oid,
    abhaAddress,
    abhaNumber: profile.abha_number,
    name: profile.fln ?? profile.name,
    mobile: profile.mobile,
  };
}

export default function AbhaPage() {
  const [mode, setMode] = useState<Mode>({ kind: "method", method: "login_or_create_abha" });
  const [widgetKey, setWidgetKey] = useState(0);
  const [kycInput, setKycInput] = useState("");
  const [session, setSession] = useState<Session | null>(null);
  const [linkState, setLinkState] = useState<LinkState>({ status: "idle" });
  const fileRef = useRef<HTMLInputElement>(null);

  // Restore session from localStorage on mount
  useEffect(() => {
    try {
      const raw = localStorage.getItem(SESSION_KEY);
      if (raw) setSession(JSON.parse(raw));
    } catch {
      // stale or corrupt — ignore
    }
  }, []);

  // Poll care context status until the link webhook lands
  useEffect(() => {
    if (linkState.status !== "success" || linkState.ccStatus) return;
    const id = linkState.careContextId;
    const timer = setInterval(async () => {
      const res = await fetch(`/api/m2/care-context/${id}`);
      if (!res.ok) return;
      const { status } = await res.json();
      if (status && status !== "PENDING") {
        setLinkState({ status: "success", careContextId: id, ccStatus: status });
      }
    }, 2000);
    return () => clearInterval(timer);
  }, [linkState]);

  function saveSession(s: Session) {
    setSession(s);
    localStorage.setItem(SESSION_KEY, JSON.stringify(s));
  }

  function logout() {
    localStorage.removeItem(SESSION_KEY);
    setSession(null);
    setLinkState({ status: "idle" });
    setWidgetKey((k) => k + 1); // remount widget to reset SDK state
  }

  function handleLoginSuccess(profile: AbhaSuccessProfile) {
    const s = extractSession(profile);
    if (s) saveSession(s);
  }

  function selectMethod(method: AbhaMethod) {
    setMode({ kind: "method", method });
    setWidgetKey((k) => k + 1);
  }

  async function startKyc() {
    const identifier = kycInput.trim();
    if (!identifier) return;
    setMode({ kind: "kyc", identifier });
    setWidgetKey((k) => k + 1);
  }

  async function handleUpload() {
    const file = fileRef.current?.files?.[0];
    if (!file || !session) return;

    setLinkState({ status: "uploading" });

    try {
      const base64 = await new Promise<string>((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve((reader.result as string).split(",")[1]);
        reader.onerror = reject;
        reader.readAsDataURL(file);
      });

      const res = await fetch("/api/m2/link-document", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          oid: session.oid,
          partner_pt_id: session.oid,
          abha_address: session.abhaAddress,
          file_name: file.name,
          mime_type: file.type || "application/octet-stream",
          content: base64,
        }),
      });

      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        setLinkState({ status: "error", message: data.error ?? `HTTP ${res.status}` });
        return;
      }
      setLinkState({ status: "success", careContextId: data.care_context_id });
      if (fileRef.current) fileRef.current.value = "";
    } catch (e) {
      setLinkState({ status: "error", message: e instanceof Error ? e.message : "Upload failed" });
    }
  }

  return (
    <main style={{ maxWidth: 720, margin: "0 auto", padding: 24 }}>
      <h1>ABHA</h1>

      {/* ── Logged-in banner ── */}
      {session && (
        <div style={{
          display: "flex", alignItems: "center", justifyContent: "space-between",
          padding: "10px 16px", marginBottom: 20,
          background: "#0a7d2f1a", border: "1px solid #0a7d2f", borderRadius: 8,
        }}>
          <span style={{ fontSize: 14 }}>
            Logged in as <strong>{session.abhaAddress}</strong>
            {session.name ? ` · ${session.name}` : ""}
          </span>
          <button onClick={logout} style={{ padding: "4px 12px", cursor: "pointer" }}>
            Logout
          </button>
        </div>
      )}

      {/* ── Link a Document (shown when logged in) ── */}
      {session ? (
        <div style={{ padding: 20, border: "1px solid #555", borderRadius: 8 }}>
          <h2 style={{ margin: "0 0 4px" }}>Link a Document</h2>
          <p style={{ margin: "0 0 16px", color: "#888", fontSize: 14 }}>
            Upload a PDF or image to link it as a care context in ABDM for{" "}
            <strong>{session.abhaAddress}</strong>.
          </p>

          <div style={{ display: "flex", gap: 12, alignItems: "center", flexWrap: "wrap" }}>
            <input
              ref={fileRef}
              type="file"
              accept=".pdf,image/*"
              disabled={linkState.status === "uploading"}
            />
            <button
              onClick={handleUpload}
              disabled={linkState.status === "uploading"}
              style={{ padding: "6px 16px" }}
            >
              {linkState.status === "uploading" ? "Linking…" : "Link to ABDM"}
            </button>
          </div>

          {linkState.status === "success" && (
            <p style={{ marginTop: 12, color: "#0a7d2f" }}>
              Care context <code>{linkState.careContextId}</code> is{" "}
              <strong>{linkState.ccStatus ?? "PENDING"}</strong>.
              {!linkState.ccStatus && " Waiting for webhook to confirm."}
            </p>
          )}
          {linkState.status === "error" && (
            <p style={{ marginTop: 12, color: "crimson" }}>Error: {linkState.message}</p>
          )}
        </div>
      ) : (
        /* ── Login / Registration ── */
        <>
          <label>
            Method:{" "}
            <select
              value={mode.kind === "method" ? mode.method : ""}
              onChange={(e) => selectMethod(e.target.value as AbhaMethod)}
            >
              {METHODS.map((m) => (
                <option key={m.value} value={m.value}>{m.label}</option>
              ))}
            </select>
          </label>

          <div style={{ marginTop: 16, paddingTop: 16, borderTop: "1px solid #333" }}>
            <p style={{ margin: "0 0 8px" }}>
              KYC upgrade (existing non-KYC ABHA via mobile OTP):
            </p>
            <input
              placeholder="ABHA address, e.g. name@abdm"
              value={kycInput}
              onChange={(e) => setKycInput(e.target.value)}
              style={{ padding: 6, marginRight: 8, minWidth: 220 }}
            />
            <button onClick={startKyc}>Start KYC upgrade</button>
          </div>

          <div style={{ marginTop: 24 }}>
            {mode.kind === "method" ? (
              <AbhaWidget
                key={widgetKey}
                method={mode.method}
                onLoginSuccess={handleLoginSuccess}
              />
            ) : (
              <AbhaWidget
                key={widgetKey}
                flow="abha-kyc"
                identifier={mode.identifier}
                oid=""
              />
            )}
          </div>
        </>
      )}
    </main>
  );
}
