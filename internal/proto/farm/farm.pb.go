// Code generated manually from farm.proto. Regenerate with protoc when available.

package farm

type StatusReport struct {
	AccountID   int64   `json:"account_id"`
	Status      string  `json:"status"`
	Detail      string  `json:"detail"`
	GameType    string  `json:"game_type"`
	Level       int32   `json:"level"`
	XP          int32   `json:"xp"`
	HoursPlayed float64 `json:"hours_played"`
}

type DropReport struct {
	AccountID    int64          `json:"account_id"`
	GameType     string         `json:"game_type"`
	ItemName     string         `json:"item_name"`
	ItemType     string         `json:"item_type"`
	ItemImageURL string         `json:"item_image_url"`
	HasChoice    bool           `json:"has_choice"`
	Choices      []ChoiceOption `json:"choices"`
}

type ChoiceOption struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	ImageURL string `json:"image_url"`
}

type CommandMsg struct {
	Command    string            `json:"command"`
	AccountIDs []int64           `json:"account_ids"`
	Params     map[string]string `json:"params"`
}

type HeartbeatMsg struct {
	ClientID        string  `json:"client_id"`
	ActiveBots      int32   `json:"active_bots"`
	ActiveSandboxes int32   `json:"active_sandboxes"`
	CPUUsage        float64 `json:"cpu_usage"`
	MemoryUsage     float64 `json:"memory_usage"`
}

type AccountInfo struct {
	ID             int64  `json:"id"`
	Username       string `json:"username"`
	GameType       string `json:"game_type"`
	FarmMode       string `json:"farm_mode"`
	Status         string `json:"status"`
	SharedSecret   string `json:"shared_secret"`
	IdentitySecret string `json:"identity_secret"`
	Proxy          string `json:"proxy"`
}

type Ack struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}
