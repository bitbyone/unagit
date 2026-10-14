package config

import "testing"

func TestRemovingTheFilteredServerShowsAllAgain(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.SetDir(t.TempDir())
	inst := cfg.AddInstance(Instance{URL: "https://git.example", Name: "server"})
	cfg.Filters.Server = inst.ID
	if cfg.Filters.MatchesServer("another") {
		t.Fatal("the filter matches a different server")
	}
	cfg.RemoveInstance(inst.ID)
	if cfg.Filters.Server != "" || !cfg.Filters.MatchesServer("another") {
		t.Fatal("removing the server left its filter active")
	}
}
