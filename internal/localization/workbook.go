package localization

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Document struct {
	SchemaVersion int         `json:"schema_version"`
	Messages      [][4]string `json:"messages"`
	Fragments     [][4]string `json:"fragments"`
}
type Row struct{ Key, Kind, SourceEN, SourceTH, EN, TH string }

var placeholders = regexp.MustCompile(`\{[0-9]+\}`)

func arguments(s string) string {
	set := map[string]bool{}
	for _, p := range placeholders.FindAllString(s, -1) {
		set[p] = true
	}
	values := []string{}
	for p := range set {
		values = append(values, p)
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}
func Compile(rows []Row) (Document, error) {
	doc := Document{SchemaVersion: 1, Messages: [][4]string{}, Fragments: [][4]string{}}
	keys := map[string]bool{}
	sources := map[string][2]string{}
	rows = append([]Row(nil), rows...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	for _, r := range rows {
		if r.Key == "" || keys[r.Key] {
			return doc, fmt.Errorf("missing or duplicate key %q", r.Key)
		}
		keys[r.Key] = true
		if r.Kind != "messages" && r.Kind != "fragments" {
			return doc, fmt.Errorf("%s: type must be messages or fragments", r.Key)
		}
		values := []string{r.SourceEN, r.SourceTH, r.EN, r.TH}
		for _, s := range values {
			if strings.TrimSpace(s) == "" || len(s) > 16000 {
				return doc, fmt.Errorf("%s: empty or excessive text", r.Key)
			}
			if arguments(s) != arguments(r.SourceEN) {
				return doc, fmt.Errorf("%s: placeholders must match %s", r.Key, arguments(r.SourceEN))
			}
		}
		if r.Kind == "fragments" && arguments(r.SourceEN) != "" {
			return doc, fmt.Errorf("%s: fragments cannot use placeholders", r.Key)
		}
		for _, source := range []string{r.SourceEN} {
			key := r.Kind + ":" + source
			if previous, ok := sources[key]; ok && previous != [2]string{r.EN, r.TH} {
				return doc, fmt.Errorf("%s: conflicting source %q", r.Key, source)
			}
			sources[key] = [2]string{r.EN, r.TH}
		}
		pair := [4]string{r.SourceEN, r.SourceTH, r.EN, r.TH}
		if r.Kind == "messages" {
			doc.Messages = append(doc.Messages, pair)
		} else {
			doc.Fragments = append(doc.Fragments, pair)
		}
	}
	if len(doc.Messages) == 0 || len(doc.Fragments) == 0 {
		return doc, fmt.Errorf("messages and fragments must both be populated")
	}
	return doc, nil
}
func Version(doc Document) string {
	raw, _ := json.Marshal(doc)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
func text(raw []byte) string {
	d := xml.NewDecoder(bytes.NewReader(raw))
	var out strings.Builder
	for {
		token, err := d.Token()
		if err != nil {
			break
		}
		if e, ok := token.(xml.StartElement); ok && e.Name.Local == "t" {
			var value string
			if d.DecodeElement(&value, &e) == nil {
				out.WriteString(value)
			}
		}
	}
	return out.String()
}
func ReadWorkbook(filename string) (Document, error) {
	z, err := zip.OpenReader(filename)
	if err != nil {
		return Document{}, err
	}
	defer z.Close()
	files := map[string]*zip.File{}
	for _, f := range z.File {
		files[f.Name] = f
	}
	read := func(name string) ([]byte, error) {
		f := files[name]
		if f == nil {
			return nil, fmt.Errorf("missing XLSX member %s", name)
		}
		if f.UncompressedSize64 > 16<<20 {
			return nil, fmt.Errorf("XLSX member too large")
		}
		r, e := f.Open()
		if e != nil {
			return nil, e
		}
		defer r.Close()
		return io.ReadAll(io.LimitReader(r, 16<<20))
	}
	workbook, err := read("xl/workbook.xml")
	if err != nil {
		return Document{}, err
	}
	var book struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err = xml.Unmarshal(workbook, &book); err != nil {
		return Document{}, err
	}
	id := ""
	for _, sheet := range book.Sheets {
		if sheet.Name == "Translations" {
			id = sheet.ID
		}
	}
	if id == "" {
		return Document{}, fmt.Errorf("missing Translations worksheet")
	}
	rel, err := read("xl/_rels/workbook.xml.rels")
	if err != nil {
		return Document{}, err
	}
	var relationships struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err = xml.Unmarshal(rel, &relationships); err != nil {
		return Document{}, err
	}
	target := ""
	for _, r := range relationships.Items {
		if r.ID == id {
			target = path.Join("xl", r.Target)
			if strings.HasPrefix(r.Target, "/") {
				target = strings.TrimPrefix(r.Target, "/")
			}
		}
	}
	if target == "" {
		return Document{}, fmt.Errorf("missing worksheet relationship")
	}
	shared := []string{}
	if files["xl/sharedStrings.xml"] != nil {
		raw, e := read("xl/sharedStrings.xml")
		if e != nil {
			return Document{}, e
		}
		var ss struct {
			Items []struct {
				Raw []byte `xml:",innerxml"`
			} `xml:"si"`
		}
		if e = xml.Unmarshal(raw, &ss); e != nil {
			return Document{}, e
		}
		for _, item := range ss.Items {
			shared = append(shared, text(item.Raw))
		}
	}
	raw, err := read(target)
	if err != nil {
		return Document{}, err
	}
	var sheet struct {
		Rows []struct {
			Number int `xml:"r,attr"`
			Cells  []struct {
				Ref    string `xml:"r,attr"`
				Type   string `xml:"t,attr"`
				Value  string `xml:"v"`
				Inline struct {
					Raw []byte `xml:",innerxml"`
				} `xml:"is"`
				Formula *string `xml:"f"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err = xml.Unmarshal(raw, &sheet); err != nil {
		return Document{}, err
	}
	headers := map[string]int{}
	rows := []Row{}
	for _, r := range sheet.Rows {
		cells := map[int]string{}
		for _, c := range r.Cells {
			col := 0
			for _, letter := range c.Ref {
				if letter < 'A' || letter > 'Z' {
					break
				}
				col = col*26 + int(letter-'A'+1)
			}
			if c.Formula != nil {
				return Document{}, fmt.Errorf("row %d: formulas are not allowed", r.Number)
			}
			value := c.Value
			if c.Type == "s" {
				n, e := strconv.Atoi(value)
				if e != nil || n < 0 || n >= len(shared) {
					return Document{}, fmt.Errorf("invalid shared string")
				}
				value = shared[n]
			} else if c.Type == "inlineStr" {
				value = text(c.Inline.Raw)
			}
			cells[col] = value
		}
		if len(headers) == 0 {
			for col, value := range cells {
				if value == "key" || value == "type" || value == "source_en" || value == "source_th" || value == "en" || value == "th" {
					headers[value] = col
				}
			}
			if len(headers) != 6 {
				headers = map[string]int{}
			}
			continue
		}
		get := func(name string) string { return cells[headers[name]] }
		if get("key") == "" && get("en") == "" && get("th") == "" {
			continue
		}
		rows = append(rows, Row{get("key"), get("type"), get("source_en"), get("source_th"), get("en"), get("th")})
	}
	if len(headers) == 0 {
		return Document{}, fmt.Errorf("missing key/type/source_en/source_th/en/th headers")
	}
	return Compile(rows)
}
