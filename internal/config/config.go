// Package config loads and stores the unagit configuration and index files.
//
// Everything in here is editable from the Settings tab; the file is only the
// place it ends up.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Scopes a selected group can have.
const (
	// ScopeGroup takes only the projects that sit directly in the group.
	ScopeGroup = "group"
	// ScopeSubgroups takes the whole tree below the group.
	ScopeSubgroups = "subgroups"
)

// Group is a GitLab group the user selected in the settings view.
type Group struct {
	ID       int    `yaml:"id" json:"id"`
	FullPath string `yaml:"full_path" json:"full_path"`
	Name     string `yaml:"name" json:"name"`
	// Scope is ScopeGroup or ScopeSubgroups; an empty value means subgroups,
	// which is what older configurations did.
	Scope string `yaml:"scope,omitempty" json:"scope,omitempty"`
	// RootDir overrides where this group's projects are cloned. A relative
	// path is taken from the root it would otherwise inherit.
	RootDir string `yaml:"root_dir,omitempty" json:"root_dir,omitempty"`
}

// IncludesSubgroups reports whether the whole tree below the group is wanted.
func (g Group) IncludesSubgroups() bool { return g.Scope != ScopeGroup }

// Owns reports whether projectPath belongs to this group under its scope.
func (g Group) Owns(projectPath string) bool {
	rest, ok := strings.CutPrefix(projectPath, g.FullPath+"/")
	if !ok {
		return false
	}
	if g.IncludesSubgroups() {
		return true
	}
	return !strings.Contains(rest, "/")
}

// Instance is one server - a GitLab installation or a GitHub account - with
// its own token and its own group selection.
type Instance struct {
	// ProjectDirs are exact clone destinations, keyed by repository namespace.
	ProjectDirs map[string]string `yaml:"project_dirs,omitempty" json:"project_dirs,omitempty"`
	// ID is a stable key: it ties the cached indexes and the stored token to
	// this instance and never changes once assigned.
	ID string `yaml:"id" json:"id"`
	// Kind is forge.KindGitLab or forge.KindGitHub; empty means GitLab, which
	// is all unagit spoke to at first.
	Kind string `yaml:"kind,omitempty" json:"kind,omitempty"`
	Name string `yaml:"name" json:"name"`
	URL  string `yaml:"url" json:"url"`
	// RootDir overrides the global root for everything on this instance.
	RootDir string `yaml:"root_dir,omitempty" json:"root_dir,omitempty"`
	// CloneProtocol is ProtocolHTTPS or ProtocolSSH. Over SSH git uses your
	// key and the token is only ever spent on the API.
	CloneProtocol string  `yaml:"clone_protocol,omitempty" json:"clone_protocol,omitempty"`
	Groups        []Group `yaml:"groups" json:"groups"`
	// PeopleUses counts whom merge requests were given to on this server -
	// by role (RoleAssignee, RoleReviewer), then by user name - so the
	// lists of people start with the usual ones.
	PeopleUses map[string]map[string]int `yaml:"people_uses,omitempty" json:"people_uses,omitempty"`
}

// The roles PeopleUses counts by.
const (
	RoleAssignee = "assignee"
	RoleReviewer = "reviewer"
)

// UsePerson counts one more time username was given a merge request in a
// role.
func (i *Instance) UsePerson(role, username string) {
	if i.PeopleUses == nil {
		i.PeopleUses = map[string]map[string]int{}
	}
	if i.PeopleUses[role] == nil {
		i.PeopleUses[role] = map[string]int{}
	}
	i.PeopleUses[role][username]++
}

// Clone protocols. They mirror the workspace's, which cannot be imported here
// without a cycle.
const (
	ProtocolHTTPS = "https"
	ProtocolSSH   = "ssh"
)

// Protocol is how this instance's repositories are cloned, defaulting to
// HTTPS, which is what unagit did before it could do anything else.
func (i Instance) Protocol() string {
	if i.CloneProtocol == ProtocolSSH {
		return ProtocolSSH
	}
	return ProtocolHTTPS
}

// Label is what the instance is called in the interface.
func (i Instance) Label() string {
	if i.Name != "" {
		return i.Name
	}
	return Host(i.URL)
}

// IsGitHub reports whether this is a github.com account.
func (i Instance) IsGitHub() bool { return i.Kind == KindGitHub }

// Kinds of server. They mirror forge's, which cannot be imported here without
// a cycle.
const (
	KindGitLab = "gitlab"
	KindGitHub = "github"
)

// What NerdFont can say besides nothing, which is to tell from the
// terminal.
const (
	NerdFontOn  = "on"
	NerdFontOff = "off"
)

