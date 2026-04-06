package models

import (
	"encoding/json"
	"time"
)

type AccountStatus string

const (
	StatusIdle      AccountStatus = "idle"
	StatusReady     AccountStatus = "ready"
	StatusFarming   AccountStatus = "farming"
	StatusQueued    AccountStatus = "queued"
	StatusFarmed    AccountStatus = "farmed"
	StatusCollected AccountStatus = "collected"
	StatusDone      AccountStatus = "done"
	StatusError     AccountStatus = "error"
	StatusBanned    AccountStatus = "banned"
)

type GameType string

const (
	GameCS2   GameType = "cs2"
	GameDota2 GameType = "dota2"
)

type FarmMode string

const (
	FarmModeProtocol FarmMode = "protocol"
	FarmModeSandbox  FarmMode = "sandbox"
)

type Account struct {
	ID              int64          `json:"id" db:"id"`
	Username        string         `json:"username" db:"username"`
	PasswordEnc     []byte         `json:"-" db:"password_enc"`
	SharedSecret    *string        `json:"shared_secret,omitempty" db:"shared_secret"`
	IdentitySecret  *string        `json:"identity_secret,omitempty" db:"identity_secret"`
	SteamID         *int64         `json:"steam_id,omitempty" db:"steam_id"`
	AvatarURL       *string        `json:"avatar_url,omitempty" db:"avatar_url"`
	PersonaName     *string        `json:"persona_name,omitempty" db:"persona_name"`
	Proxy           *string        `json:"proxy,omitempty" db:"proxy"`
	GameType        GameType       `json:"game_type" db:"game_type"`
	FarmMode        FarmMode       `json:"farm_mode" db:"farm_mode"`
	Status          AccountStatus  `json:"status" db:"status"`
	StatusDetail    *string        `json:"status_detail,omitempty" db:"status_detail"`
	IsPrime         bool           `json:"is_prime" db:"is_prime"`
	CS2Level        int            `json:"cs2_level" db:"cs2_level"`
	CS2XP           int            `json:"cs2_xp" db:"cs2_xp"`
	CS2XPNeeded     int            `json:"cs2_xp_needed" db:"cs2_xp_needed"`
	CS2Rank         *string        `json:"cs2_rank,omitempty" db:"cs2_rank"`
	ArmoryStars     int            `json:"armory_stars" db:"armory_stars"`
	DotaHours       float32        `json:"dota_hours" db:"dota_hours"`
	LastDropAt      *time.Time     `json:"last_drop_at,omitempty" db:"last_drop_at"`
	FarmedThisWeek  bool           `json:"farmed_this_week" db:"farmed_this_week"`
	DropCollected   bool           `json:"drop_collected" db:"drop_collected"`
	Tags            []string       `json:"tags" db:"tags"`
	GroupName       *string        `json:"group_name,omitempty" db:"group_name"`
	CreatedAt       time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at" db:"updated_at"`
}

