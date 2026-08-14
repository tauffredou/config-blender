package configblender.authz

import rego.v1

# The one authorization policy for every write endpoint on the central
# service, regardless of which auth method produced the caller's role
# (internal/userdb: human session login, service-account API key) —
# internal/authz.Authorizer evaluates this against {role, action} on every
# requireAction call (internal/centralserver). Keeping the grant here
# rather than in a Go map/switch is the point: changing who can do what is
# a policy edit, not a code change.
#
# input.role is one of internal/userdb.Role: "admin", "source-manager",
# "contributor", "read". input.action names the operation being
# attempted, matching internal/authz's Action constants one for one.

default allow := false

# admin is a strict superset of every other role — no action-specific rule
# needed for it, mirroring the "no hierarchy beyond RoleAdmin implicitly
# satisfying every check" invariant internal/userdb documents.
allow if input.role == "admin"

allow if {
	input.role == "contributor"
	input.action == "recipes:write"
}

allow if {
	input.role == "source-manager"
	input.action == "sources:write"
}

# "users:manage" (account management, /v1/users and /v1/service-accounts,
# reads included) has no rule beyond the blanket admin one above — no other
# role may touch it.

# "read" grants no action at all — the floor of the RBAC model
# (internal/userdb.RoleRead) — so it never appears on the right-hand side
# of an allow rule here.
