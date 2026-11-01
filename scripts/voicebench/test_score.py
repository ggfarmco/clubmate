from score import Term, normalize, term_accuracy, wer


def test_normalize_folds_case_punct_and_yo():
    assert normalize("Ещё MCP-сервер, feat/Voice!") == ["еще", "mcp", "сервер", "feat", "voice"]


def test_wer_counts_edits_over_reference_words():
    assert wer("запусти агента на ветке", "запусти агент на ветке") == 0.25


def test_term_accuracy_matches_multiword_terms():
    terms = [Term("feat/voice", []), Term("MCP", [])]
    assert term_accuracy(terms, "ветка feat voice и mcp") == 1.0
    assert term_accuracy(terms, "ветка фит войс и mcp") == 0.5


def test_term_accuracy_with_aliases():
    terms = [Term("MCP", ["эмсипи", "эм си пи"])]
    assert term_accuracy(terms, "поправь эм си пи сервер") == 0.0
    assert term_accuracy(terms, "поправь эм си пи сервер", aliases=True) == 1.0


def test_score_empty_hypothesis():
    assert wer("да мержи", "") == 1.0
    assert term_accuracy([Term("main", [])], "") == 0.0
