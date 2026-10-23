package game

import "errors"

// Only cards originating from purchases consume shop hand capacity.
// Generated defaults to false so ordinary cards retain their existing restrictions.
func purchasedHandCount(hand []Card) int {
	count := 0
	for _, card := range hand {
		if !card.Generated {
			count++
		}
	}
	return count
}

// GrantGeneratedCard is for authoritative skill resolution, never a player action.
// Granting does not buy from the pool, spend coins, merge cards or enforce shop capacity.
func (r *Room) GrantGeneratedCard(userID string, kind int) error {
	p := r.Player(userID)
	if p == nil || kind < 0 || kind >= CardKinds {
		return errors.New("invalid generated card")
	}
	r.serial++
	p.Hand = append(p.Hand, Card{Token: r.serial, Kind: kind, Price: CardCost(kind), Stars: 1, Generated: true})
	r.State.Revision++
	return nil
}
