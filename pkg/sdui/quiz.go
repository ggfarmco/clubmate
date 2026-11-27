package sdui

import "encoding/json"

type QuizProps struct {
	Request    string          `json:"request"`
	Type       string          `json:"type"`
	State      string          `json:"state"`
	Title      string          `json:"title"`
	Question   string          `json:"question"`
	Step       string          `json:"step,omitempty"`
	Hint       string          `json:"hint,omitempty"`
	Concept    string          `json:"concept,omitempty"`
	Dock       bool            `json:"dock,omitempty"`
	Excerpt    *QuizExcerpt    `json:"excerpt,omitempty"`
	Options    []QuizOption    `json:"options,omitempty"`
	Confidence *QuizConfidence `json:"confidence,omitempty"`
	Reveal     *QuizReveal     `json:"reveal,omitempty"`
	Labels     QuizLabels      `json:"labels"`
}

type QuizExcerpt struct {
	File   string    `json:"file"`
	Commit string    `json:"commit"`
	Rows   []QuizRow `json:"rows"`
	Pick   string    `json:"pick"`
	Picked []int     `json:"picked,omitempty"`
	Cursor int       `json:"cursor,omitempty"`
	Open   *QuizOpen `json:"open,omitempty"`
	Span   string    `json:"span"`
}

type QuizRow struct {
	N     int
	Text  string
	Tone  string
	Label string
	Fold  string
}

func (r QuizRow) MarshalJSON() ([]byte, error) {
	if r.Fold != "" {
		return json.Marshal(struct {
			Fold string `json:"fold"`
		}{r.Fold})
	}
	return json.Marshal(struct {
		N     int    `json:"n"`
		Text  string `json:"text"`
		Tone  string `json:"tone,omitempty"`
		Label string `json:"label,omitempty"`
	}{r.N, r.Text, r.Tone, r.Label})
}

func (r *QuizRow) UnmarshalJSON(raw []byte) error {
	var v struct {
		N     int    `json:"n"`
		Text  string `json:"text"`
		Tone  string `json:"tone"`
		Label string `json:"label"`
		Fold  string `json:"fold"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*r = QuizRow{N: v.N, Text: v.Text, Tone: v.Tone, Label: v.Label, Fold: v.Fold}
	return nil
}

type QuizOpen struct {
	Label    string `json:"label"`
	Shortcut string `json:"shortcut,omitempty"`
}

type QuizOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Key   string `json:"key"`
	On    bool   `json:"on,omitempty"`
}

type QuizConfidence struct {
	Label       string      `json:"label"`
	Levels      []QuizLevel `json:"levels"`
	Value       string      `json:"value,omitempty"`
	Enabled     bool        `json:"enabled"`
	NeedsRating bool        `json:"needsRating"`
	Sends       bool        `json:"sends"`
	HoldMs      int         `json:"holdMs"`
	Hint        string      `json:"hint,omitempty"`
}

type QuizLevel struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Key   string `json:"key"`
}

type QuizReveal struct {
	Verdict    string `json:"verdict"`
	Label      string `json:"label"`
	Yours      string `json:"yours"`
	Confidence string `json:"confidence,omitempty"`
	Answer     string `json:"answer,omitempty"`
	Why        string `json:"why,omitempty"`
	Ref        string `json:"ref,omitempty"`
	Flag       string `json:"flag,omitempty"`
	Note       string `json:"note,omitempty"`
}

type QuizLabels struct {
	Skip        string `json:"skip"`
	Undo        string `json:"undo"`
	Sending     string `json:"sending"`
	Fold        string `json:"fold"`
	AnswerHint  string `json:"answerHint"`
	You         string `json:"you"`
	Answer      string `json:"answer"`
	AllLines    string `json:"allLines"`
	SecondsLeft string `json:"secondsLeft"`
	SayIt       string `json:"sayIt"`
	OpenOnMac   string `json:"openOnMac"`
	Line        string `json:"line"`
	Range       string `json:"range"`
}

func QuizCard(id string, p QuizProps) Node {
	return Node{Type: "request.quiz", ID: id, Props: propsOf(p)}
}
