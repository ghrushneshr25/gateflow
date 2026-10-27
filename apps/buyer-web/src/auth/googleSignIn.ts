/**
 * Google Identity Services (Gmail) sign-in for lab demo.
 * Requires VITE_GOOGLE_CLIENT_ID (OAuth 2.0 Web client ID from Google Cloud Console).
 * Production would verify the ID token on a backend before issuing Gateflow JWTs.
 */

export type GoogleIdPayload = {
  email?: string;
  email_verified?: boolean | string;
  name?: string;
  sub?: string;
  picture?: string;
};

type PromptNotification = {
  isNotDisplayed?: () => boolean;
  getNotDisplayedReason?: () => string;
};

declare global {
  interface Window {
    google?: {
      accounts: {
        id: {
          initialize: (cfg: Record<string, unknown>) => void;
          prompt: (cb?: (n: PromptNotification) => void) => void;
          renderButton: (el: HTMLElement, cfg: Record<string, unknown>) => void;
          cancel: () => void;
        };
      };
    };
  }
}

const GIS_SRC = "https://accounts.google.com/gsi/client";

export function googleClientId(override?: string): string {
  if (override && override.trim()) return override.trim();
  const w = (window as unknown as { __GF_GOOGLE_CLIENT_ID?: string }).__GF_GOOGLE_CLIENT_ID;
  if (w && w.trim()) return w.trim();
  const v = import.meta.env.VITE_GOOGLE_CLIENT_ID;
  return typeof v === "string" ? v.trim() : "";
}

export function isGoogleSignInConfigured(): boolean {
  return Boolean(googleClientId());
}

let scriptPromise: Promise<void> | null = null;

export function loadGoogleScript(): Promise<void> {
  if (window.google?.accounts?.id) return Promise.resolve();
  if (scriptPromise) return scriptPromise;
  scriptPromise = new Promise((resolve, reject) => {
    const existing = document.querySelector(`script[src="${GIS_SRC}"]`);
    if (existing) {
      existing.addEventListener("load", () => resolve());
      existing.addEventListener("error", () => reject(new Error("Google script failed")));
      if (window.google?.accounts?.id) resolve();
      return;
    }
    const s = document.createElement("script");
    s.src = GIS_SRC;
    s.async = true;
    s.onload = () => resolve();
    s.onerror = () => reject(new Error("Could not load Google Sign-In"));
    document.head.appendChild(s);
  });
  return scriptPromise;
}

/** Decode JWT payload without verifying (verification is server-side in production). */
export function decodeJwtPayload(jwt: string): GoogleIdPayload {
  const parts = jwt.split(".");
  if (parts.length < 2 || !parts[1]) throw new Error("Invalid Google credential");
  const json = atob(parts[1].replace(/-/g, "+").replace(/_/g, "/"));
  return JSON.parse(json) as GoogleIdPayload;
}

export type GoogleCredentialResult = {
  email: string;
  name?: string;
  googleSub?: string;
  credential: string;
};

/** Render official Google button into a container. */
export async function renderGoogleButton(
  el: HTMLElement,
  onCredential: (r: GoogleCredentialResult) => void,
  onError: (msg: string) => void,
  clientIdOverride?: string,
): Promise<void> {
  const clientId = googleClientId(clientIdOverride);
  if (!clientId) return;
  await loadGoogleScript();
  if (!window.google?.accounts?.id) {
    onError("Google Sign-In unavailable");
    return;
  }
  window.google.accounts.id.initialize({
    client_id: clientId,
    callback: (resp: { credential?: string }) => {
      try {
        if (!resp.credential) throw new Error("No credential");
        const p = decodeJwtPayload(resp.credential);
        const email = (p.email || "").trim().toLowerCase();
        if (!email) throw new Error("No email on Google account");
        onCredential({
          email,
          name: p.name,
          googleSub: p.sub,
          credential: resp.credential,
        });
      } catch (e) {
        onError(e instanceof Error ? e.message : "Google sign-in failed");
      }
    },
  });
  el.innerHTML = "";
  window.google.accounts.id.renderButton(el, {
    type: "standard",
    theme: "outline",
    size: "large",
    text: "signup_with",
    shape: "rectangular",
    logo_alignment: "left",
    width: 280,
  });
}
