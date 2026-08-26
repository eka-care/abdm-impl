import { NextRequest, NextResponse } from "next/server";

const GO_BACKEND = process.env.GO_BACKEND_URL ?? "http://localhost:8080";

export async function GET(_req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const res = await fetch(`${GO_BACKEND}/api/m2/care-context/${id}`);
  const data = await res.json().catch(() => ({}));
  return NextResponse.json(data, { status: res.status });
}
