import { useState, type FormEvent } from "react";
import { createRole, deleteRole, updateRole, type PermissionInfo, type RoleInfo } from "./api";
import { errorMessage } from "./useApiList";

interface Props {
  roles: RoleInfo[];
  permissions: PermissionInfo[];
  onChanged: () => void;
}

// RolesPanel lists the roles and lets people with roles.manage make, change
// and delete their own. The built-in ones are fixed; the hub refuses anything
// that would give someone more than the person asking holds.
export function RolesPanel({ roles, permissions, onChanged }: Props) {
  const [editing, setEditing] = useState<RoleInfo | "new" | null>(null);
  const [error, setError] = useState("");

  const remove = async (role: RoleInfo) => {
    if (!window.confirm(`Delete the role ${role.name}?`)) return;
    setError("");
    try {
      await deleteRole(role.name);
    } catch (err) {
      setError(errorMessage(err));
    }
    onChanged();
  };

  return (
    <section aria-labelledby="roles-heading" className="panel">
      <h3 id="roles-heading">Roles</h3>
      {error && <p role="alert">{error}</p>}
      <table>
        <thead>
          <tr>
            <th>Role</th>
            <th>Can</th>
            <th>Users</th>
            <th>
              <span className="visually-hidden">Actions</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {roles.map((r) => (
            <tr key={r.name}>
              <td>
                {r.name}
                {r.builtin && " (built in)"}
                {r.description && <div className="hint">{r.description}</div>}
              </td>
              <td>{r.permissions.join(", ") || "nothing"}</td>
              <td>{r.users}</td>
              <td className="row-actions">
                {!r.builtin && (
                  <>
                    <button type="button" onClick={() => setEditing(r)}>
                      Edit {r.name}
                    </button>
                    <button type="button" onClick={() => void remove(r)}>
                      Delete role {r.name}
                    </button>
                  </>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {editing === null ? (
        <button type="button" onClick={() => setEditing("new")}>
          Add a role
        </button>
      ) : (
        <RoleForm
          key={editing === "new" ? "new" : editing.name}
          role={editing === "new" ? null : editing}
          permissions={permissions}
          onDone={() => {
            setEditing(null);
            onChanged();
          }}
          onCancel={() => setEditing(null)}
        />
      )}
    </section>
  );
}

function RoleForm({
  role,
  permissions,
  onDone,
  onCancel,
}: {
  role: RoleInfo | null;
  permissions: PermissionInfo[];
  onDone: () => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(role?.name ?? "");
  const [description, setDescription] = useState(role?.description ?? "");
  const [granted, setGranted] = useState<Set<string>>(new Set(role?.permissions));
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const toggle = (id: string) =>
    setGranted((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    setBusy(true);
    // Keep the order of the catalogue so the hub and the page agree.
    const chosen = permissions.map((p) => p.id).filter((id) => granted.has(id));
    try {
      if (role) {
        await updateRole(role.name, { name: name.trim(), description, permissions: chosen });
      } else {
        await createRole(name.trim(), description, chosen);
      }
      onDone();
    } catch (err) {
      setError(errorMessage(err));
      setBusy(false);
    }
  };

  return (
    <form className="panel" onSubmit={(e) => void submit(e)} aria-label={role ? `Edit the role ${role.name}` : "Add a role"}>
      <h4>{role ? `Edit ${role.name}` : "Add a role"}</h4>
      <label>
        Role name
        <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" required />
      </label>
      <label>
        Description
        <input value={description} onChange={(e) => setDescription(e.target.value)} autoComplete="off" />
      </label>
      <fieldset>
        <legend>Permissions</legend>
        {permissions.map((p) => (
          <label key={p.id} className="check">
            <input type="checkbox" checked={granted.has(p.id)} onChange={() => toggle(p.id)} />
            <span>
              {p.id} <span className="hint">{p.description}</span>
            </span>
          </label>
        ))}
      </fieldset>
      {error && <p role="alert">{error}</p>}
      <p className="row-actions">
        <button type="submit" disabled={busy}>
          Save role
        </button>
        <button type="button" onClick={onCancel}>
          Cancel
        </button>
      </p>
    </form>
  );
}
