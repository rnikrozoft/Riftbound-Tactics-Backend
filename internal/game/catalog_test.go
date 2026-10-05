package game

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func workbookFixture(t *testing.T, edit func(string, string) string) string {
	t.Helper()
	input, err := zip.OpenReader("testdata/characters.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	path := filepath.Join(t.TempDir(), "characters.xlsx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, entry := range input.File {
		r, e := entry.Open()
		if e != nil {
			t.Fatal(e)
		}
		data, e := io.ReadAll(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
		w, e := z.Create(entry.Name)
		if e != nil {
			t.Fatal(e)
		}
		content := string(data)
		if strings.HasPrefix(entry.Name, "xl/worksheets/") {
			content = strings.ReplaceAll(strings.ReplaceAll(content, "<x:", "<"), "</x:", "</")
		}
		if _, e = w.Write([]byte(edit(entry.Name, content))); e != nil {
			t.Fatal(e)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
func setNumericCell(xml, cell, value string) string {
	re := regexp.MustCompile(`(?s)(<c\b[^>]*\br="` + cell + `"[^>]*>.*?<v>).*?(</v>)`)
	return re.ReplaceAllString(xml, `${1}`+value+`${2}`)
}
func setTextCell(xml, cell, value string) string {
	re := regexp.MustCompile(`(?s)<c\b[^>]*\br="` + cell + `"[^>]*>.*?</c>`)
	return re.ReplaceAllString(xml, `<c r="`+cell+`" t="inlineStr"><is><t>`+value+`</t></is></c>`)
}
func TestLegacyWorkbookPreservesCatalogIdentities(t *testing.T) {
	c, err := LoadCharacterWorkbook("testdata/characters.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	var seed CharacterCatalog
	if err = json.Unmarshal(seedCatalog, &seed); err != nil {
		t.Fatal(err)
	}
	for i, d := range c.Characters {
		if d.Kind != seed.Characters[i].Kind || d.ID != seed.Characters[i].ID || d.Cost != seed.Characters[i].Cost {
			t.Fatal("legacy workbook identities changed")
		}
	}
	if len(c.Characters) != 60 || len(c.Characters[59].Stats) != 4 {
		t.Fatal("shipped rows missing")
	}
}
func TestWorkbookRejectsInvalidRows(t *testing.T) {
	for _, test := range []struct{ name, sheet, cell, value, contains string }{{"duplicate kind", "sheet1.xml", "A7", "0", "duplicate kind"}, {"invalid hp", "sheet2.xml", "C6", "0", "hp must"}, {"damage range", "sheet2.xml", "E6", "99", "exceeds"}, {"invalid enabled", "sheet1.xml", "F6", "2", "TRUE/FALSE"}} {
		t.Run(test.name, func(t *testing.T) {
			p := workbookFixture(t, func(name, content string) string {
				if name == "xl/worksheets/"+test.sheet {
					return setNumericCell(content, test.cell, test.value)
				}
				return content
			})
			_, err := LoadCharacterWorkbook(p)
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("unexpected validation result: %v", err)
			}
		})
	}
	t.Run("missing stars", func(t *testing.T) {
		p := workbookFixture(t, func(name, s string) string {
			if name == "xl/worksheets/sheet2.xml" {
				return regexp.MustCompile(`(?s)<row\b[^>]*\br="245"[^>]*>.*?</row>`).ReplaceAllString(s, "")
			}
			return s
		})
		_, err := LoadCharacterWorkbook(p)
		if err == nil || !strings.Contains(err.Error(), "four stars") {
			t.Fatalf("missing stats accepted: %v", err)
		}
	})
	t.Run("unknown ability", func(t *testing.T) {
		p := workbookFixture(t, func(name, s string) string {
			if name == "xl/worksheets/sheet1.xml" {
				return setTextCell(s, "K6", "unimplemented_spell")
			}
			return s
		})
		_, err := LoadCharacterWorkbook(p)
		if err == nil || !strings.Contains(err.Error(), "unsupported ability") {
			t.Fatalf("unknown behavior accepted: %v", err)
		}
	})
	t.Run("formula", func(t *testing.T) {
		p := workbookFixture(t, func(name, s string) string {
			if name == "xl/worksheets/sheet2.xml" {
				re := regexp.MustCompile(`(<c\b[^>]*\br="C6"[^>]*>)`)
				return re.ReplaceAllString(s, `${1}<f>100+1</f>`)
			}
			return s
		})
		_, err := LoadCharacterWorkbook(p)
		if err == nil || !strings.Contains(err.Error(), "formulas") {
			t.Fatalf("formula accepted: %v", err)
		}
	})
}
func TestWorkbookCanAddFutureCharacter(t *testing.T) {
	p := workbookFixture(t, func(name, s string) string {
		if name == "xl/worksheets/sheet1.xml" {
			row := regexp.MustCompile(`(?s)<row\b[^>]*\br="65"[^>]*>.*?</row>`).FindString(s)
			row = strings.ReplaceAll(row, `r="65"`, `r="66"`)
			row = regexp.MustCompile(`r="([A-Z]+)65"`).ReplaceAllString(row, `r="${1}66"`)
			row = setNumericCell(row, "A66", "60")
			row = setTextCell(row, "B66", "character_060")
			row = setTextCell(row, "C66", "Future Neutral Hero")
			return strings.Replace(s, "</sheetData>", row+"</sheetData>", 1)
		}
		if name == "xl/worksheets/sheet2.xml" {
			extra := ""
			for i := 0; i < 4; i++ {
				old, newRow := strconv.Itoa(242+i), strconv.Itoa(246+i)
				row := regexp.MustCompile(`(?s)<row\b[^>]*\br="` + old + `"[^>]*>.*?</row>`).FindString(s)
				row = strings.ReplaceAll(row, `r="`+old+`"`, `r="`+newRow+`"`)
				row = regexp.MustCompile(`r="([A-Z]+)`+old+`"`).ReplaceAllString(row, `r="${1}`+newRow+`"`)
				row = setNumericCell(row, "A"+newRow, "60")
				extra += row
			}
			return strings.Replace(s, "</sheetData>", extra+"</sheetData>", 1)
		}
		return s
	})
	c, err := LoadCharacterWorkbook(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Characters) != 61 || c.Characters[60].Name != "Future Neutral Hero" {
		t.Fatal("new character missing")
	}
	old := Catalog()
	defer UseCatalog(old)
	UseCatalog(c)
	deck := DefaultDeck()
	deck.Cards[len(deck.Cards)-1] = DeckCard{Kind: 60, Copies: 4}
	if err := ValidateDeck(deck); err != nil {
		t.Fatal("new character cannot be added to deck:", err)
	}
	if len(newPlayer("future", "A").RemainingCopies) != 61 {
		t.Fatal("pool did not grow for new character")
	}
}
func TestWorkbookStatsDriveCombat(t *testing.T) {
	path := workbookFixture(t, func(name, s string) string {
		if name == "xl/worksheets/sheet2.xml" {
			for cell, value := range map[string]string{"C6": "222", "D6": "7", "E6": "7", "F6": "7", "G6": "20"} {
				s = setNumericCell(s, cell, value)
			}
		}
		return s
	})
	c, err := LoadCharacterWorkbook(path)
	if err != nil {
		t.Fatal(err)
	}
	old := Catalog()
	defer UseCatalog(old)
	UseCatalog(c)
	r := setup(t)
	for side, p := range r.State.Players {
		p.Units = []Unit{{Kind: 0, Token: side + 1, Slot: 0, Stars: 1}}
	}
	plan := r.Start(2000)
	if plan.Units[0].MaxHP != 222 || plan.Units[0].Speed != 20 || plan.Units[0].Attack != 7 || plan.Events[0].Damage != 7 || plan.Events[0].TargetHP != 215 || plan.Events[1].AtMS-plan.Events[0].AtMS != 1200 {
		t.Fatal("combat did not use workbook stats")
	}
	raw, _ := json.Marshal(Catalog())
	if !bytes.Contains(raw, []byte(`"hp":222`)) {
		t.Fatal("catalog RPC metadata mismatch")
	}
}
