package server

import "testing"

func TestLocalizeBuiltinPromptTemplate(t *testing.T) {
	item := builtinPromptTemplates[0]
	zh := localizePromptTemplate(item, "zh")
	if zh.Name != item.Name || zh.Body != item.Body {
		t.Fatalf("Chinese template should not be translated: %q", zh.Name)
	}
	en := localizePromptTemplate(item, "en")
	if en.Name != "Document review" {
		t.Fatalf("unexpected English name: %q", en.Name)
	}
	if en.Body == item.Body || en.Description == item.Description {
		t.Fatal("English builtin should have translated body and description")
	}
}

func TestLocalizeBuiltinPromptTemplateKeepsCustomization(t *testing.T) {
	db, err := openStateDB("")
	if err != nil {
		t.Fatal(err)
	}
	defer closeStateDB(db)
	store := newPromptTemplateStore(db)
	if err := store.ensureDefaults(); err != nil {
		t.Fatal(err)
	}

	// An untouched built-in still follows the UI language.
	untouched, ok := store.get("builtin-document-review")
	if !ok {
		t.Fatal("seeded template missing")
	}
	if got := localizePromptTemplate(untouched, "en"); got.Name != "Document review" {
		t.Fatalf("untouched builtin name = %q", got.Name)
	}

	if _, err := store.update("builtin-document-review", "我的评审", "我的说明", "我的正文", true, true, true); err != nil {
		t.Fatal(err)
	}
	customized, ok := store.get("builtin-document-review")
	if !ok {
		t.Fatal("customized template missing")
	}
	en := localizePromptTemplate(customized, "en")
	if en.Name != "我的评审" || en.Description != "我的说明" || en.Body != "我的正文" {
		t.Fatalf("customized builtin was replaced by the English default: %+v", en)
	}
}
