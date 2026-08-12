package auth

import "slices"

const (
	PermOrdersCreate  = "orders:create"
	PermOrdersRead    = "orders:read"
	PermOrdersCancel  = "orders:cancel"
	PermOrdersReplace = "orders:replace"
	PermTradesRead    = "trades:read"
	PermPositionsRead = "positions:read"
)

var AllPermissions = []string{
	PermOrdersCreate,
	PermOrdersRead,
	PermOrdersCancel,
	PermOrdersReplace,
	PermTradesRead,
	PermPositionsRead,
}

var ReadOnlyPermissions = []string{
	PermOrdersRead,
	PermTradesRead,
	PermPositionsRead,
}

func IsValidPermission(p string) bool {
	return slices.Contains(AllPermissions, p)
}

func NormalizePermissions(in []string) ([]string, bool) {
	if len(in) == 0 {
		out := make([]string, len(AllPermissions))
		copy(out, AllPermissions)
		return out, true
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range in {
		if !IsValidPermission(p) {
			return nil, false
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out, true
}

func HasPermission(perms []string, required string) bool {
	return slices.Contains(perms, required)
}
