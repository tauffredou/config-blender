package authz_test

import (
	"context"
	"testing"

	"configblender/internal/authz"
	"configblender/internal/userdb"
)

func TestAllowed(t *testing.T) {
	ctx := context.Background()
	a, err := authz.New(ctx)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cases := []struct {
		role   userdb.Role
		action authz.Action
		want   bool
	}{
		{userdb.RoleAdmin, authz.ActionRecipesWrite, true},
		{userdb.RoleAdmin, authz.ActionSourcesWrite, true},
		{userdb.RoleAdmin, authz.ActionUsersManage, true},
		{userdb.RoleContributor, authz.ActionRecipesWrite, true},
		{userdb.RoleContributor, authz.ActionSourcesWrite, false},
		{userdb.RoleContributor, authz.ActionUsersManage, false},
		{userdb.RoleSourceManager, authz.ActionSourcesWrite, true},
		{userdb.RoleSourceManager, authz.ActionRecipesWrite, false},
		{userdb.RoleSourceManager, authz.ActionUsersManage, false},
		{userdb.RoleRead, authz.ActionRecipesWrite, false},
		{userdb.RoleRead, authz.ActionSourcesWrite, false},
		{userdb.RoleRead, authz.ActionUsersManage, false},
		{userdb.Role("bogus"), authz.ActionRecipesWrite, false},
	}
	for _, c := range cases {
		got, err := a.Allowed(ctx, c.role, c.action)
		if err != nil {
			t.Fatalf("Allowed(%q, %q): %v", c.role, c.action, err)
		}
		if got != c.want {
			t.Errorf("Allowed(%q, %q) = %v, want %v", c.role, c.action, got, c.want)
		}
	}
}
