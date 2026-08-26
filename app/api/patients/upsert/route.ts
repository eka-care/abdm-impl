import { NextResponse } from "next/server";
import { upsertPatient } from "@/server/db";

// Called from the SDK's onSuccess callback (webhook wiring deferred).
export async function POST(req: Request) {
  const body = await req.json().catch(() => null);
  const oid = body?.oid;
  if (!oid) {
    return NextResponse.json({ error: "oid is required" }, { status: 400 });
  }
  const patient = upsertPatient({
    oid,
    abhaAddress: body.abhaAddress,
    abhaNumber: body.abhaNumber,
    mobile: body.mobile,
    name: body.name,
  });
  return NextResponse.json({ patient });
}
