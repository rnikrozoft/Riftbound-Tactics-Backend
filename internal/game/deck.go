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

func CardGroup(kind int) int { return Character(kind).Group }
func DefaultDeck() Deck      { return deckForGroups("Starter", []int{0, 1, 2}) }
func deckForGroups(name string, heroes []int) Deck {
	d := Deck{Name: name, Heroes: heroes}
	groups := append(append([]int{}, heroes...), NeutralGroup)
	for _, group := range groups {
		for cost := 2; cost <= 6; cost++ {
			n := 0
			for _, c := range catalog.Characters {
				if c.Enabled && c.Group == group && c.Cost == cost {
					d.Cards = append(d.Cards, DeckCard{Kind: c.Kind, Copies: c.MaxCopies})
					n++
					if n == 2 {
						break
					}
				}
			}
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
	groups := map[int]bool{NeutralGroup: true}
	for _, g := range d.Heroes {
		if !IsHeroGroup(g) || groups[g] {
			return errors.New("choose three different heroes")
		}
		groups[g] = true
	}
	seen := map[int]bool{}
	counts := map[[2]int]int{}
	for _, c := range d.Cards {
		if c.Kind < 0 || c.Kind >= CardKinds || c.Copies < 1 || c.Copies > Character(c.Kind).MaxCopies || !Character(c.Kind).Enabled || seen[c.Kind] || !groups[CardGroup(c.Kind)] {
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
	p.RemainingCopies = make([]int, CardKinds)
	for _, c := range d.Cards {
		p.RemainingCopies[c.Kind] = c.Copies
	}
	return nil
}
