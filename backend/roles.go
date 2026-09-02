package main

import "sort"

// Port of common/roles.js — ranked lowest to highest: a role satisfies a
// check for itself or anything below it. The old scheduler role is gone;
// its holders are managers scoped by their teams.

var roles = []string{"student", "staff", "manager", "admin"}

func roleIndex(role string) int {
	for i, r := range roles {
		if r == role {
			return i
		}
	}
	return -1
}

func hasRole(userRole, minRole string) bool {
	ui, mi := roleIndex(userRole), roleIndex(minRole)
	return ui >= 0 && ui >= mi
}

// True if any held role satisfies the rank check (max role >= minRole).
func hasRoleAny(userRoles []string, minRole string) bool {
	for _, r := range userRoles {
		if hasRole(r, minRole) {
			return true
		}
	}
	return false
}

// Dedupes and sorts a role set by rank so stored/displayed roles are stable.
func normalizeRoles(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, r := range in {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return roleIndex(out[i]) < roleIndex(out[j]) })
	return out
}
