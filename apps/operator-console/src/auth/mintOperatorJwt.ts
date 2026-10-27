/** Lab-only HS256 operator JWT matching libs/go/authn DevSigner. */

const DEFAULT_LAB_SECRET = "gateflow-dev-secret-change-me";

const OPS_SCOPES = [
  "ops:sale:write",
  "ops:sale:publish",
  "ops:order:recover",
  "ops:outbox:read",
  "ops:outbox:replay",
  "ops:admission:read",
  "ops:admission:fence",
  "ops:admission:budget",
];

function b64urlJson(obj: unknown): string {
  const bytes = new TextEncoder().encode(JSON.stringify(obj));
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

export async function mintOperatorDevJwt(subject = "ops_console"): Promise<string> {
  const secret = labJwtSecret();
  const header = b64urlJson({ alg: "HS256", typ: "JWT" });
  const payload = b64urlJson({
    sub: subject,
    principal_type: "operator",
    scopes: OPS_SCOPES,
    iss: "gateflow-dev",
    aud: ["gateflow"],
    exp: Math.floor(Date.now() / 1000) + 60 * 60 * 8,
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
