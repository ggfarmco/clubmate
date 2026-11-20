package tunnelrelay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

const MaxTickets = 33

var (
	errBadTicket   = errors.New("ticket hash must be 64 lowercase hex characters")
	errTooMany     = fmt.Errorf("at most %d tickets", MaxTickets)
	errTicketTaken = errors.New("a ticket hash belongs to another Mac")
	errNotCurrent  = errors.New(NotCurrentStream)
)

type Ticket struct {
	Hash string `json:"h"`
	Exp  int64  `json:"exp,omitempty"`
}

type tickets struct {
	path string
	now  func() time.Time

	mu     sync.Mutex
	macs   map[string][]Ticket
	byHash map[string]ticketOwner
}

type ticketOwner struct {
	mac string
	exp int64
}

func loadTickets(path string, now func() time.Time) (*tickets, error) {
	t := &tickets{path: path, now: now, macs: map[string][]Ticket{}, byHash: map[string]ticketOwner{}}
	if path == "" {
		return t, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return t, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &t.macs); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if t.macs == nil {
		t.macs = map[string][]Ticket{}
	}
	t.index()
	return t, nil
}

func hashTicket(ticket string) string {
	sum := sha256.Sum256([]byte(ticket))
	return hex.EncodeToString(sum[:])
}

func (t *tickets) index() {
	t.byHash = map[string]ticketOwner{}
	for mac, list := range t.macs {
		for _, tk := range list {
			t.byHash[tk.Hash] = ticketOwner{mac: mac, exp: tk.Exp}
		}
	}
}

func (t *tickets) lookup(ticket string) (mac, hash string, ok bool) {
	if ticket == "" {
		return "", "", false
	}
	hash = hashTicket(ticket)
	t.mu.Lock()
	o, ok := t.byHash[hash]
	t.mu.Unlock()
	if !ok || (o.exp != 0 && t.now().Unix() >= o.exp) {
		return "", "", false
	}
	return o.mac, hash, true
}

func (t *tickets) has(mac, hash string) bool {
	t.mu.Lock()
	o, ok := t.byHash[hash]
	t.mu.Unlock()
	return ok && o.mac == mac && (o.exp == 0 || t.now().Unix() < o.exp)
}

func (t *tickets) set(mac string, list []Ticket, current func() bool) (int, error) {
	if len(list) > MaxTickets {
		return 0, errTooMany
	}
	now := t.now().Unix()
	seen := map[string]bool{}
	clean := make([]Ticket, 0, len(list))
	for _, tk := range list {
		if b, err := hex.DecodeString(tk.Hash); err != nil || len(b) != sha256.Size || hex.EncodeToString(b) != tk.Hash {
			return 0, errBadTicket
		}
		if tk.Exp != 0 && tk.Exp <= now {
			continue
		}
		if !seen[tk.Hash] {
			seen[tk.Hash] = true
			clean = append(clean, tk)
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !current() {
		return 0, errNotCurrent
	}
	next := make(map[string][]Ticket, len(t.macs)+1)
	for m, l := range t.macs {
		next[m] = l
	}
	for _, tk := range clean {
		o, ok := t.byHash[tk.Hash]
		if !ok || o.mac == mac {
			continue
		}
		if o.exp == 0 || now < o.exp {
			return 0, errTicketTaken
		}
		next[o.mac] = slices.DeleteFunc(slices.Clone(next[o.mac]), func(x Ticket) bool { return x.Hash == tk.Hash })
		if len(next[o.mac]) == 0 {
			delete(next, o.mac)
		}
	}
	if len(clean) == 0 {
		delete(next, mac)
	} else {
		next[mac] = clean
	}
	if err := save(t.path, next); err != nil {
		return 0, err
	}
	t.macs = next
	t.index()
	return len(clean), nil
}

func save(path string, macs map[string][]Ticket) error {
	if path == "" {
		return nil
	}
	raw, err := json.Marshal(macs)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
