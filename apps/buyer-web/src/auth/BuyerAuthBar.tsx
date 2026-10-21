import { FormEvent, useState } from "react";
import { clearBuyerSession, getToken, setToken, tokenScope } from "../api/client";

type Props = { onChange: () => void };

export function BuyerAuthBar({ onChange }: Props) {
  const [draft, setDraft] = useState(getToken());
  const has = Boolean(getToken());
  const scope = has ? tokenScope() : "";

  function save(e: FormEvent) {
    e.preventDefault();
    // Switching identity clears prior buyer's idempotency/order hints.
    clearBuyerSession();
    setToken(draft);
    setDraft(draft.trim());
    onChange();
  }

  function clear() {
    clearBuyerSession();
    setDraft("");
    onChange();
  }

  return (
    <form className="auth-bar card" onSubmit={save} aria-label="Buyer authentication">
      <label htmlFor="buyer-jwt">
        Buyer JWT <span className="muted">(session only · principal_type=buyer)</span>
      </label>
      <div className="row">
        <input
          id="buyer-jwt"
          type="password"
          autoComplete="off"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder="Paste buyer bearer token"
        />
        <button type="submit">Save</button>
        <button type="button" className="secondary" onClick={clear}>
          Clear
        </button>
      </div>
      <p className="muted small" id="buyer-auth-help">
        {has
          ? `Signed in (${scope}). Checkout keys are scoped to this identity.`
          : "Not signed in — protected steps need a token."}{" "}
        Sign-out clears idempotency keys. Mint:{" "}
        <code>go run ./tools/devtoken buyer buyer:self buyer_demo_1</code>
      </p>
    </form>
  );
}