type AccountGroup struct {
	ID        int64     `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	GameType  GameType  `json:"game_type" db:"game_type"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

type FarmSession struct {
	ID          int64           `json:"id" db:"id"`
	Name        *string         `json:"name,omitempty" db:"name"`
	GameType    GameType        `json:"game_type" db:"game_type"`
	FarmMode    FarmMode        `json:"farm_mode" db:"farm_mode"`
	AccountIDs  []int64         `json:"account_ids" db:"account_ids"`
	StartedAt   time.Time       `json:"started_at" db:"started_at"`
	EndedAt     *time.Time      `json:"ended_at,omitempty" db:"ended_at"`
	TotalHours  float32         `json:"total_hours" db:"total_hours"`
	DropsCount  int             `json:"drops_count" db:"drops_count"`
	Status      string          `json:"status" db:"status"`
	Config      json.RawMessage `json:"config" db:"config"`
}

type Drop struct {
	ID            int64            `json:"id" db:"id"`
	AccountID     int64            `json:"account_id" db:"account_id"`
	SessionID     *int64           `json:"session_id,omitempty" db:"session_id"`
	GameType      GameType         `json:"game_type" db:"game_type"`
	ItemName      string           `json:"item_name" db:"item_name"`
	ItemType      *string          `json:"item_type,omitempty" db:"item_type"`
	ItemImageURL  *string          `json:"item_image_url,omitempty" db:"item_image_url"`
	AssetID       *int64           `json:"asset_id,omitempty" db:"asset_id"`
	ClassID       *int64           `json:"class_id,omitempty" db:"class_id"`
	InstanceID    *int64           `json:"instance_id,omitempty" db:"instance_id"`
	ContextID     int64            `json:"context_id" db:"context_id"`
	MarketPrice   *float32         `json:"market_price,omitempty" db:"market_price"`
	DroppedAt     time.Time        `json:"dropped_at" db:"dropped_at"`
	Claimed       bool             `json:"claimed" db:"claimed"`
	SentToTrade   bool             `json:"sent_to_trade" db:"sent_to_trade"`
	ChoiceOptions *json.RawMessage `json:"choice_options,omitempty" db:"choice_options"`
	ChosenItems   *json.RawMessage `json:"chosen_items,omitempty" db:"chosen_items"`
}

type DotaEventProgress struct {
	ID              int64            `json:"id" db:"id"`
	AccountID       int64            `json:"account_id" db:"account_id"`
	EventID         *string          `json:"event_id,omitempty" db:"event_id"`
	EventName       *string          `json:"event_name,omitempty" db:"event_name"`
	CurrentAct      int              `json:"current_act" db:"current_act"`
	CurrentNode     int              `json:"current_node" db:"current_node"`
	TokensEarned    int              `json:"tokens_earned" db:"tokens_earned"`
	TokensSpent     int              `json:"tokens_spent" db:"tokens_spent"`
	Level           int              `json:"level" db:"level"`
	RewardsPending  json.RawMessage  `json:"rewards_pending" db:"rewards_pending"`
	RewardsClaimed  json.RawMessage  `json:"rewards_claimed" db:"rewards_claimed"`
	UpdatedAt       time.Time        `json:"updated_at" db:"updated_at"`
}

type Sandbox struct {
	ID            int64     `json:"id" db:"id"`
	AccountID     int64     `json:"account_id" db:"account_id"`
	ContainerID   *string   `json:"container_id,omitempty" db:"container_id"`
	ContainerName *string   `json:"container_name,omitempty" db:"container_name"`
	GameType      GameType  `json:"game_type" db:"game_type"`
	MachineID     *string   `json:"machine_id,omitempty" db:"machine_id"`
	MACAddress    *string   `json:"mac_address,omitempty" db:"mac_address"`
	Hostname      *string   `json:"hostname,omitempty" db:"hostname"`
	Display       *string   `json:"display,omitempty" db:"display"`
	VNCPort       *int      `json:"vnc_port,omitempty" db:"vnc_port"`
	Status        string    `json:"status" db:"status"`
	CPUUsage      float32   `json:"cpu_usage" db:"cpu_usage"`
	MemoryMB      int       `json:"memory_mb" db:"memory_mb"`
	GPUDevice     *string   `json:"gpu_device,omitempty" db:"gpu_device"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

type Proxy struct {
	ID         int64      `json:"id" db:"id"`
	Address    string     `json:"address" db:"address"`
	IsAlive    bool       `json:"is_alive" db:"is_alive"`
	LastCheck  *time.Time `json:"last_check,omitempty" db:"last_check"`
	AssignedTo *int64     `json:"assigned_to,omitempty" db:"assigned_to"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
}

type WeeklyStats struct {
	ID             int64           `json:"id" db:"id"`
	WeekStart      time.Time       `json:"week_start" db:"week_start"`
	GameType       GameType        `json:"game_type" db:"game_type"`
	AccountsFarmed int             `json:"accounts_farmed" db:"accounts_farmed"`
	TotalDrops     int             `json:"total_drops" db:"total_drops"`
	TotalValue     float32         `json:"total_value" db:"total_value"`
	DropBreakdown  json.RawMessage `json:"drop_breakdown" db:"drop_breakdown"`
}