// GitHubURL is the only address a GitHub instance can have: github.com is not
// self hosted.
const GitHubURL = "https://github.com"

// GroupScope returns the scope a group is selected with, or "" when it is not
// selected at all.
func (i *Instance) GroupScope(id int) string {
	for _, g := range i.Groups {
		if g.ID == id {
			if g.Scope == "" {
				return ScopeSubgroups
			}
			return g.Scope
		}
	}
	return ""
}

// HasGroup reports whether the group id is selected.
func (i *Instance) HasGroup(id int) bool { return i.GroupScope(id) != "" }

// Group returns the selected group with this id, or nil.
func (i *Instance) Group(id int) *Group {
	for idx := range i.Groups {
		if i.Groups[idx].ID == id {
			return &i.Groups[idx]
		}
	}
	return nil
}

// ToggleGroup selects or unselects a group outright. GitHub has no subgroups,
// so there is nothing to cycle through there.
func (i *Instance) ToggleGroup(g Group) string {
	if i.GroupScope(g.ID) != "" {
		for idx, existing := range i.Groups {
			if existing.ID == g.ID {
				i.Groups = append(i.Groups[:idx], i.Groups[idx+1:]...)
				break
			}
		}
		return ""
	}
	g.Scope = ScopeGroup
	i.Groups = append(i.Groups, g)
	return ScopeGroup
}

// CycleGroup steps a group through "not selected" -> "this group only" ->
// "including subgroups" -> "not selected" and returns the new scope.
func (i *Instance) CycleGroup(g Group) string {
	switch i.GroupScope(g.ID) {
	case "":
		g.Scope = ScopeGroup
		i.Groups = append(i.Groups, g)
		return ScopeGroup
	case ScopeGroup:
		if existing := i.Group(g.ID); existing != nil {
			existing.Scope = ScopeSubgroups
		}
		return ScopeSubgroups
	default:
		for idx, existing := range i.Groups {
			if existing.ID == g.ID {
				i.Groups = append(i.Groups[:idx], i.Groups[idx+1:]...)
				break
			}
		}
		return ""
	}
}

// Orders the lists can be sorted in.
const (
	// SortActivity puts what moved most recently first.
	SortActivity = "activity"
	// SortFrecency puts frequently and recently visited directories first.
	SortFrecency = "frecency"
	// SortName sorts by path, and merge requests by project then number.
	SortName = "name"
	// SortEdits puts the most files with uncommitted changes first:
	// repositories and worktrees.
	SortEdits = "edits"
	// SortSize puts the repositories that take the most disk first.
	SortSize = "size"
	// SortRemote puts the repositories furthest behind origin first, then
	// any other that is not in step with it.
	SortRemote = "remote"
	// SortNew puts the merge requests with the most commits since their
	// last review first.
	SortNew = "new"
	// SortComments puts the merge requests with the most open threads first.
	SortComments = "comments"
)

// SortsOf are the orders a list can be drawn in, the shared two first.
func SortsOf(list string) []string {
	switch list {
	case ListRepositories:
		return []string{SortActivity, SortName, SortEdits, SortSize, SortRemote, SortFrecency}
	case ListMergeRequests:
		return []string{SortActivity, SortName, SortNew, SortComments}
	case ListWorktrees:
		return []string{SortActivity, SortName, SortEdits, SortFrecency}
	}
	return []string{SortActivity, SortName}
}

// Hidden is one project kept out of the lists.
type Hidden struct {
	Instance string `yaml:"instance" json:"instance"`
	Path     string `yaml:"path" json:"path"`
}

