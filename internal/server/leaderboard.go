package server

import (
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
)

const winLeaderboard = "riftbound_match_wins"

func matchWinnerID(state game.State) string {
	if state.Phase != "game_over" {
		return ""
	}
	for _, p := range state.Players {
		if p != nil && p.Team == state.Winner && p.HP > 0 {
			return p.UserID
		}
	}
	return ""
}
