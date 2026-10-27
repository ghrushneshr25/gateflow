/**
 * Lab-only HS256 JWT mint matching libs/go/authn DevSigner.
 * Not for production IdP — secret must match GATEFLOW_DEV_JWT_SECRET on APIs.
 */

const DEFAULT_LAB_SECRET = "gateflow-dev-secret-change-me";

function b64urlJson(obj: unknown): string {
  const json = JSON.stringify(obj);
  const bytes = new TextEncoder().encode(json);
  let bin = "";
  bytes.forEach((b) => {
    bin += String.fromCharCode(b);
  });
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function b64urlBytes(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf);
  let bin = "";
  bytes.forEach((b) => {
    bin += String.fromCharCode(b);
  });
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export function labJwtSecret(): string {
  const v = import.meta.env.VITE_DEV_JWT_SECRET;
  if (typeof v === "string" && v.trim()) return v.trim();
  return DEFAULT_LAB_SECRET;
}

/** Sanitize buyer id: alphanumeric, underscore, hyphen; length 3–64. */
export function normalizeBuyerId(raw: string): string {
  const s = raw.trim().toLowerCase().replace(/[^a-z0-9_-]/g, "_");
  if (s.length < 3) return "";
  return s.slice(0, 64);
}

export async function mintBuyerDevJwt(
  buyerId: string,
  opts?: { secret?: string; ttlSeconds?: number },
): Promise<string> {
  const sub = normalizeBuyerId(buyerId);
  if (!sub) {
    throw new Error("Enter a buyer name (at least 3 letters or numbers).");
  }
  const secret = opts?.secret ?? labJwtSecret();
  const ttl = opts?.ttlSeconds ?? 60 * 60 * 8;
  const header = b64urlJson({ alg: "HS256", typ: "JWT" });
  const payload = b64urlJson({
    sub,
    principal_type: "buyer",
    scopes: ["buyer:self"],
    iss: "gateflow-dev",
    aud: ["gateflow"],
    exp: Math.floor(Date.now() / 1000) + ttl,
  });
  const data = `${header}.${payload}`;
  const key = await crypto.subtle.importKey(
    "raw",
    new TextEncoder().encode(secret),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  const sig = await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(data));
  return `${data}.${b64urlBytes(sig)}`;
}

export function randomBuyerId(): string {
  const n = Math.floor(Math.random() * 1e6)
    .toString()
    .padStart(6, "0");
  return `buyer_${n}`;
}
