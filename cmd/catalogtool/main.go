package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/game"
	"os"
)

func main() {
	path := flag.String("file", "data/characters.json", "Catalog JSON or legacy workbook path")
	output := flag.String("json", "", "Optional validated JSON output for client/offline seed")
	flag.Parse()
	c, err := game.LoadCharacterCatalog(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *output != "" {
		data, _ := json.MarshalIndent(c, "", "  ")
		if err = os.WriteFile(*output, data, 0644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	fmt.Printf("Valid catalog: %d characters, %d star-stat rows\n", len(c.Characters), len(c.Characters)*4)
}
