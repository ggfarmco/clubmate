package rpc

var legacyMethods = map[string]string{
	"project.create":  "workspace.create",
	"project.list":    "workspace.list",
	"project.get":     "workspace.get",
	"project.addRepo": "workspace.folder.add",
	"project.delete":  "workspace.delete",
	"project.rename":  "workspace.rename",
	"project.open":    "workspace.show",
}

func addLegacyMethods(methods map[string]handlerFunc) {
	for legacy, current := range legacyMethods {
		methods[legacy] = methods[current]
	}
}
