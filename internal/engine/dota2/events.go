package dota2

import "log"

type EventProgress struct {
	EventID     string           `json:"event_id"`
	EventName   string           `json:"event_name"`
	CurrentAct  int              `json:"current_act"`
	CurrentNode int              `json:"current_node"`
	Level       int              `json:"level"`
	Tokens      int              `json:"tokens"`
	Rewards     []EventReward    `json:"rewards"`
}

type EventReward struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	ImageURL    string `json:"image_url"`
	Available   bool   `json:"available"`
	Claimed     bool   `json:"claimed"`
}

type RewardChoice struct {
	RewardID string   `json:"reward_id"`
	Options  []RewardOption `json:"options"`
}

type RewardOption struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	ImageURL string `json:"image_url"`
}

func (c *GCClient) GetEventProgress() *EventProgress {
	if c.profile == nil || !c.profile.EventActive {
		return nil
	}

	return &EventProgress{
		EventName:   c.profile.EventName,
		CurrentAct:  1,
		CurrentNode: 0,
		Level:       0,
		Tokens:      0,
	}
}

func (c *GCClient) ClaimEventReward(rewardID string, choiceID string) error {
	log.Printf("[Dota2 GC] Claiming reward %s with choice %s", rewardID, choiceID)
	// TODO: send via GC protocol
	return nil
}
