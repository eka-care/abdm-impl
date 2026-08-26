import { NextResponse } from "next/server";
import { markKycVerified } from "@/server/db";

// Called from the SDK's onKYCSuccess callback (a confirmation string, not a
// profile object, so we key the update off the abha_address we already know).
export async function POST(req: Request) {
  const body = await req.json().catch(() => null);
  const abhaAddress = body?.abhaAddress;
  if (!abhaAddress) {
    return NextResponse.json({ error: "abhaAddress is required" }, { status: 400 });
  }
  return NextResponse.json({ patient: markKycVerified(abhaAddress) });
}
