package main

import "unicode"

type elicitLabels struct {
	Other         string `json:"other"`
	Answer        string `json:"answer"`
	Send          string `json:"send"`
	Skip          string `json:"skip"`
	SkipHint      string `json:"skipHint"`
	SkipHintShort string `json:"skipHintShort"`
	Next          string `json:"next"`
	Back          string `json:"back"`
	Skipped       string `json:"skipped"`
	Cancelled     string `json:"cancelled"`
	Answered      string `json:"answered"`
}

var (
	labelsEnglish = elicitLabels{
		Other:         "Other…",
		Answer:        "Your answer",
		Send:          "Send",
		Skip:          "Skip",
		SkipHint:      "Skip answers “none of these” and ends the turn",
		SkipHintShort: "Skip ends the turn",
		Next:          "Next",
		Back:          "Back",
		Skipped:       "Skipped",
		Cancelled:     "Cancelled",
		Answered:      "Answered",
	}
	labelsRussian = elicitLabels{
		Other:         "Другое…",
		Answer:        "Ваш ответ",
		Send:          "Отправить",
		Skip:          "Пропустить",
		SkipHint:      "«Пропустить» отвечает «ничего из этого» и завершает ход",
		SkipHintShort: "«Пропустить» завершает ход",
		Next:          "Далее",
		Back:          "Назад",
		Skipped:       "Пропущено",
		Cancelled:     "Отменено",
		Answered:      "Отвечено",
	}
)

func (el elicitation) labels() elicitLabels {
	if el.Labels != nil {
		return *el.Labels
	}
	return labelsFor(el)
}

func labelsFor(el elicitation) elicitLabels {
	texts := []string{el.Message}
	for _, f := range el.Fields {
		if isCustom(el.Fields, f) {
			continue
		}
		texts = append(texts, f.Title, f.Description)
		for _, o := range f.Options {
			texts = append(texts, o.Label, o.Description)
		}
	}
	if mostlyCyrillic(texts...) {
		return labelsRussian
	}
	return labelsEnglish
}

func mostlyCyrillic(texts ...string) bool {
	var cyrillic, latin int
	for _, s := range texts {
		for _, r := range s {
			switch {
			case unicode.Is(unicode.Cyrillic, r):
				cyrillic++
			case unicode.Is(unicode.Latin, r):
				latin++
			}
		}
	}
	return cyrillic > latin
}
