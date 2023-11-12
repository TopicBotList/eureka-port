package dovetypes

type PlatformStatus string

const (
	PlatformStatusOnline       PlatformStatus = "online"
	PlatformStatusIdle         PlatformStatus = "idle"
	PlatformStatusDoNotDisturb PlatformStatus = "dnd"
	PlatformStatusOffline      PlatformStatus = "offline"
)

type PlatformUser struct {
	ID          string         `json:"id" description:"The users ID"`
	Username    string         `json:"username" description:"The users username"`
	DisplayName string         `json:"display_name" description:"The users global display name, if the user still has a discriminator, this will be username+hashtag+discriminator"`
	Avatar      string         `json:"avatar" description:"The users resolved avatar URL for the platform (not just hash)"`
	Bot         bool           `json:"bot" description:"Whether the user is a bot or not"`
	Status      PlatformStatus `json:"status" description:"The users current status"`
	Flags       []string       `json:"flags" description:"The users flags. Note that dovewing has its own list of flags"`
	ExtraData   map[string]any `json:"extra_data" description:"Platform specific extra data"`
}
