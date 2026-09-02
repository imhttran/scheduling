package main

import (
	"reflect"
	"testing"
)

func TestHasRoleAny(t *testing.T) {
	// Rank check passes if ANY held role clears the bar.
	if !hasRoleAny([]string{"staff", "manager"}, "manager") {
		t.Fatal("manager+staff should clear manager")
	}
	if hasRoleAny([]string{"staff", "manager"}, "admin") {
		t.Fatal("manager+staff should not clear admin")
	}
	if hasRoleAny([]string{"staff"}, "manager") {
		t.Fatal("staff should not clear manager")
	}
	if hasRoleAny([]string{}, "student") {
		t.Fatal("no roles should clear nothing")
	}
}

func TestNormalizeRoles(t *testing.T) {
	got := normalizeRoles([]string{"admin", "student", "manager", "admin"})
	want := []string{"student", "manager", "admin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeRoles = %v, want %v", got, want)
	}
}
