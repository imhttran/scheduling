// Ranked lowest to highest: a role satisfies a check for itself or anything
// below it. Single manager role — the old scheduler role is gone (schedulers
// merged into manager; department-level scoping is structural on the backend).
export const ROLES = ["student", "staff", "manager", "admin"] as const;

export type Role = (typeof ROLES)[number];

export function hasRole(userRole: string, minRole: Role): boolean {
  return (
    (ROLES as readonly string[]).indexOf(userRole) >= ROLES.indexOf(minRole)
  );
}

// A user can hold multiple roles; this checks membership in the held set —
// for gates the linear hierarchy doesn't fit (e.g. shift assignment is
// any manager while job management is manager/admin).
export function hasAnyRole(
  userRoles: string[] | undefined,
  ...want: Role[]
): boolean {
  return (userRoles ?? []).some((r) => (want as string[]).includes(r));
}
