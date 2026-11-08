package store

import "testing"

func TestPlanRepair(t *testing.T) {
	const password, rights = true, 2
	tests := []struct {
		name  string
		users []User
		kind  repairKind
		id    string
	}{
		{name: "a new install has no users", kind: repairNotNeeded},
		{
			name: "someone can already log in",
			users: []User{
				{ID: "admin", Name: "a", HasPassword: password, AdminSites: rights},
				{ID: "lost", Name: "b", AdminSites: rights},
			},
			kind: repairNotNeeded,
		},
		{
			// What a real backup from UniFi OS looks like
			name: "same name: the owner's password is on another user",
			users: []User{
				{ID: "person", Name: "Alex", HasPassword: password},
				{ID: "owner", Name: "alex", Owner: true, AdminSites: rights},
				{ID: "integration", Name: "integration", HasPassword: password},
			},
			kind: repairGrantRights, id: "person",
		},
		{
			name: "same email",
			users: []User{
				{ID: "person", Name: "local", Email: "A@example.invalid", HasPassword: password},
				{ID: "owner", Name: "owner", Email: "a@example.invalid", Owner: true, AdminSites: rights},
			},
			kind: repairGrantRights, id: "person",
		},
		{
			name: "no email is not the same email",
			users: []User{
				{ID: "other", Name: "other", HasPassword: password},
				{ID: "owner", Name: "owner", Owner: true, AdminSites: rights},
			},
			kind: repairTemporaryPassword, id: "owner",
		},
		{
			name: "two users match: it can't tell, so the owner gets a temporary password",
			users: []User{
				{ID: "one", Name: "alex", HasPassword: password},
				{ID: "two", Name: "ALEX", HasPassword: password},
				{ID: "owner", Name: "alex", Owner: true, AdminSites: rights},
			},
			kind: repairTemporaryPassword, id: "owner",
		},
		{
			name: "an unrelated user with a password never gains rights",
			users: []User{
				{ID: "integration", Name: "integration", HasPassword: password},
				{ID: "owner", Name: "owner", Owner: true, AdminSites: rights},
			},
			kind: repairTemporaryPassword, id: "owner",
		},
		{
			name:  "the only admin, with no owner",
			users: []User{{ID: "admin", Name: "a", AdminSites: rights}},
			kind:  repairTemporaryPassword, id: "admin",
		},
		{
			name: "several admins and no owner: not clear",
			users: []User{
				{ID: "one", Name: "a", AdminSites: rights},
				{ID: "two", Name: "b", AdminSites: rights},
			},
			kind: repairUnclear,
		},
		{
			name:  "no admin at all: not clear, and an user alone isn't promoted",
			users: []User{{ID: "alone", Name: "alone", HasPassword: password}},
			kind:  repairUnclear,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			kind, user := planRepair(test.users)
			if kind != test.kind || user.ID != test.id {
				t.Errorf("got %d %q, want %d %q", kind, user.ID, test.kind, test.id)
			}
		})
	}
}

func TestFindUser(t *testing.T) {
	users := []User{
		{ID: "1", Name: "alex", Email: "a@example.invalid"},
		{ID: "2", Name: "alex", Email: "b@example.invalid"},
		{ID: "3", Name: "other"},
	}
	for who, want := range map[string]string{"1": "1", "B@example.invalid": "2", "OTHER": "3"} {
		got, err := FindUser(users, who)
		if err != nil || got.ID != want {
			t.Errorf("FindUser(%q) = %q, %v; want %q", who, got.ID, err, want)
		}
	}
	for _, who := range []string{"alex", "nobody", ""} {
		if got, err := FindUser(users, who); err == nil {
			t.Errorf("FindUser(%q) = %q, want an error", who, got.ID)
		}
	}
}
