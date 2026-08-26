"use client";

import { useEffect, useRef, useState } from "react";

export type AbhaMethod =
  | "login_with_abha"
  | "create_abha_with_mobile"
  | "login_or_create_abha"
  | "login_abha_with_mobile";

export type AbhaSuccessProfile = {
  // actual fields from the SDK response
  oid?: string;
  fln?: string;         // full legal name
  fn?: string;          // first name
  mn?: string;          // middle name
  ln?: string;          // last name
  gen?: string;
  dob?: string;
  mobile?: string;
  abha_number?: string;
  kyc_verified?: boolean;
  "health-ids"?: string[];   // ABHA addresses, e.g. ["name@abdm"]
  // fields we may normalise onto in upsert
  abha_address?: string;
  name?: string;
};

declare global {
  interface Window {
    initAbhaApp?: (config: {
      clientId: string;
      containerId: string;
      method: AbhaMethod;
      data?: {
        accessToken: string;
        oid?: string;
        // KYC upgrade for an existing non-KYC ABHA: set flow + identifier below.
        // identifier is always the patient's phr_address for this flow.
        flow?: "abha-kyc";
        identifier?: string;
        identifier_type?: "phr_address";
        hipId?: string;
        orgIconUrl?: string;
        linkToOrgIcon?: string;
      };
      onSuccess?: (params: { response: { data?: { profile?: AbhaSuccessProfile } } }) => void;
      onKYCSuccess?: (message: string) => void;
      onError?: (params: unknown) => void;
      onAbhaClose?: () => void;
    }) => void;
    closeAbhaApp?: () => void;
  }
}

const SDK_JS = "https://unpkg.com/@eka-care/abha/dist/sdk/abha/js/abha.js";
const SDK_CSS = "https://unpkg.com/@eka-care/abha/dist/sdk/abha/css/abha.css";
const CONTAINER_ID = "eka-abha-sdk";

function loadSdkAssets(): Promise<void> {
  if (window.initAbhaApp) return Promise.resolve();

  if (!document.querySelector(`link[href="${SDK_CSS}"]`)) {
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = SDK_CSS;
    document.head.appendChild(link);
  }

  const existing = document.querySelector<HTMLScriptElement>(`script[src="${SDK_JS}"]`);
  if (existing) {
    return new Promise((resolve) => existing.addEventListener("load", () => resolve(), { once: true }));
  }

  return new Promise((resolve, reject) => {
    const script = document.createElement("script");
    script.type = "module";
    script.src = SDK_JS;
    script.onload = () => resolve();
    script.onerror = () => reject(new Error("Failed to load ABHA SDK script"));
    document.head.appendChild(script);
  });
}

type AbhaWidgetProps =
  | { method: AbhaMethod; flow?: undefined; identifier?: undefined; oid?: string; onLoginSuccess?: (profile: AbhaSuccessProfile) => void }
  // KYC upgrade: needs an existing non-KYC ABHA address, not a create/login method.
  | { method?: AbhaMethod; flow: "abha-kyc"; identifier: string; oid?: string; onLoginSuccess?: (profile: AbhaSuccessProfile) => void };

export default function AbhaWidget({ method = "login_with_abha", flow, identifier, oid, onLoginSuccess }: AbhaWidgetProps) {
  // AbhaWidget is remounted (via `key`) whenever method/flow/identifier changes,
  // so state only ever needs to reset via these initializers, never mid-effect.
  const [status, setStatus] = useState<"loading" | "ready" | "idle" | "error">("loading");
  const [result, setResult] = useState<AbhaSuccessProfile | null>(null);
  const [kycMessage, setKycMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const cancelledRef = useRef(false);

  useEffect(() => {
    cancelledRef.current = false;

    (async () => {
      try {
        await loadSdkAssets();
        const res = await fetch("/api/abha/session", { method: "POST" });
        if (!res.ok) throw new Error("Failed to fetch session token");
        const { accessToken } = await res.json();
        if (cancelledRef.current || !window.initAbhaApp) return;

        window.initAbhaApp({
          clientId: process.env.NEXT_PUBLIC_EKA_CLIENT_ID!,
          containerId: CONTAINER_ID,
          method,
          data: {
            accessToken,
            oid,
            ...(flow
              ? {
                  flow,
                  identifier,
                  identifier_type: "phr_address" as const,
                  hipId: process.env.NEXT_PUBLIC_EKA_HIP_ID || undefined,
                  orgIconUrl: process.env.NEXT_PUBLIC_EKA_ORG_ICON_URL || undefined,
                  linkToOrgIcon: process.env.NEXT_PUBLIC_EKA_LINK_ICON_URL || undefined,
                }
              : {}),
          },
          onSuccess: async ({ response }) => {
            const profile = response?.data?.profile;

            // logged before any state/unmount so the response survives the widget going away
            console.log("[ABHA] onSuccess response:", response);
            console.log("[ABHA] onSuccess profile:", profile);
            setResult(profile ?? null);
            if (profile) onLoginSuccess?.(profile);
            if (profile?.oid) {
              await fetch("/api/patients/upsert", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({
                  oid: profile.oid,
                  abhaAddress: profile.abha_address,
                  abhaNumber: profile.abha_number,
                  mobile: profile.mobile,
                  name: profile.name,
                }),
              });
            }
          },
          onKYCSuccess: async (message) => {
            setKycMessage(message);
            if (identifier) {
              await fetch("/api/patients/kyc-verified", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ abhaAddress: identifier }),
              });
            }
          },
          onError: (err) => {
            console.error("ABHA SDK error", err);
            setError("ABHA flow failed. Check console for details.");
          },
          onAbhaClose: () => setStatus("idle"),
        });
        setStatus("ready");
      } catch (err) {
        if (!cancelledRef.current) {
          setError(err instanceof Error ? err.message : "Failed to initialize ABHA SDK");
          setStatus("error");
        }
      }
    })();

    return () => {
      cancelledRef.current = true;
      window.closeAbhaApp?.();
    };
  }, [method, flow, identifier, oid]);

  return (
    <div>
      {status === "loading" && <p>Loading ABHA…</p>}
      {status === "error" && error && <p style={{ color: "crimson" }}>{error}</p>}
      <div id={CONTAINER_ID} style={{ width: "100%", minHeight: 600 }} />
      {kycMessage && (
        <p style={{ marginTop: 16, color: "#0a7d2f" }}>{kycMessage}</p>
      )}
    </div>
  );
}
