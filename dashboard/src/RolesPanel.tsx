import { useState, type FormEvent } from "react";
import { createRole, deleteRole, updateRole, type PermissionInfo, type RoleInfo } from "./api";
import { useT } from "./i18n";
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
  const t = useT();
  const [editing, setEditing] = useState<RoleInfo | "new" | null>(null);
  const [error, setError] = useState("");

  const remove = async (role: RoleInfo) => {
    if (!window.confirm(t("users.roles.confirmDelete", { name: role.name }))) return;
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
      <h3 id="roles-heading">{t("users.roles.title")}</h3>
      {error && <p role="alert">{error}</p>}
      <table>
        <thead>
          <tr>
            <th>{t("users.roles.colRole")}</th>
            <th>{t("users.roles.colCan")}</th>
            <th>{t("users.roles.colUsers")}</th>
            <th>
              <span className="visually-hidden">{t("users.roles.colActions")}</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {roles.map((r) => (
            <tr key={r.name}>
              <td>
                {r.name}
                {r.builtin && t("users.roles.builtIn")}
                {r.description && <div className="hint">{r.description}</div>}
              </td>
              <td>{r.permissions.join(", ") || t("users.roles.nothing")}</td>
              <td>{r.users}</td>
              <td className="row-actions">
                {!r.builtin && (
                  <>
                    <button type="button" onClick={() => setEditing(r)}>
                      {t("users.roles.edit", { name: r.name })}
                    </button>
                    <button type="button" onClick={() => void remove(r)}>
                      {t("users.roles.delete", { name: r.name })}
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
          {t("users.roles.add")}
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
  const t = useT();
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
    <form className="panel" onSubmit={(e) => void submit(e)} aria-label={role ? t("users.roles.formLabel", { name: role.name }) : t("users.roles.add")}>
      <h4>{role ? t("users.roles.formTitle", { name: role.name }) : t("users.roles.add")}</h4>
      <label>
        {t("users.roles.name")}
        <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" required />
      </label>
      <label>
        {t("users.roles.description")}
        <input value={description} onChange={(e) => setDescription(e.target.value)} autoComplete="off" />
      </label>
      <fieldset>
        <legend>{t("users.roles.permissions")}</legend>
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
          {t("users.roles.save")}
        </button>
        <button type="button" onClick={onCancel}>
          {t("users.roles.cancel")}
        </button>
      </p>
    </form>
  );
}
