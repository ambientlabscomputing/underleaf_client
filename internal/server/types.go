package server

const (
	ServerOSDarwin  = "darwin"
	ServerOSLinux   = "linux"
	ServerOSWindows = "windows"
	ServerOSUnknown = "unknown"

	ServerArchAMD64   = "amd64"
	ServerArchARM64   = "arm64"
	ServerArchUnknown = "unknown"
)

type ServerCommandSettings struct {
	AllowLiteralCommands bool `json:"allow_literal_commands"`
}

type ServerPlatform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// Server represents a server
type Server struct {
	ID       string                `json:"id"`
	Name     string                `json:"name"`
	Commands ServerCommandSettings `json:"commands"`
	Platform ServerPlatform        `json:"platform"`
}
