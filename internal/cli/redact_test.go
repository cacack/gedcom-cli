package cli

import (
	"strings"
	"testing"

	"github.com/cacack/gedcom-go/v3/gedcom"
)

// Every record hiddenRecords returns must be one redactDocument rewrites: a
// hidden record of a type it does not handle would otherwise be shown whole.
func TestRedactDocumentRewritesEveryHiddenRecord(t *testing.T) {
	doc, err := decodeFile(living)
	if err != nil {
		t.Fatal(err)
	}
	hidden := hiddenRecords(doc, presumedLiving(doc, gedcom.LivingConfig{MaxAge: gedcom.DefaultLivingMaxAge, ReferenceYear: 2026}))
	types := map[gedcom.RecordType]bool{}
	for record := range hidden {
		types[record.Type] = true
	}
	for _, want := range []gedcom.RecordType{gedcom.RecordTypeIndividual, gedcom.RecordTypeFamily, gedcom.RecordTypeNote} {
		if !types[want] {
			t.Fatalf("fixture hides no %s record", want)
		}
	}
	if err := redactDocument(hidden); err != nil {
		t.Fatal(err)
	}
	for record := range hidden {
		if record.Tags != nil || record.Value != "" {
			t.Errorf("%s (%s) is hidden but was not rewritten", record.XRef, record.Type)
		}
	}
}

func TestRedactDocumentRejectsUnhandledType(t *testing.T) {
	source := &gedcom.Record{
		XRef:   "@S1@",
		Type:   gedcom.RecordTypeSource,
		Entity: &gedcom.Source{XRef: "@S1@", Title: "Example Parish Register"},
		Tags:   []*gedcom.Tag{{Level: 1, Tag: "TITL", Value: "Example Parish Register"}},
	}
	err := redactDocument(map[*gedcom.Record]bool{source: true})
	if err == nil {
		t.Fatal("redactDocument passed a source record through unredacted")
	}
	if !strings.Contains(err.Error(), "@S1@") || !strings.Contains(err.Error(), "SOUR") {
		t.Errorf("error does not name the record and its type: %v", err)
	}
}