// Filters are the view settings the project and merge request lists share.
// They are a lasting preference, so they live in the configuration rather
// than in the session.
type Filters struct {
	// ClonedOnly narrows both lists to projects that are on disk.
	ClonedOnly bool `yaml:"cloned_only,omitempty" json:"cloned_only,omitempty"`
	// Sort is SortActivity or SortName; empty means activity. It is the
	// order of a list that has none of its own in Sorts, and all a
	// configuration written before Sorts says.
	Sort string `yaml:"sort,omitempty" json:"sort,omitempty"`
	// Sorts is each list's own order, by the list (ListRepositories,
	// ListMergeRequests, ListWorktrees): the lists can be sorted by what
	// only one of them has, the size of a clone or the comments.
	Sorts map[string]string `yaml:"sorts,omitempty" json:"sorts,omitempty"`
	// Hidden are the projects kept out of both lists.
	Hidden []Hidden `yaml:"hidden,omitempty" json:"hidden,omitempty"`
	// GroupByProject gathers the merge requests under the project they
	// belong to. It means nothing to the project list.
	GroupByProject bool `yaml:"group_by_project,omitempty" json:"group_by_project,omitempty"`
	// GroupRepositories gathers the repositories under the group or subgroup
	// (on GitHub the owner) they live in.
	GroupRepositories bool `yaml:"group_repositories,omitempty" json:"group_repositories,omitempty"`
	// Favourites are the starred repositories and merge requests.
	Favourites []Favourite `yaml:"favourites,omitempty" json:"favourites,omitempty"`
	// HideTags leaves the tags out of the repository list; they still
	// filter and are still found by /. It is what HiddenColumns says of
	// the tags now, and is read for a configuration written before them.
	HideTags bool `yaml:"hide_tags,omitempty" json:"hide_tags,omitempty"`
	// HiddenColumns are the columns left out of each list, by the list
	// (ListRepositories, ListMergeRequests, ListWorktrees) and the
	// column's name.
	HiddenColumns map[string][]string `yaml:"hidden_columns,omitempty" json:"hidden_columns,omitempty"`
	// ShownColumns are the columns hidden until asked for (hiddenAtFirst)
	// that the user has shown.
	ShownColumns map[string][]string `yaml:"shown_columns,omitempty" json:"shown_columns,omitempty"`
	// Tags narrow the repositories to those wearing any of them.
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	// FavouritesInPlace leaves the favourites among the other rows. By
	// default they come first, set apart from the rest.
	FavouritesInPlace bool `yaml:"favourites_in_place,omitempty" json:"favourites_in_place,omitempty"`
	// HiddenAuthors keep their merge requests out of the list - bots, most
	// often, whose merge requests would bury the rest. HiddenMRs do the same
	// for repositories: their merge requests are kept out while the
	// repository itself stays listed, unlike Hidden. ShowHiddenAuthors turns
	// both off for a while without forgetting what they are.
	HiddenAuthors     []HiddenAuthor `yaml:"hidden_authors,omitempty" json:"hidden_authors,omitempty"`
	HiddenMRs         []Hidden       `yaml:"hidden_merge_requests,omitempty" json:"hidden_merge_requests,omitempty"`
	ShowHiddenAuthors bool           `yaml:"show_hidden_authors,omitempty" json:"show_hidden_authors,omitempty"`
	// OnlyMine and OnlyToReview narrow the merge requests to those you
	// wrote, and those you are asked to review or are assigned; with both,
	// to either. HideDrafts leaves the drafts out.
	OnlyMine     bool `yaml:"only_mine,omitempty" json:"only_mine,omitempty"`
	OnlyToReview bool `yaml:"only_to_review,omitempty" json:"only_to_review,omitempty"`
	HideDrafts   bool `yaml:"hide_drafts,omitempty" json:"hide_drafts,omitempty"`
}

// HiddenAuthor is an author of a server whose merge requests are not listed.
type HiddenAuthor struct {
	Instance string `yaml:"instance" json:"instance"`
	Username string `yaml:"username" json:"username"`
}

// HidesAuthor reports whether an author's merge requests are kept out of the
// list now.
func (f *Filters) HidesAuthor(instance, username string) bool {
	if f.ShowHiddenAuthors {
		return false
	}
	for _, h := range f.HiddenAuthors {
		if h.Instance == instance && h.Username == username {
			return true
		}
	}
	return false
}

// HidesMRsOf reports whether a repository's merge requests are kept out of
// the list now.
func (f *Filters) HidesMRsOf(instance, path string) bool {
	if f.ShowHiddenAuthors {
		return false
	}
	for _, h := range f.HiddenMRs {
		if h.Instance == instance && h.Path == path {
			return true
		}
	}
	return false
}

// ToggleMRsOf hides a repository's merge requests, or shows them again, and
// reports whether they are now hidden. Like ToggleAuthor, hiding turns the
// filter back on.
func (f *Filters) ToggleMRsOf(instance, path string) bool {
	for i, h := range f.HiddenMRs {
		if h.Instance == instance && h.Path == path {
			f.HiddenMRs = append(f.HiddenMRs[:i], f.HiddenMRs[i+1:]...)
			return false
		}
	}
	f.HiddenMRs = append(f.HiddenMRs, Hidden{Instance: instance, Path: path})
	f.ShowHiddenAuthors = false
	return true
}

// ToggleAuthor hides an author's merge requests, or shows them again, and
// reports whether the author is now hidden. Hiding one turns the filter
// back on, since that is what was asked for.
func (f *Filters) ToggleAuthor(instance, username string) bool {
	for i, h := range f.HiddenAuthors {
		if h.Instance == instance && h.Username == username {
			f.HiddenAuthors = append(f.HiddenAuthors[:i], f.HiddenAuthors[i+1:]...)
			return false
		}
	}
	f.HiddenAuthors = append(f.HiddenAuthors, HiddenAuthor{Instance: instance, Username: username})
	f.ShowHiddenAuthors = false
	return true
}

