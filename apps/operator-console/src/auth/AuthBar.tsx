import { FormEvent, useState } from "react";
import { clearToken, getToken, setToken } from "../api/client";

type Props = {
  onChange: () => void;
};

export function AuthBar({ onChange }: Props) {
  const [draft, setDraft] = useState(getToken());
  const [saved, setSaved] = useState(Boolean(getToken()));

  function save(e: FormEvent) {
    e.preventDefault();
    setToken(draft);
    setSaved(Boolean(draft.trim()));
    onChange();
  }

  function clear() {
    clearToken();
    setDraft("");
    setSaved(false);
    onChange();
  }

  return (
    <form className="auth-bar card" onSubmit={save} aria-label="Operator authentication">
      <label htmlFor="ops-jwt">
        Operator JWT{" "}
        <span className="muted">(session only; scopes: recover, outbox, fence)</span>
      </label>
      <div className="auth-row">
        <input
          id="ops-jwt"
          name="ops-jwt"
          type="password"
          autoComplete="off"
          value={draft}
          onChange={(ev) => setDraft(ev.target.value)}
          placeholder="Paste operator bearer token"
          aria-describedby="ops-jwt-help"
        />
        <button type="submit">Save</button>
        <button type="button" className="secondary" onClick={clear} disabled={!saved && !draft}>
          Clear
        </button>
      </div>
      <p id="ops-jwt-help" className="muted small">
        {saved
          ? "Token saved in sessionStorage for this browser tab."
          : "No token — protected APIs will return 401."}{" "}
        Mint locally:{" "}
        <code>
          go run ./tools/devtoken operator
          ops:sale:write,ops:sale:publish,ops:order:recover,ops:outbox:read,ops:outbox:replay,ops:admission:read,ops:admission:fence,ops:admission:budget
        </code>
      </p>
    </form>
  );
}
