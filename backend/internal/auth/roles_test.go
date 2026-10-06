package auth

import "testing"

func TestHasAnyRole(t *testing.T) {
	if !HasAnyRole(RoleAdmin, RolesWrite...) {
		t.Fatal("ADMIN should write")
	}
	if HasAnyRole(RoleReadOnly, RolesWrite...) {
		t.Fatal("READ_ONLY must not write")
	}
	if !HasAnyRole(RoleAuditor, RolesAudit...) {
		t.Fatal("AUDITOR should read audit")
	}
	if HasAnyRole(RoleOperator, RolesUsers...) {
		t.Fatal("OPERATOR must not list users")
	}
}