// Favourite is a starred repository, or with an IID one of its merge
// requests.
type Favourite struct {
	Instance string `yaml:"instance" json:"instance"`
	Path     string `yaml:"path" json:"path"`
	IID      int    `yaml:"iid,omitempty" json:"iid,omitempty"`
}

// The lists whose columns can be hidden, as HiddenColumns names them.
const (
	ListRepositories  = "repositories"
	ListMergeRequests = "merge_requests"
	ListWorktrees     = "worktrees"
)

// hiddenAtFirst are the columns a list leaves out until the user shows
// them: worth having, not worth the room on every screen.
var hiddenAtFirst = map[string][]string{
	ListMergeRequests: {"assignees", "reviewers"},
}

// HidesColumn says whether a list leaves a column out.
func (f *Filters) HidesColumn(list, column string) bool {
	if list == ListRepositories && column == "tags" && f.HideTags {
		return true
	}
	if slices.Contains(hiddenAtFirst[list], column) {
		return !slices.Contains(f.ShownColumns[list], column)
	}
	return slices.Contains(f.HiddenColumns[list], column)
}

// ToggleColumn hides a column of a list, or shows it again.
func (f *Filters) ToggleColumn(list, column string) {
	if slices.Contains(hiddenAtFirst[list], column) {
		f.ShownColumns = toggled(f.ShownColumns, list, column)
		return
	}
	if f.HidesColumn(list, column) && list == ListRepositories && column == "tags" {
		f.HideTags = false
		if !slices.Contains(f.HiddenColumns[list], column) {
			return
		}
	}
	f.HiddenColumns = toggled(f.HiddenColumns, list, column)
}

// toggled is a list's columns with one added, or taken out when it was
// there; nil when none is left.
func toggled(m map[string][]string, list, column string) map[string][]string {
	if !slices.Contains(m[list], column) {
		if m == nil {
			m = map[string][]string{}
		}
		m[list] = append(m[list], column)
		return m
	}
	kept := slices.DeleteFunc(slices.Clone(m[list]), func(c string) bool { return c == column })
	if len(kept) == 0 {
		delete(m, list)
		if len(m) == 0 {
			return nil
		}
		return m
	}
	m[list] = kept
	return m
}

// FavouritesFirst reports whether the favourites lead the lists.
func (f *Filters) FavouritesFirst() bool { return !f.FavouritesInPlace }

// IsFavourite reports whether a repository (iid 0) or a merge request is
// starred.
func (f *Filters) IsFavourite(instance, path string, iid int) bool {
	for _, fav := range f.Favourites {
		if fav == (Favourite{Instance: instance, Path: path, IID: iid}) {
			return true
		}
	}
	return false
}

// ForgetClosedFavourites drops the starred merge requests of the asked
// servers that are no longer open, so the list of favourites does not grow
// with every merge request ever starred. Repositories and the servers that
// were not asked keep theirs. It reports how many went.
func (f *Filters) ForgetClosedFavourites(asked map[string]bool, open map[Favourite]bool) int {
	kept := f.Favourites[:0]
	for _, fav := range f.Favourites {
		if fav.IID == 0 || !asked[fav.Instance] || open[fav] {
			kept = append(kept, fav)
		}
	}
	gone := len(f.Favourites) - len(kept)
	if len(kept) == 0 {
		kept = nil
	}
	f.Favourites = kept
	return gone
}

// ToggleFavourite stars or unstars a repository (iid 0) or a merge request
// and reports the new state.
func (f *Filters) ToggleFavourite(instance, path string, iid int) bool {
	this := Favourite{Instance: instance, Path: path, IID: iid}
	for i, fav := range f.Favourites {
		if fav == this {
			f.Favourites = append(f.Favourites[:i], f.Favourites[i+1:]...)
			return false
		}
	}
	f.Favourites = append(f.Favourites, this)
	return true
}

// Order is the sort to apply to a list, normalised: its own when it has
// one that it can be sorted by, the shared one otherwise.
func (f *Filters) Order(list string) string {
	if own, ok := f.Sorts[list]; ok && slices.Contains(SortsOf(list), own) {
		return own
	}
	if f.Sort == SortName {
		return SortName
	}
	return SortActivity
}

// SetOrder gives a list an order of its own.
func (f *Filters) SetOrder(list, order string) {
	if f.Sorts == nil {
		f.Sorts = map[string]string{}
	}
	f.Sorts[list] = order
}

// IsHidden reports whether a project is kept out of the lists.
func (f *Filters) IsHidden(instance, path string) bool {
	for _, h := range f.Hidden {
		if h.Instance == instance && h.Path == path {
			return true
		}
	}
	return false
}

