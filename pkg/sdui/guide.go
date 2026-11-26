package sdui

import "encoding/json"

const (
	StepTestMe       = "test_me"
	StepShowMe       = "show_me"
	StepGoDeeper     = "go_deeper"
	StepSmallerSteps = "smaller_steps"
	StepContinue     = "continue"

	TourKnown   = "known"
	TourShaky   = "shaky"
	TourMissed  = "missed"
	TourCurrent = "current"
	TourNotYet  = "not_yet"

	TourAskAgain = "ask_again"
	TourGoBack   = "go_back"
)

type GuideStepProps struct {
	Step  string `json:"step"`
	Label string `json:"label"`
	Node  string `json:"node,omitempty"`
}

type StepControlsProps struct {
	Session string       `json:"session"`
	Step    string       `json:"step"`
	Actions []StepAction `json:"actions"`
	Pressed string       `json:"pressed,omitempty"`
	Aside   *StepAside   `json:"aside,omitempty"`
}

type StepAction struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Enabled  bool   `json:"enabled"`
	Primary  bool   `json:"primary,omitempty"`
	Shortcut string `json:"shortcut,omitempty"`
}

type StepAside struct {
	Step    string `json:"step"`
	Title   string `json:"title"`
	Open    bool   `json:"open"`
	Session string `json:"session,omitempty"`
}

type TourMapProps struct {
	Session   string        `json:"session"`
	Title     string        `json:"title"`
	Step      int           `json:"step"`
	Of        int           `json:"of"`
	StepLabel string        `json:"stepLabel"`
	Summary   string        `json:"summary"`
	Current   string        `json:"current,omitempty"`
	Counts    TourCounts    `json:"counts"`
	Concepts  []TourConcept `json:"concepts"`
	Edges     []TourEdge    `json:"edges,omitempty"`
	Legend    []TourLegend  `json:"legend"`
	Shown     bool          `json:"shown"`
	Shortcut  string        `json:"shortcut,omitempty"`
}

type TourCounts struct {
	Known  int `json:"known"`
	Shaky  int `json:"shaky"`
	Missed int `json:"missed"`
	NotYet int `json:"notYet"`
}

type TourConcept struct {
	ID         string      `json:"id"`
	Label      string      `json:"label"`
	State      string      `json:"state"`
	StateLabel string      `json:"stateLabel"`
	Step       int         `json:"step"`
	StepLabel  string      `json:"stepLabel"`
	Depth      int         `json:"depth"`
	Note       string      `json:"note,omitempty"`
	Links      []TourLink  `json:"links,omitempty"`
	Offers     []TourOffer `json:"offers,omitempty"`
}

type TourLink struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type TourOffer struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type TourEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Branch bool   `json:"branch,omitempty"`
}

type TourLegend struct {
	State string `json:"state"`
	Label string `json:"label"`
}

func GuideStep(id string, p GuideStepProps) Node {
	return Node{Type: "guide.step", ID: id, Props: propsOf(p)}
}

func StepControls(id string, p StepControlsProps) Node {
	return Node{Type: "guide.steps", ID: id, Props: propsOf(p)}
}

func TourMap(id string, p TourMapProps) Node {
	return Node{Type: "guide.tour", ID: id, Props: propsOf(p)}
}

func TourStrip(id string, p TourMapProps) Node {
	return Node{Type: "guide.strip", ID: id, Props: propsOf(p)}
}

func propsOf(p any) map[string]any {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	var props map[string]any
	if err := json.Unmarshal(raw, &props); err != nil {
		return nil
	}
	return props
}
