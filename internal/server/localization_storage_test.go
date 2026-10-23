package server

import (
	"context"
	"encoding/json"
	"github.com/rnikrozoft/riftbound-tactics-backend/internal/localization"
	"testing"
)

func TestLocalizationPublicationAndConditionalDownload(t *testing.T) {
	ctx := queueContext("locale-user")
	nk := inventoryFake()
	doc, err := localization.ReadWorkbook("../../data/localization.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if err = publishLocalization(ctx, nk, doc); err != nil {
		t.Fatal(err)
	}
	serial := nk.serial
	if err = publishLocalization(ctx, nk, doc); err != nil || nk.serial != serial {
		t.Fatal("unchanged workbook must not rewrite database", err)
	}
	raw, err := localizationBootstrap(ctx, nil, nil, nk, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	var first publishedLocalization
	if err = json.Unmarshal([]byte(raw), &first); err != nil || first.Document == nil || first.Version == "" {
		t.Fatal("first download missing document", err)
	}
	request, _ := json.Marshal(map[string]string{"cached_version": first.Version})
	raw, err = localizationBootstrap(ctx, nil, nil, nk, string(request))
	if err != nil {
		t.Fatal(err)
	}
	var cached publishedLocalization
	json.Unmarshal([]byte(raw), &cached)
	if cached.Document != nil || cached.Version != first.Version {
		t.Fatal("cache hit should send version only")
	}
	doc.Messages[0][2] += " updated"
	if err = publishLocalization(ctx, nk, doc); err != nil {
		t.Fatal(err)
	}
	raw, err = localizationBootstrap(ctx, nil, nil, nk, string(request))
	if err != nil {
		t.Fatal(err)
	}
	var updated publishedLocalization
	json.Unmarshal([]byte(raw), &updated)
	if updated.Document == nil || updated.Version == first.Version {
		t.Fatal("changed workbook did not invalidate cache")
	}
	for _, o := range nk.objects {
		if o.Key == localizationKey && (o.PermissionRead != 0 || o.PermissionWrite != 0) {
			t.Fatal("players must not write translations")
		}
	}
	if _, err = localizationBootstrap(context.Background(), nil, nil, nk, `{}`); err == nil {
		t.Fatal("unauthenticated localization request accepted")
	}
}
