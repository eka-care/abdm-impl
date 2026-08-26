import { NextResponse } from "next/server";
import { getPatientByAbhaAddress } from "@/server/db";

// Used before mounting the KYC flow, to pass along a known oid if we have one.
export async function GET(req: Request) {
  const abhaAddress = new URL(req.url).searchParams.get("abhaAddress");
  if (!abhaAddress) {
    return NextResponse.json({ error: "abhaAddress is required" }, { status: 400 });
  }
  return NextResponse.json({ patient: getPatientByAbhaAddress(abhaAddress) });
}
