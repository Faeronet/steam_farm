package cs2

import "log"

type InventoryItem struct {
	AssetID    int64  `json:"asset_id"`
	ClassID    int64  `json:"class_id"`
	InstanceID int64  `json:"instance_id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	ImageURL   string `json:"image_url"`
	Tradeable  bool   `json:"tradeable"`
}

func (c *GCClient) RequestInventory() {
	log.Printf("[CS2 GC] Requesting inventory")
	// TODO: request via Steam Web API or GC
}
