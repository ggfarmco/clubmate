package automation

import (
	"context"
	"fmt"

	"clubmate/pkg/pluginsdk"
)

type Storage[P any] interface {
	Save(ctx context.Context, p P) error
	Remove(ctx context.Context, p P) error
}

type Store[E any, P interface {
	*E
	Entry[P]
}] struct {
	Host   *pluginsdk.Host
	Prefix string
	Noun   string
	Log    func(error)
}

func (st *Store[E, P]) Load(ctx context.Context) ([]P, error) {
	keys, err := st.Host.StorageList(ctx, st.Prefix)
	if err != nil {
		return nil, fmt.Errorf("list %ss: %w", st.Noun, err)
	}
	items := make([]P, 0, len(keys))
	for _, key := range keys {
		var item E
		found, err := st.Host.StorageGet(ctx, key, &item)
		if err != nil {
			st.Log(fmt.Errorf("skip stored %s %s: %w", st.Noun, key, err))
			continue
		}
		if found {
			items = append(items, &item)
		}
	}
	return items, nil
}

func (st *Store[E, P]) Save(ctx context.Context, p P) error {
	if err := st.Host.StorageSet(ctx, st.key(p), p); err != nil {
		return fmt.Errorf("save %s %s: %w", st.Noun, p.Action().ID, err)
	}
	return nil
}

func (st *Store[E, P]) Remove(ctx context.Context, p P) error {
	if err := st.Host.StorageDelete(ctx, st.key(p)); err != nil {
		return fmt.Errorf("delete %s %s: %w", st.Noun, p.Action().ID, err)
	}
	return nil
}

func (st *Store[E, P]) key(p P) string {
	act := p.Action()
	return st.Prefix + act.Workspace + "/" + act.ID
}
