package cs2

import "log"

type ArmoryPassInfo struct {
	Stars       int  `json:"stars"`
	HasPass     bool `json:"has_pass"`
	PassExpiry  int64 `json:"pass_expiry"`
}

func (c *GCClient) GetArmoryPassInfo() *ArmoryPassInfo {
	if c.profile == nil {
		return &ArmoryPassInfo{}
	}
	return &ArmoryPassInfo{
		Stars: c.profile.ArmoryStars,
	}
}

func (c *GCClient) RedeemArmoryStars(itemID string, count int) error {
	log.Printf("[CS2 GC] Redeeming %d armory stars for item %s", count, itemID)
	// TODO: implement via GC protocol
	return nil
}
