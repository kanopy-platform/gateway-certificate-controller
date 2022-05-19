package version

var (
	// these are set using ldflags
	version   = "0.0.0"
	gitCommit = ""
)

type Info struct {
	Version   string `json:"version,omitempty"`
	GitCommit string `json:"gitCommit,omitempty"`
}

func Get() Info {
	return Info{
		Version:   version,
		GitCommit: gitCommit,
	}
}