// ToggleHidden hides or unhides a project and reports the new state.
func (f *Filters) ToggleHidden(instance, path string) bool {
	for i, h := range f.Hidden {
		if h.Instance == instance && h.Path == path {
			f.Hidden = append(f.Hidden[:i], f.Hidden[i+1:]...)
			return false
		}
	}
	f.Hidden = append(f.Hidden, Hidden{Instance: instance, Path: path})
	return true
}

// ShowAll unhides everything and reports how many were hidden.
func (f *Filters) ShowAll() int {
	n := len(f.Hidden)
	f.Hidden = nil
	return n
}

// Active reports whether anything is narrowing the lists.
func (f *Filters) Active() bool { return f.ClonedOnly || len(f.Hidden) > 0 }

type Integrations struct {
	// Yazi browses directories and hands chosen files to the favourite editor.
	// Unset, it is on whenever yazi is installed.
	Yazi *bool `yaml:"yazi,omitempty"`
	// Zoxide remembers opened directories and ranks them by visits. Unset,
	// it is on whenever zoxide is installed.
	Zoxide *bool `yaml:"zoxide,omitempty"`
	Incomm bool  `yaml:"incomm,omitempty"`
	// Hunk shows the changes of a clone, a worktree or a review with D. Unset,
	// it is on whenever hunk is installed; set, it is what the user chose.
	Hunk *bool `yaml:"hunk,omitempty"`
	// Chezmoi opens the repository chezmoi keeps the dotfiles in where
	// chezmoi has it, instead of cloning it again. Unset, it is on whenever
	// chezmoi is installed.
	Chezmoi *bool `yaml:"chezmoi,omitempty"`
	// Zellij opens editors and agents in Zellij's tabs and splits while
	// unagit runs in it. Unset, it is on whenever zellij is installed.
	Zellij *bool `yaml:"zellij,omitempty"`
	// Herdr opens editors in herdr's tabs and splits while unagit runs in
	// it, and starts agents there from anywhere. Unset, it is on whenever
	// herdr is installed.
	Herdr *bool `yaml:"herdr,omitempty"`
	// Ghostty opens editors and agents in Ghostty windows and splits on a
	// Mac. Unset, it is on whenever Ghostty is installed.
	Ghostty *bool `yaml:"ghostty,omitempty"`
	// Notifications is where a watched pipeline's news goes while the user
	// looks elsewhere: "" for the terminal where it can and the system
	// otherwise, "terminal", "system", or "off" (internal/notify).
	Notifications string `yaml:"notifications,omitempty"`
	// Agents are the coding agents a directory can be opened in, by their
	// id (agents.All). One not named is on whenever it is installed.
	Agents map[string]bool `yaml:"agents,omitempty"`
	// Editors are the editors things open in, by their id (nvim, idea,
	// code, zed, custom). One not named is on whenever it is installed;
	// one turned off is offered nowhere.
	Editors map[string]bool `yaml:"editors,omitempty"`
	// AgentPlace is where an agent was last opened, which the next one is
	// offered first.
	AgentPlace string `yaml:"agent_place,omitempty"`
	// PlaceUses counts the places things were opened in - by what went
	// there (agent, attach), then by place - so a picker of places starts
	// on the usual one rather than the one used last.
	PlaceUses map[string]map[string]int `yaml:"place_uses,omitempty"`
	// OpenForm is what Open… was last given for each kind of row
	// (repository, merge_request, worktree), so it opens filled in so.
	OpenForm map[string]OpenChoice `yaml:"open_form,omitempty"`
}

// OpenChoice is one filling of Open…: the mode of a merge request (branch
// or review), the tool (editor:<id> or agent:<id>) and the place.
type OpenChoice struct {
	Mode  string `yaml:"mode,omitempty"`
	With  string `yaml:"with,omitempty"`
	Where string `yaml:"where,omitempty"`
}

// UsePlace counts one more opening of what in place.
func (i *Integrations) UsePlace(what, place string) {
	if i.PlaceUses == nil {
		i.PlaceUses = map[string]map[string]int{}
	}
	if i.PlaceUses[what] == nil {
		i.PlaceUses[what] = map[string]int{}
	}
	i.PlaceUses[what][place]++
}

// DefaultToastSeconds is how long a toast stays unless the user chose.
const DefaultToastSeconds = 5

// ToastLife is how long a toast stays.
func (c *Config) ToastLife() time.Duration {
	if c.ToastSeconds <= 0 {
		return DefaultToastSeconds * time.Second
	}
	return time.Duration(c.ToastSeconds) * time.Second
}

