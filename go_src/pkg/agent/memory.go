// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/fileutil"
)

// MemoryStore manages persistent memory for the agent.
// - Long-term memory: memory/MEMORY.md (default) or memory/characters/{id}/MEMORY.md (character)
// - Daily notes: memory/YYYYMM/YYYYMMDD.md or memory/characters/{id}/YYYYMM/YYYYMMDD.md
type MemoryStore struct {
	workspace    string
	memoryDir    string
	memoryFile   string
	characterID  string // empty = default agent, non-empty = character-isolated paths
}

// characterMemoryDir returns the character-specific memory root, or "" if default.
func (ms *MemoryStore) characterMemoryDir() string {
	if ms.characterID == "" {
		return ""
	}
	return filepath.Join(ms.workspace, "memory", "characters", ms.characterID)
}

// NewMemoryStore creates a new MemoryStore with the given workspace path.
// It ensures the memory directory exists.
func NewMemoryStore(workspace string) *MemoryStore {
	memoryDir := filepath.Join(workspace, "memory")
	memoryFile := filepath.Join(memoryDir, "MEMORY.md")

	// Ensure memory directory exists
	os.MkdirAll(memoryDir, 0o755)

	return &MemoryStore{
		workspace:   workspace,
		memoryDir:   memoryDir,
		memoryFile:  memoryFile,
		characterID: "",
	}
}

// SetCharacterID switches this MemoryStore to read/write character-isolated paths.
// Pass "" to revert to the default (shared) memory.
// Creates the character memory directory if it doesn't exist.
func (ms *MemoryStore) SetCharacterID(id string) {
	ms.characterID = id
	charDir := ms.characterMemoryDir()
	if charDir != "" {
		os.MkdirAll(charDir, 0o755)
		ms.memoryFile = filepath.Join(charDir, "MEMORY.md")
	} else {
		ms.memoryFile = filepath.Join(ms.memoryDir, "MEMORY.md")
	}
}

// CharacterID returns the current character ID, or "" if using default memory.
func (ms *MemoryStore) CharacterID() string {
	return ms.characterID
}

// CharacterMemoryPath returns the character memory root path, or "" if default.
func (ms *MemoryStore) CharacterMemoryDir() string {
	return ms.characterMemoryDir()
}

// MemoryFilePath returns the current long-term memory file path.
func (ms *MemoryStore) MemoryFilePath() string {
	return ms.memoryFile
}

// getTodayFile returns the path to today's daily note file.
// When a character is active, notes go to memory/characters/{id}/YYYYMM/YYYYMMDD.md.
// Otherwise, they go to memory/YYYYMM/YYYYMMDD.md.
func (ms *MemoryStore) getTodayFile() string {
	today := time.Now().Format("20060102") // YYYYMMDD
	monthDir := today[:6]                  // YYYYMM

	root := ms.memoryDir
	if charDir := ms.characterMemoryDir(); charDir != "" {
		root = charDir
	}
	filePath := filepath.Join(root, monthDir, today+".md")
	return filePath
}

// ReadLongTerm reads the long-term memory (MEMORY.md).
// Returns empty string if the file doesn't exist.
func (ms *MemoryStore) ReadLongTerm() string {
	if data, err := os.ReadFile(ms.memoryFile); err == nil {
		return string(data)
	}
	return ""
}

// WriteLongTerm writes content to the long-term memory file (MEMORY.md).
func (ms *MemoryStore) WriteLongTerm(content string) error {
	// Use unified atomic write utility with explicit sync for flash storage reliability.
	// Using 0o600 (owner read/write only) for secure default permissions.
	return fileutil.WriteFileAtomic(ms.memoryFile, []byte(content), 0o600)
}

// ReadToday reads today's daily note.
// Returns empty string if the file doesn't exist.
func (ms *MemoryStore) ReadToday() string {
	todayFile := ms.getTodayFile()
	if data, err := os.ReadFile(todayFile); err == nil {
		return string(data)
	}
	return ""
}

// AppendToday appends content to today's daily note.
// If the file doesn't exist, it creates a new file with a date header.
func (ms *MemoryStore) AppendToday(content string) error {
	todayFile := ms.getTodayFile()

	// Ensure month directory exists
	monthDir := filepath.Dir(todayFile)
	if err := os.MkdirAll(monthDir, 0o755); err != nil {
		return err
	}

	var existingContent string
	if data, err := os.ReadFile(todayFile); err == nil {
		existingContent = string(data)
	}

	var newContent string
	if existingContent == "" {
		// Add header for new day
		header := fmt.Sprintf("# %s\n\n", time.Now().Format("2006-01-02"))
		newContent = header + content
	} else {
		// Append to existing content
		newContent = existingContent + "\n" + content
	}

	// Use unified atomic write utility with explicit sync for flash storage reliability.
	return fileutil.WriteFileAtomic(todayFile, []byte(newContent), 0o600)
}

// GetRecentDailyNotes returns daily notes from the last N days.
// Contents are joined with "---" separator.
func (ms *MemoryStore) GetRecentDailyNotes(days int) string {
	var sb strings.Builder
	first := true

	root := ms.memoryDir
	if charDir := ms.characterMemoryDir(); charDir != "" {
		root = charDir
	}

	for i := range days {
		date := time.Now().AddDate(0, 0, -i)
		dateStr := date.Format("20060102") // YYYYMMDD
		monthDir := dateStr[:6]            // YYYYMM
		filePath := filepath.Join(root, monthDir, dateStr+".md")

		if data, err := os.ReadFile(filePath); err == nil {
			if !first {
				sb.WriteString("\n\n---\n\n")
			}
			sb.Write(data)
			first = false
		}
	}

	return sb.String()
}

// GetMemoryContext returns formatted memory context for the agent prompt.
// Includes long-term memory and recent daily notes.
func (ms *MemoryStore) GetMemoryContext() string {
	longTerm := ms.ReadLongTerm()
	recentNotes := ms.GetRecentDailyNotes(3)

	if longTerm == "" && recentNotes == "" {
		return ""
	}

	var sb strings.Builder

	if longTerm != "" {
		sb.WriteString("## Long-term Memory\n\n")
		sb.WriteString(longTerm)
	}

	if recentNotes != "" {
		if longTerm != "" {
			sb.WriteString("\n\n---\n\n")
		}
		sb.WriteString("## Recent Daily Notes\n\n")
		sb.WriteString(recentNotes)
	}

	return sb.String()
}

// MigrateFromDefault copies existing default memory (memory/MEMORY.md) into the
// character-specific path. It does NOT overwrite if the target already exists.
// Returns true if migration happened, false if skipped.
func (ms *MemoryStore) MigrateFromDefault(characterID string) (bool, error) {
	// Read default memory
	defaultPath := filepath.Join(ms.memoryDir, "MEMORY.md")
	defaultData, err := os.ReadFile(defaultPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // no default memory to migrate
		}
		return false, err
	}

	// Build character path
	charDir := filepath.Join(ms.memoryDir, "characters", characterID)
	charFile := filepath.Join(charDir, "MEMORY.md")

	// Check if target already exists
	if _, err := os.Stat(charFile); err == nil {
		return false, nil // already has character memory, skip
	}

	// Ensure directory exists
	if err := os.MkdirAll(charDir, 0o755); err != nil {
		return false, err
	}

	// Copy default memory to character path
	if err := fileutil.WriteFileAtomic(charFile, defaultData, 0o600); err != nil {
		return false, err
	}

	return true, nil
}
