package tools

import "testing"

func TestExtractTextStripsScript(t *testing.T) {
	html := `<html><head><script>alert("xss")</script></head><body><p>Hello World</p></body></html>`
	result := extractText(html)
	if contains(result, "alert") {
		t.Fatalf("script content should be stripped, got: %s", result)
	}
	if !contains(result, "Hello World") {
		t.Fatalf("body content should be preserved, got: %s", result)
	}
}

func TestExtractTextStripsStyle(t *testing.T) {
	html := `<html><head><style>.btn { color: red; }</style></head><body><p>Visible text here</p></body></html>`
	result := extractText(html)
	if contains(result, "color: red") {
		t.Fatalf("style content should be stripped, got: %s", result)
	}
	if !contains(result, "Visible text here") {
		t.Fatalf("body content should be preserved, got: %s", result)
	}
}

func TestExtractTextScriptWithAttributes(t *testing.T) {
	html := `<script src="app.js"></script><p>After script</p>`
	result := extractText(html)
	if !contains(result, "After script") {
		t.Fatalf("content after script tag should be preserved, got: %s", result)
	}
}

func TestExtractTextNestedTags(t *testing.T) {
	html := `<div><p>First <b>bold</b> paragraph</p><p>Second paragraph</p></div>`
	result := extractText(html)
	if !contains(result, "First  bold  paragraph") && !contains(result, "First bold paragraph") {
		t.Fatalf("nested tag content should be extracted, got: %s", result)
	}
	if !contains(result, "Second paragraph") {
		t.Fatalf("second paragraph should be present, got: %s", result)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
