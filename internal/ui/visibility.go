package ui

import (
	"sort"
	"strings"

	"github.com/tobola/unagit/internal/forge"
)

func repositoryVisibility(pr forge.Project) string {
	if visibility := strings.ToLower(strings.TrimSpace(pr.Visibility)); visibility != "" {
		return visibility
	}
	return "unknown"
}

func (a *App) showRepositoryVisibility(changed func()) {
	values := []string{"", forge.VisibilityPublic, forge.VisibilityPrivate, forge.VisibilityInternal}
	seen := map[string]bool{"": true, forge.VisibilityPublic: true, forge.VisibilityPrivate: true, forge.VisibilityInternal: true}
	var extra []string
	add := func(value string) {
		if !seen[value] {
			seen[value] = true
			extra = append(extra, value)
		}
	}
	for _, pr := range a.projects {
		add(repositoryVisibility(pr))
	}
	add(a.cfg.Filters.RepositoryVisibility)
	sort.Strings(extra)
	values = append(values, extra...)
	var items []pickItem
	for _, value := range values {
		label, about := value, "The visibility reported by the server."
		switch value {
		case "":
			label, about = "all", "Show every repository, whatever its visibility."
		case forge.VisibilityPublic:
			about = "Visible to everyone, including people who are not signed in."
		case forge.VisibilityPrivate:
			about = "Visible to members, including people with access through a group."
		case forge.VisibilityInternal:
			about = "Visible within the server or enterprise, rather than publicly."
		case "unknown":
			about = "Visibility is not known yet; refresh to read it from the server."
		}
		if value == a.cfg.Filters.RepositoryVisibility {
			label += " " + glyphCheck
		}
		items = append(items, pickItem{Label: label, Sub: about, Data: value})
	}
	a.showPickerWith("Repository visibility", items, pickerOptions{pack: true, explain: true, enterName: "Use Visibility", enterAbout: "Show repositories with this visibility, or every visibility again."}, func(it pickItem) {
		a.cfg.Filters.RepositoryVisibility = it.Data.(string)
		a.applyFilters()
		changed()
	})
}
