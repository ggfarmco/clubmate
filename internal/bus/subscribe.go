package bus

import "context"

const subscriptionBuffer = 256

type Subscription struct {
	bus     *Bus
	pattern string
	out     chan Event
	cancel  context.CancelFunc

	head  int64
	live  bool
	ended bool
	err   error
}

func (b *Bus) Subscribe(pattern string, after int64) (*Subscription, error) {
	if !ValidPattern(pattern) {
		return nil, ErrInvalidPattern
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, errClosed
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Subscription{
		bus:     b,
		pattern: pattern,
		out:     make(chan Event, subscriptionBuffer),
		cancel:  cancel,
		head:    b.last,
		live:    after >= b.last,
	}
	b.subs[s] = struct{}{}
	if !s.live {
		b.wg.Add(1)
		go s.backfill(ctx, after)
	}
	return s, nil
}

func (s *Subscription) C() <-chan Event {
	return s.out
}

func (s *Subscription) Err() error {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	return s.err
}

func (s *Subscription) Close() {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	s.endLocked(nil)
}

func (b *Bus) fanoutLocked(e Event) {
	for s := range b.subs {
		if !Match(s.pattern, e.Topic) {
			continue
		}
		if !s.live {
			s.head = e.Seq
			continue
		}
		select {
		case s.out <- e:
		default:
			s.endLocked(ErrSlowSubscriber)
		}
	}
}

func (s *Subscription) backfill(ctx context.Context, after int64) {
	defer s.bus.wg.Done()
	defer s.cancel()
	for {
		upto, ok := s.nextBackfill(after)
		if !ok {
			return
		}
		events, err := s.bus.readRange(ctx, s.pattern, after, upto, subscriptionBuffer)
		if err != nil {
			s.fail(err)
			return
		}
		for _, e := range events {
			select {
			case s.out <- e:
				after = e.Seq
			case <-ctx.Done():
				close(s.out)
				return
			}
		}
		if len(events) < subscriptionBuffer {
			after = upto
		}
	}
}

func (s *Subscription) nextBackfill(after int64) (int64, bool) {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	if s.ended {
		close(s.out)
		return 0, false
	}
	if after >= s.head {
		s.live = true
		return 0, false
	}
	return s.head, true
}

func (s *Subscription) fail(err error) {
	s.bus.mu.Lock()
	s.endLocked(err)
	s.bus.mu.Unlock()
	close(s.out)
}

func (s *Subscription) endLocked(err error) {
	if s.ended {
		return
	}
	s.ended = true
	s.err = err
	s.cancel()
	delete(s.bus.subs, s)
	if s.live {
		close(s.out)
	}
}