// Config is the on-disk configuration (~/.config/unagit/config.yaml).
// Tokens are not stored here; they live encrypted in the vault.
type Config struct {
	Integrations Integrations `yaml:"integrations,omitempty"`
	RootDir      string       `yaml:"root_dir"`
	// FavouriteEditor is the editor everything opens in unless another is
	// chosen: nvim, idea, code, zed or custom. Empty, or ask, means every
	// open asks which.
	FavouriteEditor string `yaml:"favourite_editor,omitempty"`
	// Editor and EditorArgs are the custom editor, a command of the user's
	// own; EditorWindow says it opens a window of its own rather than taking
	// over the terminal. Before there was a choice, they were the editor.
	Editor       string   `yaml:"editor,omitempty"`
	EditorArgs   []string `yaml:"editor_args,omitempty"`
	EditorWindow bool     `yaml:"editor_window,omitempty"`
	// RememberPassphrase keeps the vault passphrase in the macOS keychain,
	// readable by the unagit binary alone, so it opens without asking. The
	// user's choice, off unless they make it.
	RememberPassphrase bool `yaml:"remember_passphrase,omitempty"`
	// ToastSeconds is how long a toast stays in front of the user; 0 is
	// DefaultToastSeconds.
	ToastSeconds int        `yaml:"toast_seconds,omitempty"`
	Filters      Filters    `yaml:"filters,omitempty"`
	Instances    []Instance `yaml:"instances"`
	// Tags are the user's own labels for repositories - the default ones
	// until a configuration says otherwise, an empty list included. A server
	// passes its tags down to everything on it and a group to its subgroups
	// and repositories - GroupTags holds both, a server's under the empty
	// path - and RepositoryTags are what a repository adds or takes away.
	Tags []Tag `yaml:"tags"`
	// DefaultTagsSeen is how many of the default tags this configuration has
	// been offered, so the ones added to the defaults later reach it once,
	// and one the user removed stays removed.
	DefaultTagsSeen int      `yaml:"default_tags_seen,omitempty"`
	GroupTags       []TagSet `yaml:"group_tags,omitempty"`
	RepositoryTags  []TagSet `yaml:"repository_tags,omitempty"`
	// TagEnds is how a tag's pill ends: TagEndsRounded, TagEndsCircles or
	// TagEndsSquare.
	TagEnds string `yaml:"tag_ends,omitempty"`
	// Theme names the theme unagit draws with: one it comes with, or one of
	// the user's in <config>/themes. Empty is the default one.
	Theme string `yaml:"theme,omitempty"`
	// NerdFont says whether the terminal's font draws Nerd Font icons:
	// NerdFontOn, NerdFontOff, or empty to tell from the terminal.
	NerdFont string `yaml:"nerd_font,omitempty"`
	// TerminalBackground leaves the terminal's own background under unagit
	// whatever the theme paints, so a translucent or blurred terminal shows
	// through; every other colour still comes from the theme.
	TerminalBackground bool `yaml:"terminal_background,omitempty"`

	// Written by unagit before it grew multiple instances; read once and
	// folded into Instances.
	LegacyURL    string  `yaml:"gitlab_url,omitempty"`
	LegacyGroups []Group `yaml:"groups,omitempty"`

	// dir is where this configuration lives, with its vault and indexes.
	// Held by the value rather than read from the environment each time, so
	// that two configurations can live side by side - the tests run in
	// parallel, each with a directory of its own.
	dir string
}

// Default returns a configuration with sane defaults filled in.
func Default() *Config {
	return &Config{
		RootDir: "~/unagit",
		Tags:    DefaultTags(),

		DefaultTagsSeen: len(DefaultTags()),
	}
}

