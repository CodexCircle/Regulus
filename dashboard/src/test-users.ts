import type { PermissionInfo } from "./api";

// The permission catalogue the hub lists (internal/auth/permissions.go).
export const CATALOGUE: PermissionInfo[] = [
  { id: "servers.read", description: "See the servers and their metrics" },
  { id: "servers.manage", description: "Add, rename and remove servers" },
  { id: "layout.edit", description: "Arrange the dashboard" },
  { id: "users.manage", description: "Manage accounts" },
  { id: "roles.manage", description: "Manage roles" },
  { id: "audit.read", description: "Read the audit log" },
];

export const ADMIN_PERMISSIONS = CATALOGUE.map((p) => p.id);
export const VIEWER_PERMISSIONS = ["servers.read", "layout.edit"];
