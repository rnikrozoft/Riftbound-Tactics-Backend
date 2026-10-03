package game

import (
	"errors"
	"strings"
	"unicode/utf8"
)

type DeckCard struct {
	Kind   int `json:"kind"`
	Copies int `json:"copies"`
}
type Deck struct {
	Name   string     `json:"name"`
	Heroes []int      `json:"heroes"`
	Cards  []DeckCard `json:"cards"`
}

func CardGroup(kind int) int {
	if kind < 30 {
		return kind % 6 / 2
	}
	if kind < 40 {
		return 3
	}
	return 4
}
func DefaultDeck() Deck {
	d := Deck{Name: "Starter", Heroes: []int{0, 1, 2}}
	for k := 0; k < CardKinds; k++ {
		if k < 30 || k >= 40 && (k-40)%4 < 2 {
			d.Cards = append(d.Cards, DeckCard{Kind: k, Copies: 4})
		}
	}
	return d
}
func ValidateDeck(d Deck) error {
	if strings.TrimSpace(d.Name) == "" || utf8.RuneCountInString(d.Name) > 40 {
		return errors.New("deck name must contain 1-40 characters")
	}
	if len(d.Heroes) != 3 || len(d.Cards) != 40 {
		return errors.New("deck requires three heroes and forty distinct card slots")
	}
	groups := map[int]bool{4: true}
	for _, g := range d.Heroes {
		if g < 0 || g > 3 || groups[g] {
			return errors.New("choose three different heroes")
		}
		groups[g] = true
	}
	seen := map[int]bool{}
	counts := map[[2]int]int{}
	for _, c := range d.Cards {
		if c.Kind < 0 || c.Kind >= CardKinds || c.Copies < 1 || c.Copies > 4 || seen[c.Kind] || !groups[CardGroup(c.Kind)] {
			return errors.New("invalid card kind, group or copy count")
		}
		seen[c.Kind] = true
		counts[[2]int{CardGroup(c.Kind), CardCost(c.Kind)}]++
	}
	for g := range groups {
		for cost := 2; cost <= 6; cost++ {
			if counts[[2]int{g, cost}] != 2 {
				return errors.New("each group requires two different cards at every cost from two to six")
			}
		}
	}
	return nil
}
func (r *Room) SetDeck(id string, d Deck) error {
	if err := ValidateDeck(d); err != nil {
		return err
	}
	p := r.Player(id)
	if p == nil || r.State.Phase != "waiting" {
		return errors.New("deck can only be selected before preparation")
	}
	p.DeckName = d.Name
	p.RemainingCopies = [CardKinds]int{}
	for _, c := range d.Cards {
		p.RemainingCopies[c.Kind] = c.Copies
	}
	return nil
}
