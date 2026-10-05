package game

import (
	"archive/zip"
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type CharacterStats struct {
	Stars     int `json:"stars"`
	HP        int `json:"hp"`
	Attack    int `json:"attack"`
	DamageMin int `json:"damage_min"`
	DamageMax int `json:"damage_max"`
	Speed     int `json:"speed"`
	Armor     int `json:"armor"`
}
type CharacterDefinition struct {
	Kind               int              `json:"kind"`
	ID                 string           `json:"character_id"`
	Name               string           `json:"name"`
	Group              int              `json:"group"`
	Cost               int              `json:"cost"`
	Enabled            bool             `json:"enabled"`
	MaxCopies          int              `json:"max_copies"`
	Role               string           `json:"role"`
	Tags               string           `json:"tags"`
	Description        string           `json:"description"`
	AbilityKey         string           `json:"ability_key"`
	AbilityName        string           `json:"ability_name"`
	AbilityDescription string           `json:"ability_description"`
	AbilityCondition   string           `json:"ability_condition"`
	PassiveKey         string           `json:"passive_key"`
	PassiveName        string           `json:"passive_name"`
	PassiveDescription string           `json:"passive_description"`
	PassiveCondition   string           `json:"passive_condition"`
	ScenePath          string           `json:"scene_path"`
	EnemyScenePath     string           `json:"enemy_scene_path"`
	CardArtPath        string           `json:"card_art_path"`
	CardX              int              `json:"card_x"`
	CardY              int              `json:"card_y"`
	CardW              int              `json:"card_w"`
	CardH              int              `json:"card_h"`
	PortraitPath       string           `json:"portrait_path"`
	PortraitX          int              `json:"portrait_x"`
	PortraitY          int              `json:"portrait_y"`
	PortraitW          int              `json:"portrait_w"`
	PortraitH          int              `json:"portrait_h"`
	Stats              []CharacterStats `json:"stats"`
	Combat             *CombatRules     `json:"combat,omitempty"`
}
type CharacterCatalog struct {
	SchemaVersion int                   `json:"schema_version"`
	GroupNames    []string              `json:"group_names,omitempty"`
	Characters    []CharacterDefinition `json:"characters"`
}

const NeutralGroup = 4

func groupNames(c CharacterCatalog) []string {
	if len(c.GroupNames) != 0 {
		return c.GroupNames
	}
	return []string{"Knight", "Ranger", "Mage", "Guardian", "Neutral"}
}
func IsHeroGroup(group int) bool {
	if group < 0 || group >= len(groupNames(catalog)) || group == NeutralGroup {
		return false
	}
	for _, c := range catalog.Characters {
		if c.Enabled && c.Group == group {
			return true
		}
	}
	return false
}
func HeroGroups() []int {
	var groups []int
	for group := range groupNames(catalog) {
		if IsHeroGroup(group) {
			groups = append(groups, group)
		}
	}
	return groups
}

// JSON is the runtime source. The workbook reader remains available for legacy imports.
func LoadCharacterCatalog(path string) (CharacterCatalog, error) {
	if strings.EqualFold(filepath.Ext(path), ".xlsx") {
		return LoadCharacterWorkbook(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return CharacterCatalog{}, err
	}
	var c CharacterCatalog
	if err = json.Unmarshal(data, &c); err != nil {
		return c, err
	}
	if err = ValidateCharacterCatalog(c); err != nil {
		return c, err
	}
	return c, nil
}

func ValidateCharacterCatalog(c CharacterCatalog) error {
	if c.SchemaVersion != 1 || len(c.Characters) == 0 || len(c.Characters) > 4096 {
		return fmt.Errorf("invalid catalog schema or character count")
	}
	names := groupNames(c)
	if len(names) < 5 || len(names) > 64 || names[NeutralGroup] != "Neutral" {
		return fmt.Errorf("invalid character groups")
	}
	groupIDs := map[string]bool{}
	for _, name := range names {
		if strings.TrimSpace(name) == "" || groupIDs[name] {
			return fmt.Errorf("group names must be nonempty and unique")
		}
		groupIDs[name] = true
	}
	ids, characterNames := map[string]bool{}, map[string]bool{}
	counts := map[[2]int]int{}
	for kind, d := range c.Characters {
		if d.Kind != kind || !regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`).MatchString(d.ID) || ids[d.ID] || strings.TrimSpace(d.Name) == "" || characterNames[d.Name] {
			return fmt.Errorf("invalid or duplicate character identity at kind %d", kind)
		}
		ids[d.ID], characterNames[d.Name] = true, true
		if d.Group < 0 || d.Group >= len(names) || d.Cost < 2 || d.Cost > 6 || d.MaxCopies < 1 || d.MaxCopies > 4 {
			return fmt.Errorf("kind %d: invalid group, cost or copies", kind)
		}
		for _, path := range []string{d.ScenePath, d.EnemyScenePath, d.CardArtPath, d.PortraitPath} {
			if !strings.HasPrefix(path, "res://") || strings.Contains(path, "..") {
				return fmt.Errorf("kind %d: invalid resource path", kind)
			}
		}
		for _, crop := range [][4]int{{d.CardX, d.CardY, d.CardW, d.CardH}, {d.PortraitX, d.PortraitY, d.PortraitW, d.PortraitH}} {
			if crop[0] < 0 || crop[1] < 0 || crop[2] < 1 || crop[3] < 1 || crop[0] > 100000 || crop[1] > 100000 || crop[2] > 100000 || crop[3] > 100000 {
				return fmt.Errorf("kind %d: invalid image crop", kind)
			}
		}
		if (d.AbilityKey != "basic_attack" && d.AbilityKey != "character_combat") || d.AbilityCondition != "each_turn" || d.PassiveKey != "none" || d.PassiveCondition != "none" {
			return fmt.Errorf("kind %d: unsupported ability behavior", kind)
		}
		if d.Combat != nil {
			if err := d.Combat.Validate(); err != nil {
				return fmt.Errorf("kind %d: %w", kind, err)
			}
		}
		if len(d.Stats) != 4 {
			return fmt.Errorf("kind %d: requires four star stats", kind)
		}
		for i, s := range d.Stats {
			if s.Stars != i+1 || s.HP < 1 || s.HP > 100000 || s.Armor < 0 || s.Armor > 100000 || s.Attack < 1 || s.Attack > 100000 || s.DamageMin < 1 || s.DamageMax < s.DamageMin || s.DamageMax > 100000 || s.Speed < 1 || s.Speed > 1000 {
				return fmt.Errorf("kind %d: invalid star stats", kind)
			}
		}
		if d.Enabled {
			counts[[2]int{d.Group, d.Cost}]++
		}
	}
	for group := range names {
		for cost := 2; cost <= 6; cost++ {
			if counts[[2]int{group, cost}] < 2 {
				return fmt.Errorf("group %d cost %d requires at least two enabled characters", group, cost)
			}
		}
	}
	return nil
}

// A generated copy of the shipped workbook keeps standalone tests/CLI deterministic.
// Production always validates and loads the workbook at startup.
//
//go:embed data/characters.json
var seedCatalog []byte
var catalog = func() CharacterCatalog {
	var c CharacterCatalog
	if err := json.Unmarshal(seedCatalog, &c); err != nil {
		panic(err)
	}
	return c
}()
var CardKinds = len(catalog.Characters)

func Catalog() CharacterCatalog { return catalog }
func Character(kind int) CharacterDefinition {
	if kind < 0 || kind >= len(catalog.Characters) {
		return CharacterDefinition{Kind: -1}
	}
	return catalog.Characters[kind]
}
func StatsFor(kind, stars int) CharacterStats {
	if stars < 1 {
		stars = 1
	}
	if stars > 4 {
		stars = 4
	}
	return Character(kind).Stats[stars-1]
}
func UseCatalog(c CharacterCatalog) {
	catalog = c
	CardKinds = len(c.Characters)
	botDeckTemplates = makeBotDeckTemplates()
}

// Read only values, never evaluate formulas or execute workbook content.
func LoadCharacterWorkbook(path string) (CharacterCatalog, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return CharacterCatalog{}, fmt.Errorf("character workbook: %w", err)
	}
	defer z.Close()
	files := map[string]*zip.File{}
	for _, f := range z.File {
		files[f.Name] = f
	}
	read := func(name string, out any) error {
		f := files[name]
		if f == nil {
			return fmt.Errorf("missing %s", name)
		}
		if f.UncompressedSize64 > 20<<20 {
			return fmt.Errorf("%s too large", name)
		}
		r, e := f.Open()
		if e != nil {
			return e
		}
		defer r.Close()
		return xml.NewDecoder(io.LimitReader(r, 20<<20)).Decode(out)
	}
	var shared struct {
		Items []struct {
			Text string `xml:"t"`
			Runs []struct {
				Text string `xml:"t"`
			} `xml:"r"`
		} `xml:"si"`
	}
	if files["xl/sharedStrings.xml"] != nil {
		if err = read("xl/sharedStrings.xml", &shared); err != nil {
			return CharacterCatalog{}, err
		}
	}
	stringsTable := []string{}
	for _, s := range shared.Items {
		v := s.Text
		for _, r := range s.Runs {
			v += r.Text
		}
		stringsTable = append(stringsTable, v)
	}
	var book struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	var rels struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err = read("xl/workbook.xml", &book); err != nil {
		return CharacterCatalog{}, err
	}
	if err = read("xl/_rels/workbook.xml.rels", &rels); err != nil {
		return CharacterCatalog{}, err
	}
	type record struct {
		row    int
		values map[string]string
	}
	tables := map[string][]record{}
	for _, s := range book.Sheets {
		if s.Name != "Characters" && s.Name != "StarStats" {
			continue
		}
		target := ""
		for _, r := range rels.Items {
			if r.ID == s.ID {
				target = r.Target
			}
		}
		if strings.HasPrefix(target, "/") {
			target = strings.TrimPrefix(target, "/")
		} else {
			target = "xl/" + strings.TrimPrefix(target, "./")
		}
		var sheet struct {
			Rows []struct {
				Number int `xml:"r,attr"`
				Cells  []struct {
					Ref     string  `xml:"r,attr"`
					Type    string  `xml:"t,attr"`
					Value   string  `xml:"v"`
					Formula *string `xml:"f"`
					Inline  struct {
						Text string `xml:"t"`
						Runs []struct {
							Text string `xml:"t"`
						} `xml:"r"`
					} `xml:"is"`
				} `xml:"c"`
			} `xml:"sheetData>row"`
		}
		if err = read(target, &sheet); err != nil {
			return CharacterCatalog{}, err
		}
		headers := map[string]string{}
		found := false
		for _, row := range sheet.Rows {
			vals := map[string]string{}
			formulas := false
			for _, cell := range row.Cells {
				col := strings.TrimRight(cell.Ref, "0123456789")
				v := cell.Value
				if cell.Formula != nil {
					formulas = true
				}
				if cell.Type == "s" {
					i, e := strconv.Atoi(v)
					if e != nil || i < 0 || i >= len(stringsTable) {
						return CharacterCatalog{}, fmt.Errorf("%s!%s invalid shared string", s.Name, cell.Ref)
					}
					v = stringsTable[i]
				}
				if cell.Type == "inlineStr" {
					v = cell.Inline.Text
					for _, r := range cell.Inline.Runs {
						v += r.Text
					}
				}
				vals[col] = strings.TrimSpace(v)
			}
			if !found {
				for _, v := range vals {
					if v == "kind" {
						found = true
						headers = vals
						break
					}
				}
				continue
			}
			rec := record{row: row.Number, values: map[string]string{}}
			for col, header := range headers {
				rec.values[header] = vals[col]
			}
			if rec.values["kind"] == "" {
				empty := true
				for key, v := range rec.values {
					if key != "preview" && key != "notes" && v != "" {
						empty = false
					}
				}
				if empty {
					continue
				}
				return CharacterCatalog{}, fmt.Errorf("%s row %d: kind is required", s.Name, row.Number)
			}
			if formulas {
				return CharacterCatalog{}, fmt.Errorf("%s row %d: formulas are not supported; use values", s.Name, row.Number)
			}
			tables[s.Name] = append(tables[s.Name], rec)
		}
	}
	c := CharacterCatalog{SchemaVersion: 1}
	ids := map[string]bool{}
	names := map[string]bool{}
	kinds := map[int]int{}
	integer := func(v map[string]string, key string, min, max int) (int, error) {
		n, e := strconv.Atoi(v[key])
		if e != nil || n < min || n > max {
			return 0, fmt.Errorf("%s must be an integer %d–%d (got %q)", key, min, max, v[key])
		}
		return n, nil
	}
	var fail error
	for _, row := range tables["Characters"] {
		v := row.values
		d := CharacterDefinition{}
		payload := map[string]any{}
		for key, value := range v {
			payload[key] = value
		}
		for _, field := range []struct {
			name     string
			min, max int
		}{{"kind", 0, 4095}, {"group", 0, 4}, {"cost", 2, 6}, {"max_copies", 1, 4}, {"card_x", 0, 100000}, {"card_y", 0, 100000}, {"card_w", 1, 100000}, {"card_h", 1, 100000}, {"portrait_x", 0, 100000}, {"portrait_y", 0, 100000}, {"portrait_w", 1, 100000}, {"portrait_h", 1, 100000}} {
			payload[field.name], fail = integer(v, field.name, field.min, field.max)
			if fail != nil {
				return c, fmt.Errorf("Characters row %d: %w", row.row, fail)
			}
		}
		switch strings.ToLower(v["enabled"]) {
		case "true", "1":
			payload["enabled"] = true
		case "false", "0":
			payload["enabled"] = false
		default:
			return c, fmt.Errorf("Characters row %d: enabled must be TRUE/FALSE", row.row)
		}
		raw, _ := json.Marshal(payload)
		if err = json.Unmarshal(raw, &d); err != nil {
			return c, err
		}
		if !regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`).MatchString(d.ID) || ids[d.ID] {
			return c, fmt.Errorf("Characters row %d: invalid or duplicate character_id", row.row)
		}
		if _, exists := kinds[d.Kind]; exists {
			return c, fmt.Errorf("Characters row %d: duplicate kind %d", row.row, d.Kind)
		}
		if d.Name == "" || names[d.Name] {
			return c, fmt.Errorf("Characters row %d: name is required and must be unique", row.row)
		}
		for _, path := range []string{d.ScenePath, d.EnemyScenePath, d.CardArtPath, d.PortraitPath} {
			if !strings.HasPrefix(path, "res://") || strings.Contains(path, "..") {
				return c, fmt.Errorf("Characters row %d: asset paths must start with res:// without ..", row.row)
			}
		}
		if d.AbilityKey != "basic_attack" || d.AbilityCondition != "each_turn" || d.PassiveKey != "none" || d.PassiveCondition != "none" {
			return c, fmt.Errorf("Characters row %d: unsupported ability/passive behavior or condition", row.row)
		}
		names[d.Name] = true
		ids[d.ID] = true
		kinds[d.Kind] = len(c.Characters)
		c.Characters = append(c.Characters, d)
	}
	if len(c.Characters) == 0 {
		return c, fmt.Errorf("Characters table is empty or missing")
	}
	statsSeen := map[[2]int]bool{}
	for _, row := range tables["StarStats"] {
		v := row.values
		values := map[string]int{}
		for _, field := range []struct {
			name     string
			min, max int
		}{{"kind", 0, 4095}, {"stars", 1, 4}, {"hp", 1, 100000}, {"attack", 1, 100000}, {"damage_min", 1, 100000}, {"damage_max", 1, 100000}, {"speed", 1, 1000}} {
			values[field.name], fail = integer(v, field.name, field.min, field.max)
			if fail != nil {
				return c, fmt.Errorf("StarStats row %d: %w", row.row, fail)
			}
		}
		i, ok := kinds[values["kind"]]
		key := [2]int{values["kind"], values["stars"]}
		if !ok || statsSeen[key] {
			return c, fmt.Errorf("StarStats row %d: unknown kind or duplicate kind/stars", row.row)
		}
		if values["damage_min"] > values["damage_max"] {
			return c, fmt.Errorf("StarStats row %d: damage_min exceeds damage_max", row.row)
		}
		statsSeen[key] = true
		c.Characters[i].Stats = append(c.Characters[i].Stats, CharacterStats{Stars: values["stars"], HP: values["hp"], Attack: values["attack"], DamageMin: values["damage_min"], DamageMax: values["damage_max"], Speed: values["speed"]})
	}
	sort.Slice(c.Characters, func(i, j int) bool { return c.Characters[i].Kind < c.Characters[j].Kind })
	groups := map[[2]int]int{}
	for i := range c.Characters {
		d := &c.Characters[i]
		if d.Kind != i {
			return c, fmt.Errorf("kind must be contiguous from 0; missing %d", i)
		}
		if len(d.Stats) != 4 {
			return c, fmt.Errorf("kind %d requires StarStats for all four stars", i)
		}
		sort.Slice(d.Stats, func(i, j int) bool { return d.Stats[i].Stars < d.Stats[j].Stars })
		if d.Enabled {
			groups[[2]int{d.Group, d.Cost}]++
		}
	}
	for group := 0; group < 5; group++ {
		for cost := 2; cost <= 6; cost++ {
			if groups[[2]int{group, cost}] < 2 {
				return c, fmt.Errorf("group %d cost %d requires at least two enabled characters", group, cost)
			}
		}
	}
	return c, nil
}
