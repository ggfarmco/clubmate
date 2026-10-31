package automation

import (
	"context"
	"slices"

	"clubmate/pkg/pluginsdk"
)

func SelectedKey(workspace string) string {
	return "selected/" + workspace
}

func JSONKey(workspace string) string {
	return "json/" + workspace
}

func JSONExpanded(ctx context.Context, h *pluginsdk.Host, workspace, id string) (bool, error) {
	var expanded string
	if _, err := h.StorageGet(ctx, JSONKey(workspace), &expanded); err != nil {
		return false, err
	}
	return id != "" && expanded == id, nil
}

func ToggleJSON(ctx context.Context, h *pluginsdk.Host, workspace, id string) error {
	expanded, err := JSONExpanded(ctx, h, workspace, id)
	if err != nil {
		return err
	}
	if expanded {
		return h.StorageDelete(ctx, JSONKey(workspace))
	}
	return h.StorageSet(ctx, JSONKey(workspace), id)
}

func Selection[P Entry[P]](ctx context.Context, h *pluginsdk.Host, workspace string, items []P) (P, string, error) {
	var current P
	var selected string
	if _, err := h.StorageGet(ctx, SelectedKey(workspace), &selected); err != nil {
		return current, "", err
	}
	if resolved := resolve(items, selected); resolved != selected {
		if err := persist(ctx, h, workspace, resolved); err != nil {
			return current, "", err
		}
		selected = resolved
	}
	if i := slices.IndexFunc(items, func(p P) bool { return p.Action().ID == selected }); i >= 0 {
		current = items[i]
	}
	return current, selected, nil
}

func resolve[P Entry[P]](items []P, selected string) string {
	if slices.ContainsFunc(items, func(p P) bool { return p.Action().ID == selected }) {
		return selected
	}
	if len(items) > 0 {
		return items[0].Action().ID
	}
	return ""
}

func persist(ctx context.Context, h *pluginsdk.Host, workspace, id string) error {
	if id == "" {
		return h.StorageDelete(ctx, SelectedKey(workspace))
	}
	return h.StorageSet(ctx, SelectedKey(workspace), id)
}