// Dir is the configuration directory, honouring XDG_CONFIG_HOME.
func Dir() string {
	if d := os.Getenv("UNAGIT_CONFIG_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "unagit")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "unagit")
}

// Path is the location of config.yaml.
func Path() string { return filepath.Join(Dir(), "config.yaml") }

// SetDir moves the configuration to another directory; everything it reads
// and writes from then on is there.
func (c *Config) SetDir(dir string) { c.dir = dir }

// Dir is the directory this configuration lives in: the one it was loaded
// from or given, and the usual one otherwise.
func (c *Config) Dir() string {
	if c.dir != "" {
		return c.dir
	}
	return Dir()
}

// Path is the location of this configuration's config.yaml.
func (c *Config) Path() string { return filepath.Join(c.Dir(), "config.yaml") }

// VaultPath is the location of the encrypted tokens beside this configuration.
func (c *Config) VaultPath() string { return filepath.Join(c.Dir(), "tokens.enc") }

// LegacyTokenPath is where a single token lived before the vault.
func (c *Config) LegacyTokenPath() string { return filepath.Join(c.Dir(), "token.enc") }

// ThemesDir is where the user's own themes are, beside this configuration.
func (c *Config) ThemesDir() string { return filepath.Join(c.Dir(), "themes") }

// IndexPath is the location of a cached index file beside this configuration.
func (c *Config) IndexPath(name string) string {
	return filepath.Join(c.Dir(), "index-"+name+".json")
}

// WatchDir is where what is watched and what was last seen of it are kept:
// this machine's state, not configuration (internal/watch).
func (c *Config) WatchDir() string { return filepath.Join(c.Dir(), "watch") }

// VaultPath is the location of the encrypted tokens.
func VaultPath() string { return filepath.Join(Dir(), "tokens.enc") }

// LegacyTokenPath is where a single token lived before the vault.
func LegacyTokenPath() string { return filepath.Join(Dir(), "token.enc") }

// IndexPath is the location of a cached index file (projects, mrs, groups).
func IndexPath(name string) string { return filepath.Join(Dir(), "index-"+name+".json") }

// Load reads config.yaml, applies defaults and folds any legacy layout into
// the current one. A missing file yields the defaults rather than an error:
// everything can be set up from the Settings tab.
func Load() (*Config, error) { return LoadFrom(Dir()) }

// LoadFrom is Load from a directory of the caller's choosing.
func LoadFrom(dir string) (*Config, error) {
	cfg := Default()
	cfg.dir = dir
	b, err := os.ReadFile(cfg.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	// What a file does not say it has seen, it has not.
	cfg.DefaultTagsSeen = 0
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", cfg.Path(), err)
	}
	cfg.normalise()
	return cfg, nil
}

// knownEditors are the editors a plain command name is recognised as.
var knownEditors = map[string]bool{"nvim": true, "idea": true, "code": true, "zed": true}

// migrateEditor turns the single editor of an older configuration into the
// favourite: one unagit knows by name becomes that one, anything else the
// custom editor, which runs exactly as before. A configuration with neither
// has no favourite, and every open asks.
func (c *Config) migrateEditor() {
	if c.FavouriteEditor != "" || c.Editor == "" {
		return
	}
	name := filepath.Base(c.Editor)
	plainArgs := len(c.EditorArgs) == 0 || (len(c.EditorArgs) == 1 && c.EditorArgs[0] == ".")
	switch {
	case knownEditors[name] && plainArgs:
		c.FavouriteEditor = name
		c.Editor, c.EditorArgs = "", nil
	default:
		c.FavouriteEditor = "custom"
	}
}

// LegacyInstanceID is the id given to the instance migrated from a
// single-server configuration, so its cached indexes keep working.
const LegacyInstanceID = "default"

func (c *Config) normalise() {
	c.tildePaths()
	c.migrateEditor()
	c.offerNewDefaultTags()
	if c.RootDir == "" {
		c.RootDir = Default().RootDir
	}
	// One server, no instances: that is the old layout.
	if len(c.Instances) == 0 && c.LegacyURL != "" {
		c.Instances = []Instance{{
			ID:     LegacyInstanceID,
			Kind:   KindGitLab,
			Name:   Host(c.LegacyURL),
			URL:    c.LegacyURL,
			Groups: c.LegacyGroups,
		}}
	}
	c.LegacyURL, c.LegacyGroups = "", nil

	for i := range c.Instances {
		inst := &c.Instances[i]
		if inst.Kind == "" {
			inst.Kind = KindGitLab
		}
		if inst.Kind == KindGitHub {
			inst.URL = GitHubURL
		}
		if inst.CloneProtocol != ProtocolSSH {
			inst.CloneProtocol = ProtocolHTTPS
		}
		inst.URL = strings.TrimRight(inst.URL, "/")
		if inst.ID == "" {
			inst.ID = c.freeID(Slug(Host(inst.URL)), inst.ID)
		}
		for g := range inst.Groups {
			if inst.Groups[g].Scope == "" {
				inst.Groups[g].Scope = ScopeSubgroups
			}
		}
	}
}

// Save writes config.yaml, creating the config directory when needed.
func (c *Config) Save() error {
	if err := os.MkdirAll(c.Dir(), 0o700); err != nil {
		return err
	}
	c.tildePaths()
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(c.Path(), b, 0o600)
}

// Instance returns the instance with this id, or nil.
func (c *Config) Instance(id string) *Instance {
	for i := range c.Instances {
		if c.Instances[i].ID == id {
			return &c.Instances[i]
		}
	}
	return nil
}

// AddInstance appends an instance, giving it a unique id derived from its URL.
func (c *Config) AddInstance(inst Instance) *Instance {
	if inst.Kind == "" {
		inst.Kind = KindGitLab
	}
	if inst.Kind == KindGitHub {
		inst.URL = GitHubURL
	}
	inst.URL = strings.TrimRight(inst.URL, "/")
	inst.ID = c.freeID(Slug(Host(inst.URL)), "")
	c.Instances = append(c.Instances, inst)
	return &c.Instances[len(c.Instances)-1]
}

// RemoveInstance drops an instance and everything selected on it.
func (c *Config) RemoveInstance(id string) {
	for i := range c.Instances {
		if c.Instances[i].ID == id {
			c.Instances = append(c.Instances[:i], c.Instances[i+1:]...)
			return
		}
	}
}

// freeID returns a unique instance id based on want, ignoring the instance
// called self.
func (c *Config) freeID(want, self string) string {
	if want == "" {
		want = "gitlab"
	}
	taken := func(id string) bool {
		for _, inst := range c.Instances {
			if inst.ID == id && inst.ID != self {
				return true
			}
		}
		return false
	}
	if !taken(want) {
		return want
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", want, n)
		if !taken(candidate) {
			return candidate
		}
	}
}

// Root is the default clone root, with ~ expanded.
func (c *Config) Root() string { return Expand(c.RootDir) }

// RootFor works out where a project is cloned: the most specific selected
// group that owns it wins, then the instance, then the global default.
func (c *Config) RootFor(inst *Instance, projectPath string) string {
	root := c.Root()
	if inst == nil {
		return root
	}
	root = resolveRoot(root, inst.RootDir)
	best := ""
	override := ""
	for _, g := range inst.Groups {
		if g.RootDir == "" || !ownsOrIs(g, projectPath) {
			continue
		}
		if len(g.FullPath) > len(best) {
			best, override = g.FullPath, g.RootDir
		}
	}
	return resolveRoot(root, override)
}

// GroupRoot is the root a group's own projects land in, for display in the
// settings view.
func (c *Config) GroupRoot(inst *Instance, g Group) string {
	return c.RootFor(inst, g.FullPath+"/project")
}

// ownsOrIs is Owns, but a group with its own root also covers the projects of
// its subgroups when no more specific override exists.
func ownsOrIs(g Group, projectPath string) bool {
	return strings.HasPrefix(projectPath, g.FullPath+"/")
}

// resolveRoot applies an override to a base directory. An absolute or ~ path
// replaces the base, a relative one is taken from it.
func resolveRoot(base, override string) string {
	if strings.TrimSpace(override) == "" {
		return base
	}
	p := Expand(strings.TrimSpace(override))
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(base, p)
}

// Expand replaces a leading ~ with the user's home directory.
// tildePaths keeps every directory under the home directory as ~/…, however
// it was typed or whatever wrote it, so that config.yaml means the same on a
// machine whose home has another name - and can be kept in dotfiles.
func (c *Config) tildePaths() {
	c.RootDir = Tilde(c.RootDir)
	for i := range c.Instances {
		inst := &c.Instances[i]
		inst.RootDir = Tilde(inst.RootDir)
		for path, dir := range inst.ProjectDirs {
			inst.ProjectDirs[path] = Tilde(dir)
		}
		for j := range inst.Groups {
			inst.Groups[j].RootDir = Tilde(inst.Groups[j].RootDir)
		}
	}
}

// Tilde is Expand the other way round: a path inside the home directory
// written from ~. Anything else - relative, or elsewhere - is left as it is.
func Tilde(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !filepath.IsAbs(p) {
		return p
	}
	home = filepath.Clean(home)
	switch clean := filepath.Clean(p); {
	case clean == home:
		return "~"
	case strings.HasPrefix(clean, home+string(filepath.Separator)):
		return "~" + strings.TrimPrefix(clean, home)
	}
	return p
}

func Expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// Host is the host part of a URL, for naming things after it.
func Host(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	}
	return u.Hostname()
}

var notSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a host name into something usable as a file name component.
func Slug(s string) string {
	return strings.Trim(notSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// InstancesOfKind lists the configured servers of one kind, in order.
func (c *Config) InstancesOfKind(kind string) []Instance {
	var out []Instance
	for _, inst := range c.Instances {
		if inst.Kind == kind {
			out = append(out, inst)
		}
	}
	return out
}

// ProjectDir resolves an exact repository override before the inherited roots.
func (c *Config) ProjectDir(inst *Instance, projectPath string) string {
	if inst != nil {
		if dir := inst.ProjectDirs[projectPath]; dir != "" {
			return Expand(dir)
		}
	}
	return filepath.Join(c.RootFor(inst, projectPath), filepath.FromSlash(projectPath))
}
