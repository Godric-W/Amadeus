package buildinfo

const (
	defaultVersion   = "dev"
	defaultCommit    = "unknown"
	defaultBuildTime = "unknown"
)

var (
	version   = defaultVersion
	commit    = defaultCommit
	buildTime = defaultBuildTime
)

type Info struct {
	Version   string
	Commit    string
	BuildTime string
}

func Current() Info {
	return Info{
		Version:   valueOrDefault(version, defaultVersion),
		Commit:    valueOrDefault(commit, defaultCommit),
		BuildTime: valueOrDefault(buildTime, defaultBuildTime),
	}
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}
