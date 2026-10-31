package contract

import "time"

type Workspace struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Repos        []string   `json:"repos"`
	Roots        []string   `json:"roots"`
	Missing      []string   `json:"missing,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	Home         bool       `json:"home"`
	LastOpenedAt *time.Time `json:"lastOpenedAt"`
}
