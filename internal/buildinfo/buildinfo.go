package buildinfo

const shortCommitLen = 7

var (
	Version = "0.0.0-dev"
	Commit  = ""
	Flavor  = "dev"

	RelayURL = "https://clubmate-push.exe.xyz"
)

func String() string {
	if Commit == "" {
		return Version
	}
	return Version + " (" + Commit[:min(len(Commit), shortCommitLen)] + ")"
}
