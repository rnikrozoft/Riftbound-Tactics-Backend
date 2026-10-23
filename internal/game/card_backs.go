package game

import (
	_ "embed"
	"encoding/json"
)

const DefaultCardBackID = "card_back_00"

type CardBackDefinition struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Price       int64  `json:"price"`
	TexturePath string `json:"texture_path"`
	X           int    `json:"x"`
	Y           int    `json:"y"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

//go:embed data/card_backs.json
var cardBackJSON []byte
var CardBacks = func() []CardBackDefinition {
	var backs []CardBackDefinition
	if err := json.Unmarshal(cardBackJSON, &backs); err != nil {
		panic(err)
	}
	return backs
}()

func NormalizeCardBackID(id string) string {
	if id == "" {
		return DefaultCardBackID
	}
	return id
}
func CardBack(id string) (CardBackDefinition, bool) {
	id = NormalizeCardBackID(id)
	for _, back := range CardBacks {
		if back.ID == id {
			return back, true
		}
	}
	return CardBackDefinition{}, false
}
