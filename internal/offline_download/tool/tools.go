package tool

import (
	"sort"

	"github.com/dongdio/OpenList/v4/utility/errs"

	"github.com/dongdio/OpenList/v4/internal/model"
)

var Tools = make(ToolsManager)

type ToolsManager map[string]Tool

func (t ToolsManager) Get(name string) (Tool, error) {
	if tool, ok := t[name]; ok {
		return tool, nil
	}
	return nil, errs.New("tool " + name + " not found")
}

func (t ToolsManager) Add(tool Tool) {
	t[tool.Name()] = tool
}

func (t ToolsManager) Names() []string {
	ns := make([]string, 0, len(t))
	for name := range t {
		if tool, err := t.Get(name); err == nil && tool.IsReady() {
			ns = append(ns, name)
		}
	}
	sort.Strings(ns)
	return ns
}

func (t ToolsManager) Items() []model.SettingItem {
	var items []model.SettingItem
	for _, tool := range t {
		items = append(items, tool.Items()...)
	}
	return items
}