import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FormEvent, useState } from "react";
import { api, getToken, HttpError } from "../api/client";

export function UsersPage() {
  const qc = useQueryClient();
  const signedIn = Boolean(getToken());
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [msg, setMsg] = useState("");

  const listQ = useQuery({
    queryKey: ["ops-users"],
    enabled: signedIn,
    queryFn: () => api.listUsers(),
  });

  const createM = useMutation({
    mutationFn: () =>
      api.createUser({
        username: username.trim(),
        password,
        display_name: displayName.trim(),
      }),
    onSuccess: (u) => {
      setMsg(`Created user ${u.username}`);
      setUsername("");
      setPassword("");
      setDisplayName("");
      void qc.invalidateQueries({ queryKey: ["ops-users"] });
    },
    onError: (e: unknown) => {
      if (e instanceof HttpError) setMsg(`${e.status}: ${e.message}`);
      else if (e instanceof Error) setMsg(e.message);
    },
  });

  function onCreate(e: FormEvent) {
    e.preventDefault();
    setMsg("");
    createM.mutate();
  }

  if (!signedIn) {
    return <p className="muted">Sign in as admin to manage operators.</p>;
  }

  return (
    <div>
      <h2>Operator users</h2>
      <p className="muted">Create additional admins (requires ops:users:write). Default bootstrap user is auto-created on sale startup.</p>
      {msg ? <p className="info" role="status">{msg}</p> : null}

      <form className="detail card" onSubmit={onCreate}>
        <h3>Create user</h3>
        <label htmlFor="nu">Username</label>
        <input id="nu" value={username} onChange={(e) => setUsername(e.target.value)} required minLength={3} />
        <label htmlFor="np">Password</label>
        <input id="np" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
        <label htmlFor="nd">Display name</label>
        <input id="nd" value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
        <div className="auth-row">
          <button type="submit" disabled={createM.isPending}>
            {createM.isPending ? "Creating…" : "Create user"}
          </button>
        </div>
      </form>

      {listQ.isLoading ? <p>Loading…</p> : null}
      {listQ.data ? (
        <ul className="list">
          {listQ.data.items.map((u) => (
            <li key={u.id}>
              <code>{u.username}</code>
              {u.display_name ? ` — ${u.display_name}` : ""}
              {u.active ? "" : " (disabled)"}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
