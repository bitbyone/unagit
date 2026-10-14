package ui

import "github.com/tobola/unagit/internal/config"

func (a *App) serverFilterItem() toggleItem {
	label := "server: all…"
	if server := a.cfg.Filters.Server; server != "" {
		label = "server: " + a.instanceLabel(server) + "…"
	}
	return toggleItem{Label: tagMark(a.cfg.Filters.Server != "") + " " + esc(label), Search: label, Edit: a.showServerFilter}
}

func (a *App) showServerFilter(changed func()) {
	label := "all"
	if a.cfg.Filters.Server == "" {
		label += " " + glyphCheck
	}
	items := []pickItem{{Label: label, Sub: "Show repositories, merge requests and worktrees from every server.", Data: ""}}
	for _, inst := range a.cfg.Instances {
		label := inst.Label()
		if inst.ID == a.cfg.Filters.Server {
			label += " " + glyphCheck
		}
		items = append(items, pickItem{Label: label, Sub: config.Host(inst.URL), Data: inst.ID})
	}
	a.showPickerWith("Filter by server", items, pickerOptions{pack: true, explain: true, enterName: "Use Server", enterAbout: "Narrow the main lists to this server, or show every server again."}, func(it pickItem) {
		a.cfg.Filters.Server = it.Data.(string)
		a.applyFilters()
		changed()
	})
}
