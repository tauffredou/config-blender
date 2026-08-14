// Package authz decides whether a role may perform an action, evaluated
// against a Rego policy (Open Policy Agent, embedded in-process — no
// separate OPA server/sidecar) rather than hardcoded in Go:
// internal/centralserver's requireAction asks this package "may role X
// perform action Y", the same question for every account kind (a human's
// session role, a service account's API key role, internal/userdb.Role)
// and every write endpoint, instead of each handler consulting its own map
// of allowed roles. Changing who can do what is now a policy.rego edit,
// not a Go code change.
package authz

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/open-policy-agent/opa/rego"

	"configblender/internal/userdb"
)

//go:embed policy.rego
var policySrc string

// Action names the operation being attempted — matches
// internal/centralserver's write endpoints one for one.
type Action string

const (
	ActionRecipesWrite Action = "recipes:write"
	ActionSourcesWrite Action = "sources:write"
	// ActionUsersManage covers every /v1/users and /v1/service-accounts
	// endpoint, reads included — account management was never split into a
	// separate read permission, only admin may see the account list at all.
	ActionUsersManage Action = "users:manage"
)

// Authorizer evaluates policy.rego's allow rule. The query is compiled
// once (New), not recompiled per request.
type Authorizer struct {
	query rego.PreparedEvalQuery
}

// New compiles the embedded policy.
func New(ctx context.Context) (*Authorizer, error) {
	query, err := rego.New(
		rego.Query("data.configblender.authz.allow"),
		rego.Module("policy.rego", policySrc),
	).PrepareForEval(ctx)
	if err != nil {
		return nil, fmt.Errorf("authz: compiling policy: %w", err)
	}
	return &Authorizer{query: query}, nil
}

// Allowed reports whether role may perform action, per policy.rego.
func (a *Authorizer) Allowed(ctx context.Context, role userdb.Role, action Action) (bool, error) {
	results, err := a.query.Eval(ctx, rego.EvalInput(map[string]any{
		"role":   string(role),
		"action": string(action),
	}))
	if err != nil {
		return false, fmt.Errorf("authz: evaluating policy: %w", err)
	}
	if len(results) == 0 || len(results[0].Expressions) == 0 {
		return false, nil
	}
	allow, _ := results[0].Expressions[0].Value.(bool)
	return allow, nil
}
