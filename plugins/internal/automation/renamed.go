package automation

var Renamed = map[string]string{
	"graphs.new":      "agents.graph-new",
	"graphs.list":     "agents.graph-list",
	"graphs.run":      "agents.graph-run",
	"graphs.show":     "agents.graph-show",
	"graphs.stop":     "agents.graph-stop",
	"graphs.approve":  "agents.graph-approve",
	"graphs.continue": "agents.graph-continue",
	"graphs.trust":    "agents.graph-trust",
	"graphs.trusted":  "agents.graph-trusted",
	"graphs.reveal":   "agents.graph-reveal",
	"graphs.delete":   "agents.graph-delete",
}

func RenameCommand(id string) (string, bool) {
	to, ok := Renamed[id]
	return to, ok
}
