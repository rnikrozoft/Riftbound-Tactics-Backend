package localization

import (
	"reflect"
	"testing"
)

func sampleRows() []Row {
	return []Row{{"M1", "messages", "Cost {0}", "ค่าร่าย {0}", "Cost {0}", "ค่าร่าย {0}"}, {"F1", "fragments", "HP", "พลังชีวิต", "HP", "พลังชีวิต"}}
}
func TestWorkbookGolden(t *testing.T) {
	doc, err := ReadWorkbook("../../data/localization.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Messages) < 300 || len(doc.Fragments) < 30 {
		t.Fatal("catalog truncated")
	}
	found := false
	for _, r := range doc.Messages {
		if r[0] == "Play" && r[2] == "Play" && r[3] == "เล่น" {
			found = true
		}
	}
	if !found {
		t.Fatal("Unicode or source columns lost")
	}
}
func TestEditableTextPreservesSourceAndVersions(t *testing.T) {
	rows := sampleRows()
	a, err := Compile(rows)
	if err != nil {
		t.Fatal(err)
	}
	rows[0].EN = "Price {0}"
	b, err := Compile(rows)
	if err != nil {
		t.Fatal(err)
	}
	if b.Messages[0][0] != "Cost {0}" || b.Messages[0][2] != "Price {0}" || Version(a) == Version(b) {
		t.Fatal("editable English lost source key or version did not change")
	}
	reverse := []Row{rows[1], rows[0]}
	c, _ := Compile(reverse)
	if !reflect.DeepEqual(b, c) {
		t.Fatal("sorting rows changed publication")
	}
}
func TestInvalidAdminEditsFail(t *testing.T) {
	for _, change := range []func([]Row){func(r []Row) { r[0].TH = "" }, func(r []Row) { r[0].TH = "ค่าร่าย" }, func(r []Row) { r[0].TH = "ค่าร่าย {1}" }, func(r []Row) { r[1].Key = r[0].Key }, func(r []Row) { r[0].Kind = "other" }} {
		rows := sampleRows()
		change(rows)
		if _, err := Compile(rows); err == nil {
			t.Fatal("invalid edit accepted")
		}
	}
}
