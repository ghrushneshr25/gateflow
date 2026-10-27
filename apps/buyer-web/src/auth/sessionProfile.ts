const PROFILE_KEY = "gateflow_buyer_profile";

export type BuyerProfile = {
  buyerId: string;
  email?: string;
  name?: string;
  provider: "email" | "google" | "local" | "token";
};

export function getBuyerProfile(): BuyerProfile | null {
  try {
    const raw = sessionStorage.getItem(PROFILE_KEY);
    if (!raw) return null;
    return JSON.parse(raw) as BuyerProfile;
  } catch {
    return null;
  }
}

export function setBuyerProfile(p: BuyerProfile): void {
  sessionStorage.setItem(PROFILE_KEY, JSON.stringify(p));
}

export function clearBuyerProfile(): void {
  sessionStorage.removeItem(PROFILE_KEY);
}

/** Map an email to a stable buyer_id suitable for Gateflow JWT sub. */
export function emailToBuyerId(email: string): string {
  const e = email.trim().toLowerCase();
  if (!e.includes("@") || e.length < 5) return "";
  // Prefer readable local-part; suffix short hash of full email for uniqueness.
  const [local, domain] = e.split("@");
  const base = (local || "user")
    .replace(/[^a-z0-9]/g, "_")
    .replace(/_+/g, "_")
    .replace(/^_|_$/g, "")
    .slice(0, 32);
  let h = 0;
  for (let i = 0; i < e.length; i++) h = (h * 31 + e.charCodeAt(i)) >>> 0;
  const tag = h.toString(36).slice(0, 6);
  const dom = (domain || "mail").replace(/[^a-z0-9]/g, "").slice(0, 8);
  return `g_${base}_${dom}_${tag}`.slice(0, 64);
}

export function isValidEmail(email: string): boolean {
  const e = email.trim();
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(e) && e.length <= 254;
}
