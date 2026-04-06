package dota2

import "log"

type InventoryItem struct {
	AssetID    int64  `json:"asset_id"`
	ClassID    int64  `json:"class_id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	ImageURL   string `json:"image_url"`
	Tradeable  bool   `json:"tradeable"`
}

func (c *GCClient) RequestInventory() {
	log.Printf("[Dota2 GC] Requesting inventory")
	// TODO: via Steam Web API
}
