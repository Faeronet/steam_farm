package cs2

import "log"

type CarePackage struct {
	Options []CarePackageOption
}

type CarePackageOption struct {
	ItemName     string `json:"item_name"`
	ItemType     string `json:"item_type"`
	ItemImageURL string `json:"item_image_url"`
	Selected     bool   `json:"selected"`
}

func (c *GCClient) RedeemFreeReward(selectedIndices []int) error {
	log.Printf("[CS2 GC] Redeeming free reward with selections: %v", selectedIndices)
	// TODO: send CMsgGCCstrike15_v2_ClientRedeemFreeReward via GC
	return nil
}

func ParseCarePackageFromDrop(drop DropInfo) *CarePackage {
	return &CarePackage{
		Options: []CarePackageOption{
			{ItemName: "Kilowatt Case", ItemType: "case"},
			{ItemName: "Revolution Case", ItemType: "case"},
			{ItemName: "Graffiti", ItemType: "graffiti"},
			{ItemName: "Weapon Skin", ItemType: "skin"},
		},
	}
}
