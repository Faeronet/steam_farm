package dota2

import "log"

type PendingReward struct {
	AccountID  int64          `json:"account_id"`
	RewardType string         `json:"reward_type"`
	Level      int            `json:"level"`
	Options    []RewardOption `json:"options"`
}

func (c *GCClient) CheckPendingRewards() []PendingReward {
	log.Printf("[Dota2 GC] Checking for pending rewards")
	// TODO: parse SOCache for pending rewards
	return nil
}

func (c *GCClient) SelectReward(rewardID string, optionID string) error {
	log.Printf("[Dota2 GC] Selecting reward %s option %s", rewardID, optionID)
	// TODO: send selection via GC
	return nil
}
