package bus

import "context"

type Event struct{}

type Bus struct{}

func (b *Bus) Read(ctx context.Context, topic string, after int64, limit int) ([]Event, error) {
	return nil, nil
}
