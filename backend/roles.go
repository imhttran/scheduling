package main

// Port of common/roles.js — ranked lowest to highest: a role satisfies a
// check for itself or anything below it.

var roles = []string{"student", "staff", "manager", "scheduler", "admin"}

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
