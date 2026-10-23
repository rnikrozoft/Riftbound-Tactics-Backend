package main

import (
	"fmt"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/localization"
	"os"
)

func main() {
	file := "data/localization.xlsx"
	if len(os.Args) > 1 {
		file = os.Args[1]
	}
	doc, err := localization.ReadWorkbook(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Localization valid: %d messages, %d fragments, version %s\n", len(doc.Messages), len(doc.Fragments), localization.Version(doc))
}
