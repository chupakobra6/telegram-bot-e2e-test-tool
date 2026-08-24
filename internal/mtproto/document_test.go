package mtproto

import "testing"

func TestDocumentMetadataPreservesDOCXFilename(t *testing.T) {
	filename, mimeType := documentMetadata("/tmp/Дополнительное соглашение — на подпись.docx")

	if filename != "Дополнительное соглашение — на подпись.docx" {
		t.Fatalf("filename = %q", filename)
	}
	if mimeType != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Fatalf("mimeType = %q", mimeType)
	}
}

func TestDocumentMetadataFallsBackForUnknownExtension(t *testing.T) {
	filename, mimeType := documentMetadata("/tmp/archive.unknown-extension")

	if filename != "archive.unknown-extension" {
		t.Fatalf("filename = %q", filename)
	}
	if mimeType != "application/octet-stream" {
		t.Fatalf("mimeType = %q", mimeType)
	}
}
