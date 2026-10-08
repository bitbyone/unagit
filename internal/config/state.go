package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// State is what unagit learns by being used on this machine - where things
// were opened, how the Open… form was last filled, whom merge requests
// were given to. It is not configuration: config.yaml is the user's
// choices alone, portable and kept in a dotfiles repository, and must not
// change because the user worked. State lives beside the indexes, in
// state.json, which goes with the machine or with whatever syncs the
// directory, never with the configuration.
type State struct {
	// PlaceUses counts the places things were opened in - by what went
	// there (agent, attach), then by place - so a picker of places starts
	// on the usual one rather than the one used last.
	PlaceUses map[string]map[string]int `json:"place_uses,omitempty"`
	// AgentPlace is where an agent was last opened, which the next one is
	// offered first.
	AgentPlace string `json:"agent_place,omitempty"`
	// OpenForm is what Open… was last given for each kind of row
	// (repository, merge_request, worktree), so it opens filled in so.
	OpenForm map[string]OpenChoice `json:"open_form,omitempty"`
	// PeopleUses counts whom merge requests were given to - by server,
	// then by role (RoleAssignee, RoleReviewer), then by user name - so the
	// lists of people start with the usual ones.
	PeopleUses map[string]map[string]map[string]int `json:"people_uses,omitempty"`
}

// The roles PeopleUses counts by.
const (
	RoleAssignee = "assignee"
	RoleReviewer = "reviewer"
)

// UsePlace counts one more opening of what in place.
func (s *State) UsePlace(what, place string) {
	if s.PlaceUses == nil {
		s.PlaceUses = map[string]map[string]int{}
	}
	if s.PlaceUses[what] == nil {
		s.PlaceUses[what] = map[string]int{}
	}
	s.PlaceUses[what][place]++
}

// PersonUses is how often each user was given a merge request in a role on
// a server.
func (s *State) PersonUses(instance, role string) map[string]int {
	return s.PeopleUses[instance][role]
}

// UsePerson counts one more time username was given a merge request in a
// role on a server.
func (s *State) UsePerson(instance, role, username string) {
	if s.PeopleUses == nil {
		s.PeopleUses = map[string]map[string]map[string]int{}
	}
	if s.PeopleUses[instance] == nil {
		s.PeopleUses[instance] = map[string]map[string]int{}
	}
	if s.PeopleUses[instance][role] == nil {
		s.PeopleUses[instance][role] = map[string]int{}
	}
	s.PeopleUses[instance][role][username]++
}

// StatePath is where this configuration's State is kept.
func (c *Config) StatePath() string { return filepath.Join(c.Dir(), "state.json") }

// loadState reads state.json; a missing or broken one is an empty State -
// it is a convenience, not something to fail on.
func (c *Config) loadState() {
	b, err := os.ReadFile(c.StatePath())
	if err != nil {
		return
	}
	var s State
	if json.Unmarshal(b, &s) == nil {
		c.State = s
	}
}

// SaveState writes the State, through a temporary file so a reader never
// sees half of it.
func (c *Config) SaveState() error {
	b, err := json.MarshalIndent(c.State, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.Dir(), 0o700); err != nil {
		return err
	}
	tmp := c.StatePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.StatePath())
}

// moveStateOut takes what older versions kept in config.yaml into the
// State, and reports whether there was any, so the file can be written
// again without it.
func (c *Config) moveStateOut() bool {
	moved := false
	in := &c.Integrations
	if len(in.LegacyPlaceUses) > 0 {
		for what, places := range in.LegacyPlaceUses {
			for place, n := range places {
				for range n {
					c.State.UsePlace(what, place)
				}
			}
		}
		in.LegacyPlaceUses, moved = nil, true
	}
	if in.LegacyAgentPlace != "" {
		if c.State.AgentPlace == "" {
			c.State.AgentPlace = in.LegacyAgentPlace
		}
		in.LegacyAgentPlace, moved = "", true
	}
	if len(in.LegacyOpenForm) > 0 {
		for kind, choice := range in.LegacyOpenForm {
			if _, ok := c.State.OpenForm[kind]; !ok {
				if c.State.OpenForm == nil {
					c.State.OpenForm = map[string]OpenChoice{}
				}
				c.State.OpenForm[kind] = choice
			}
		}
		in.LegacyOpenForm, moved = nil, true
	}
	for i := range c.Instances {
		inst := &c.Instances[i]
		if len(inst.LegacyPeopleUses) == 0 {
			continue
		}
		for role, people := range inst.LegacyPeopleUses {
			for name, n := range people {
				for range n {
					c.State.UsePerson(inst.ID, role, name)
				}
			}
		}
		inst.LegacyPeopleUses, moved = nil, true
	}
	return moved
}
