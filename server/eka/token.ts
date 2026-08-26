// Server-only: never import from a client component.
type EkaLoginResponse = {
  access_token: string;
  expires_in: number;
};

const BASE_URL = process.env.EKA_BASE_URL ?? "https://api.eka.care";

let cached: { accessToken: string; expiresAt: number } | null = null;
let inFlight: Promise<string> | null = null;

async function fetchToken(): Promise<{ accessToken: string; expiresAt: number }> {
  const res = await fetch(`${BASE_URL}/connect-auth/v1/account/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      client_id: process.env.NEXT_PUBLIC_EKA_CLIENT_ID,
      client_secret: process.env.EKA_CLIENT_SECRET,
    }),
  });
  if (!res.ok) {
    throw new Error(`Eka login failed: ${res.status} ${await res.text()}`);
  }
  const data = (await res.json()) as EkaLoginResponse;
  // refresh 30s before actual expiry
  return { accessToken: data.access_token, expiresAt: Date.now() + (data.expires_in - 30) * 1000 };
}

export async function getAccessToken(): Promise<string> {
  if (cached && cached.expiresAt > Date.now()) return cached.accessToken;
  if (!inFlight) {
    inFlight = fetchToken()
      .then((t) => {
        cached = t;
        return t.accessToken;
      })
      .finally(() => {
        inFlight = null;
      });
  }
  return inFlight;
}

// Test-only escape hatch to reset module state between cases.
export function __resetTokenCacheForTests() {
  cached = null;
  inFlight = null;
}
