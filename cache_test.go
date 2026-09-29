package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeChat(t *testing.T, project, uuid string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(projectsDir, project)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, uuid+".jsonl")
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFindAllChats_UsesCacheWhileFileUnchanged(t *testing.T) {
	setupStorageDirs(t)
	path := writeChat(t, "p", "a", `{"type":"user","message":{"content":"real"},"version":"1"}`)
	findAllChats()

	// Tamper with the cached title: a cache hit must return it verbatim.
	entries := loadMetadataCache()
	e := entries[path]
	e.Title = "from-cache"
	entries[path] = e
	if err := saveMetadataCache(entries); err != nil {
		t.Fatal(err)
	}

	if got := findAllChats()[0].Title; got != "from-cache" {
		t.Errorf("title = %q, want cached %q", got, "from-cache")
	}
}

func TestFindAllChats_RescansWhenFileChanges(t *testing.T) {
	setupStorageDirs(t)
	path := writeChat(t, "p", "a", `{"type":"user","message":{"content":"first"},"version":"1"}`)
	findAllChats()

	writeChat(t, "p", "a",
		`{"type":"user","message":{"content":"first"},"version":"1"}`,
		`{"type":"custom-title","customTitle":"renamed"}`)
	later := time.Now().Add(time.Minute)
	os.Chtimes(path, later, later)

	chats := findAllChats()
	if chats[0].Title != "renamed" || chats[0].LineCount != 2 {
		t.Errorf("got title=%q lines=%d, want renamed/2", chats[0].Title, chats[0].LineCount)
	}
}

func TestFindAllChats_PrunesDeletedChatsFromCache(t *testing.T) {
	setupStorageDirs(t)
	keep := writeChat(t, "p", "keep", `{"type":"user","message":{"content":"k"}}`)
	gone := writeChat(t, "p", "gone", `{"type":"user","message":{"content":"g"}}`)
	findAllChats()

	os.Remove(gone)
	findAllChats()

	entries := loadMetadataCache()
	if _, ok := entries[gone]; ok {
		t.Error("deleted chat still in cache")
	}
	if _, ok := entries[keep]; !ok {
		t.Error("existing chat missing from cache")
	}
}

func TestLoadMetadataCache_IgnoresOtherVersions(t *testing.T) {
	setupStorageDirs(t)
	os.WriteFile(metadataCachePath, []byte(`{"version":0,"entries":{"x":{"title":"old"}}}`), 0644)
	if got := loadMetadataCache(); got != nil {
		t.Errorf("got %v, want nil for mismatched version", got)
	}
}
