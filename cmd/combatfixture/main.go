// Generate a real authoritative plan for the Godot effects integration runner.
package main

import (
	"encoding/json"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"os"
	"strings"
)

func main() {
	r := game.New("123456", "effects-a", 81)
	r.Reserve("effects-b")
	r.Connect("effects-a", 0)
	r.Connect("effects-b", 0)
	names := [][]string{{"swordsman", "warlock", "wizard", "ghostfire", "knight", "demoness_a"}, {"armored_skeleton", "blood_monster", "knight_templar", "black_knight_a", "lancer", "werebear"}}
	for side, p := range r.State.Players {
		p.Units = nil
		for i, slug := range names[side] {
			kind := -1
			for _, d := range game.Catalog().Characters {
				if strings.HasSuffix(d.ScenePath, "/"+slug+".tscn") {
					kind = d.Kind
					break
				}
			}
			if kind < 0 {
				panic(slug)
			}
			p.Units = append(p.Units, game.Unit{Kind: kind, Token: side*10 + i + 1, Slot: i, Stars: 1 + i%4})
		}
	}
	r.Start(0)
	out := os.Stdout
	if len(os.Args) > 1 {
		var err error
		out, err = os.Create(os.Args[1])
		if err != nil {
			panic(err)
		}
		defer out.Close()
	}
	if err := json.NewEncoder(out).Encode(r.State); err != nil {
		panic(err)
	}
}
