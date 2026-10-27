import { useEffect, useRef, useState } from "react";
import { api, clearBuyerSession, getToken, HttpError, setToken, tokenScope } from "../api/client";
import { GoogleCredentialResult, renderGoogleButton } from "./googleSignIn";
import { clearBuyerProfile, getBuyerProfile, setBuyerProfile } from "./sessionProfile";

type Props = { onChange: () => void };

/**
 * Buyer auth is third-party only (Google first). No local password signup.
 * Flow: Google ID token → POST /v1/auth/buyer/google → Gateflow buyer JWT.
 */
export function BuyerAuthBar({ onChange }: Props) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [clientId, setClientId] = useState("");
  const [providersLoaded, setProvidersLoaded] = useState(false);
  const googleBtnRef = useRef<HTMLDivElement>(null);
  const has = Boolean(getToken());
  const profile = getBuyerProfile();
  const scope = has ? tokenScope() : "";

  useEffect(() => {
    let cancelled = false;
    api
      .buyerProviders()
      .then((p) => {
        if (cancelled) return;
        const g = p.providers.find((x) => x.id === "google");
        setClientId((g?.enabled && g.client_id) || import.meta.env.VITE_GOOGLE_CLIENT_ID || "");
        setProvidersLoaded(true);
      })
      .catch(() => {
        if (cancelled) return;
        // Fall back to Vite env if sale unreachable
        setClientId(import.meta.env.VITE_GOOGLE_CLIENT_ID || "");
        setProvidersLoaded(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  async function exchangeGoogle(r: GoogleCredentialResult) {
    setErr("");
    setBusy(true);
    try {
      const res = await api.buyerGoogle(r.credential);
      clearBuyerSession();
      clearBuyerProfile();
      setToken(res.access_token);
      setBuyerProfile({
        buyerId: res.buyer.buyer_id,
        email: res.buyer.email,
        name: res.buyer.display_name,
        provider: "google",
      });
      onChange();
    } catch (ex) {
      if (ex instanceof HttpError) {
        setErr(ex.message || `Sign-in failed (${ex.status})`);
      } else {
        setErr(ex instanceof Error ? ex.message : "Google sign-in failed");
      }
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    if (has || !providersLoaded || !clientId || !googleBtnRef.current) return;
    let cancelled = false;
    // Ensure GIS sees the same client id the server validates
    (window as unknown as { __GF_GOOGLE_CLIENT_ID?: string }).__GF_GOOGLE_CLIENT_ID = clientId;
    renderGoogleButton(
      googleBtnRef.current,
      (r) => {
        if (!cancelled) void exchangeGoogle(r);
      },
      (msg) => {
        if (!cancelled) setErr(msg);
      },
      clientId,
    ).catch((e) => {
      if (!cancelled) setErr(e instanceof Error ? e.message : "Google button failed");
    });
    return () => {
      cancelled = true;
    };
  }, [has, providersLoaded, clientId]);

  function clear() {
    clearBuyerSession();
    clearBuyerProfile();
    setErr("");
    onChange();
  }

  const displayName =
    profile?.email || profile?.name || (scope.startsWith("sub:") ? scope.slice(4) : scope);

  return (
    <div className="auth-bar card" aria-label="Buyer authentication">
      {!has ? (
        <>
          <h2 className="auth-title">Sign in with Google</h2>
          <p className="muted small" style={{ marginTop: 0 }}>
            Buyers use third-party sign-in only (no local password). More providers can be added
            later; Google is first.
          </p>
          {!providersLoaded ? (
            <p className="muted small">Loading sign-in options…</p>
          ) : clientId ? (
            <div className="google-wrap">
              <div ref={googleBtnRef} className="google-btn" aria-busy={busy} />
              {busy ? <p className="muted small">Completing sign-in…</p> : null}
            </div>
          ) : (
            <p className="muted small info-box">
              <strong>Google sign-in is not configured.</strong> Set{" "}
              <code>GATEFLOW_GOOGLE_CLIENT_ID</code> on the sale service (and matching{" "}
              <code>VITE_GOOGLE_CLIENT_ID</code> for the buyer app build). Create an OAuth 2.0 Web
              client in Google Cloud Console; authorized JS origins must include this site
              (e.g. <code>http://127.0.0.1:8088</code>).
            </p>
          )}
        </>
      ) : (
        <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start" }}>
          <div>
            <p className="ok small" style={{ margin: 0 }}>
              Signed in as <strong>{displayName}</strong>
            </p>
            {profile?.buyerId ? (
              <p className="muted small" style={{ margin: "0.25rem 0 0" }}>
                Buyer id <code>{profile.buyerId}</code>
                {profile.provider === "google" ? " · Google" : ""}
              </p>
            ) : null}
          </div>
          <button type="button" className="secondary" onClick={clear}>
            Sign out
          </button>
        </div>
      )}
      {err ? <p className="error small">{err}</p> : null}
    </div>
  );
}
