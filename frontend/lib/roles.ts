// Ranked lowest to highest: a role satisfies a check for itself or anything below it.
export const ROLES = ["client", "staff", "admin"] as const;

export type Role = (typeof ROLES)[number];

export function hasRole(userRole: string, minRole: Role): boolean {
  return (
    (ROLES as readonly string[]).indexOf(userRole) >= ROLES.indexOf(minRole)
  );
}
