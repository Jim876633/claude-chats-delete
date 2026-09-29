package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// metadataCacheVersion must be bumped whenever scanChatMetadata's output
// changes, so stale entries computed by older logic are discarded.
const metadataCacheVersion = 1

var metadataCachePath = filepath.Join(os.Getenv("HOME"), ".config", "claude-chats", "metadata-cache.json")

// cachedMeta is a scanChatMetadata result, valid only while the file's size
// and mtime still match.
type cachedMeta struct {
	Size         int64  `json:"size"`
	ModTime      int64  `json:"mtime"`
	Title        string `json:"title"`
	Version      string `json:"version"`
	ForkParentID string `json:"forkParentId,omitempty"`
	LineCount    int    `json:"lineCount"`
}

type metadataCache struct {
	Version int                   `json:"version"`
	Entries map[string]cachedMeta `json:"entries"` // keyed by absolute jsonl path
}

func loadMetadataCache() map[string]cachedMeta {
	data, err := os.ReadFile(metadataCachePath)
	if err != nil {
		return nil
	}
	var c metadataCache
	if err := json.Unmarshal(data, &c); err != nil || c.Version != metadataCacheVersion {
		return nil
	}
	return c.Entries
}

// saveMetadataCache writes via a temp file + rename so a crash or a second
// instance never leaves a half-written cache behind.
func saveMetadataCache(entries map[string]cachedMeta) error {
	if err := os.MkdirAll(filepath.Dir(metadataCachePath), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(metadataCache{Version: metadataCacheVersion, Entries: entries})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(metadataCachePath), ".metadata-cache-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), metadataCachePath)
}
