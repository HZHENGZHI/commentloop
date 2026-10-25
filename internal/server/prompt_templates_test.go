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
