package auth

const (
	RoleSuperAdmin = "SUPER_ADMIN"
	RoleAdmin      = "ADMIN"
	RoleOperator   = "OPERATOR"
	RoleAuditor    = "AUDITOR"
	RoleReadOnly   = "READ_ONLY"
)

// RolesAll is any authenticated admin user.
var RolesAll = []string{RoleSuperAdmin, RoleAdmin, RoleOperator, RoleAuditor, RoleReadOnly}

// RolesWrite covers broker/token/adapter mutations.
var RolesWrite = []string{RoleSuperAdmin, RoleAdmin}

// RolesOps covers adapter lifecycle plus write.
var RolesOps = []string{RoleSuperAdmin, RoleAdmin, RoleOperator}

// RolesAudit covers logs and audit trails.
var RolesAudit = []string{RoleSuperAdmin, RoleAdmin, RoleOperator, RoleAuditor}

// RolesUsers is user-directory access.
var RolesUsers = []string{RoleSuperAdmin}

func HasAnyRole(have string, allowed ...string) bool {
	for _, a := range allowed {
		if have == a {
			return true
		}
	}
	return false
}
