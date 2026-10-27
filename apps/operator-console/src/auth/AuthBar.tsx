import { FormEvent, useState } from "react";
import { api, clearToken, getToken, setToken, HttpError } from "../api/client";

type Props = { onChange: () => void };

export function AuthBar({ onChange }: Props) {
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [pasteTok, setPasteTok] = useState("");
  const signedIn = Boolean(getToken());

  async function login(e: FormEvent) {
    e.preventDefault();
    setErr("");
    setBusy(true);
    try {
      const res = await api.login(username.trim(), password);
      setToken(res.access_token);
      setPassword("");
      onChange();
    } catch (ex) {
      if (ex instanceof HttpError) {
        setErr(ex.status === 401 ? "Invalid username or password" : ex.message);
      } else {
        setErr(ex instanceof Error ? ex.message : "Login failed");
      }
    } finally {
      setBusy(false);
    }
  }

  function savePaste(e: FormEvent) {
    e.preventDefault();
    setToken(pasteTok);
    setPasteTok("");
    onChange();
  }

  function clear() {
    clearToken();
    setPassword("");
    setPasteTok("");
    setErr("");
    onChange();
  }

  return (
    <div className="auth-bar card" aria-label="Operator authentication">
      {!signedIn ? (
        <>
          <h2 style={{ margin: "0 0 0.35rem", fontSize: "1.1rem" }}>Admin sign-in</h2>
          <p className="muted small" style={{ marginTop: 0 }}>
            Default lab user is created automatically: <code>admin</code> /{" "}
            <code>GateflowAdmin1!</code> (override with GATEFLOW_ADMIN_*).
          </p>
          <form onSubmit={login}>
            <label htmlFor="ops-user">Username</label>
            <input
              id="ops-user"
              autoComplete="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              disabled={busy}
            />
            <label htmlFor="ops-pass">Password</label>
            <div className="auth-row">
              <input
                id="ops-pass"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                disabled={busy}
              />
              <button type="submit" disabled={busy || !username.trim() || !password}>
                {busy ? "Signing in…" : "Sign in"}
              </button>
            </div>
          </form>
        </>
      ) : (
        <div className="auth-row" style={{ justifyContent: "space-between" }}>
          <p className="ok small" style={{ margin: 0 }}>
            Signed in
          </p>
          <button type="button" className="secondary" onClick={clear}>
            Sign out
          </button>
        </div>
      )}
      {err ? <p className="error small">{err}</p> : null}
      <details className="advanced" style={{ marginTop: "0.75rem" }}>
        <summary className="muted small">Advanced: paste JWT</summary>
        <form onSubmit={savePaste} className="auth-row" style={{ marginTop: "0.5rem" }}>
          <input
            type="password"
            autoComplete="off"
            value={pasteTok}
            onChange={(e) => setPasteTok(e.target.value)}
            placeholder="Bearer token"
            aria-label="Operator token"
          />
          <button type="submit" className="secondary">
            Use token
          </button>
        </form>
      </details>
    </div>
  );
}
