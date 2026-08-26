import { NextResponse } from "next/server";
import { getAccessToken } from "@/server/eka/token";

// Browser calls this before mounting the ABHA Web SDK. Returns a short-lived
// access token only — client_secret never leaves the server.
export async function POST() {
  try {
    const accessToken = await getAccessToken();
    return NextResponse.json({ accessToken });
  } catch (err) {
    console.error("Eka session token fetch failed", err);
    return NextResponse.json({ error: "Failed to obtain session token" }, { status: 502 });
  }
}
