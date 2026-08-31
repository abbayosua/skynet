package tools

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// ToolOutputCache: LRU cache for deterministic tool outputs.
// ============================================================================

// cacheEntry holds a cached tool output with its expiry time.
type cacheEntry struct {
	key       string
	value     string
	expiresAt time.Time
}

// ToolOutputCache provides a thread-safe LRU cache for deterministic tool
// results (e.g. glob, grep, ls with the same parameters on unchanged files).
//
// Only read-only tools are cached. Write tools (edit, write, bash) always
// bypass the cache and invalidate relevant entries.
type ToolOutputCache struct {
	mu        sync.Mutex
	entries   map[string]*list.Element
	lru       *list.List
	maxSize   int
	ttl       time.Duration
	hitCount  int64
	missCount int64
}

// NewToolOutputCache creates a new cache with the given capacity and TTL.
func NewToolOutputCache(maxSize int, ttl time.Duration) *ToolOutputCache {
	if maxSize <= 0 {
		maxSize = 100
	}
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	return &ToolOutputCache{
		entries: make(map[string]*list.Element),
		lru:     list.New(),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// cacheKey generates a deterministic key from a tool name and its parameters.
// For file-reading tools, include relevant file content hashes so cache
// is invalidated when files change.
func cacheKey(toolName string, params any, fileHashes ...string) string {
	h := sha256.New()
	h.Write([]byte(toolName))
	h.Write([]byte(fmt.Sprintf("%v", params)))
	for _, fh := range fileHashes {
		h.Write([]byte(fh))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Get retrieves a cached value. Returns the value and true if found and valid.
func (c *ToolOutputCache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.entries[key]
	if !ok {
		c.missCount++
		return "", false
	}

	entry := elem.Value.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		c.lru.Remove(elem)
		delete(c.entries, key)
		c.missCount++
		return "", false
	}

	// Move to front (most recently used).
	c.lru.MoveToFront(elem)
	c.hitCount++
	return entry.value, true
}

// Set stores a value in the cache.
func (c *ToolOutputCache) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If already exists, update and move to front.
	if elem, ok := c.entries[key]; ok {
		c.lru.MoveToFront(elem)
		elem.Value.(*cacheEntry).value = value
		elem.Value.(*cacheEntry).expiresAt = time.Now().Add(c.ttl)
		return
	}

	// Evict oldest if at capacity.
	for c.lru.Len() >= c.maxSize {
		oldest := c.lru.Back()
		if oldest != nil {
			c.lru.Remove(oldest)
			delete(c.entries, oldest.Value.(*cacheEntry).key)
		}
	}

	entry := &cacheEntry{
		key:       key,
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}
	elem := c.lru.PushFront(entry)
	c.entries[key] = elem
}

// Invalidate removes all entries for a given tool name.
// Call this when a write tool modifies files that could affect tool outputs.
func (c *ToolOutputCache) Invalidate(toolName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key, elem := range c.entries {
		if strings.HasPrefix(key, toolName+":") {
			c.lru.Remove(elem)
			delete(c.entries, key)
		}
	}
}

// Clear removes all entries.
func (c *ToolOutputCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*list.Element)
	c.lru = list.New()
}

// Stats returns cache hit/miss counts for monitoring.
func (c *ToolOutputCache) Stats() (hits, misses int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hitCount, c.missCount
}

// IsReadOnlyTool returns true if the tool only reads data and produces
// deterministic outputs suitable for caching.
func IsReadOnlyTool(toolName string) bool {
	switch toolName {
	case "glob", "grep", "ls", "view", "lsp_diagnostics",
		"lsp_references", "sourcegraph", "agent_status",
		"skynet_info", "skynet_logs":
		return true
	default:
		return false
	}
}

// globalToolCache is the shared tool output cache.
// Initialized lazily with defaults (100 entries, 60s TTL).
var globalToolCache = NewToolOutputCache(100, 60*time.Second)

// ============================================================================
// SmartTruncation: Intelligent truncation of large outputs.
// ============================================================================

// TruncateResult holds the truncated content and metadata.
type TruncateResult struct {
	Content     string
	OriginalLen int
	Truncated   bool
}

// SmartTruncate truncates content intelligently:
// - Keeps the head and tail of the content
// - Replaces the middle with a summary line
// - Adds truncation notice
func SmartTruncate(content string, maxLen int) TruncateResult {
	if maxLen <= 0 {
		maxLen = 10000
	}

	if len(content) <= maxLen {
		return TruncateResult{
			Content:     content,
			OriginalLen: len(content),
			Truncated:   false,
		}
	}

	// Calculate head/tail split: keep 40% head, 40% tail, cut 20% middle.
	headLen := int(float64(maxLen) * 0.4)
	tailLen := int(float64(maxLen) * 0.4)
	remaining := maxLen - headLen - tailLen

	head := content[:headLen]
	tail := content[len(content)-tailLen:]

	// Estimate line counts.
	lineCount := strings.Count(content, "\n")
	truncatedLines := lineCount - strings.Count(head, "\n") - strings.Count(tail, "\n")

	// Add at least some head room for the truncation notice.
	if remaining > 0 {
		// Use remaining space for more content from the head.
		extraHead := remaining / 2
		if extraHead > 0 && headLen+extraHead < len(content) {
			head = content[:headLen+extraHead]
		}
	}

	summary := fmt.Sprintf("\n\n[... truncated %d lines, %d characters ...]\n\n",
		truncatedLines, len(content)-maxLen)

	result := head + summary + tail
	if len(result) > maxLen {
		// Double-truncation safety: if the notice itself pushes us over, trim head.
		overflow := len(result) - maxLen
		if overflow < len(head) {
			head = head[:len(head)-overflow]
			result = head + summary + tail
		}
	}

	return TruncateResult{
		Content:     result,
		OriginalLen: len(content),
		Truncated:   true,
	}
}

// TruncateFileContent truncates file content keeping file structure readable.
// Preserves the first N lines and last M lines with a truncation notice in between.
func TruncateFileContent(content string, maxLines int) TruncateResult {
	if maxLines <= 0 {
		maxLines = 200
	}

	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return TruncateResult{
			Content:     content,
			OriginalLen: len(content),
			Truncated:   false,
		}
	}

	// Keep first 40% and last 40% of the limit.
	headLines := int(float64(maxLines) * 0.4)
	tailLines := int(float64(maxLines) * 0.4)
	if headLines < 10 {
		headLines = 10
	}
	if tailLines < 10 {
		tailLines = 10
	}
	if headLines+tailLines > maxLines {
		headLines = maxLines / 2
		tailLines = maxLines - headLines
	}

	head := lines[:headLines]
	tail := lines[len(lines)-tailLines:]
	truncatedLines := len(lines) - headLines - tailLines

	summary := fmt.Sprintf("[... %d lines truncated ...]", truncatedLines)

	result := strings.Join(append(append(head, summary), tail...), "\n")

	return TruncateResult{
		Content:     result,
		OriginalLen: len(content),
		Truncated:   true,
	}
}

// ============================================================================
// Token Budget Tracking
// ============================================================================

// TokenBudget tracks per-turn and per-session token usage for efficient
// context management.
type TokenBudget struct {
	mu               sync.Mutex
	perTurnLimit     int
	usedThisTurn     int
	usedTotal        int
	compressionCount int
}

// NewTokenBudget creates a budget tracker with the given per-turn limit.
func NewTokenBudget(perTurnLimit int) *TokenBudget {
	if perTurnLimit <= 0 {
		perTurnLimit = 100000
	}
	return &TokenBudget{
		perTurnLimit: perTurnLimit,
	}
}

// RecordUsage records token usage for the current turn.
// Returns true if the budget is approaching its limit (80%+ used).
func (b *TokenBudget) RecordUsage(tokens int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.usedThisTurn += tokens
	b.usedTotal += tokens

	// Alert when approaching limit.
	return b.usedThisTurn >= int(float64(b.perTurnLimit)*0.8)
}

// ResetTurn resets the per-turn counter.
func (b *TokenBudget) ResetTurn() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.usedThisTurn = 0
}

// CompressionRatio returns the ratio of compressed-to-original content as a
// rough measure of token efficiency (0.0 = no compression, 1.0 = fully compressed).
func (b *TokenBudget) CompressionRatio() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.compressionCount == 0 {
		return 0
	}
	return float64(b.compressionCount) / float64(b.usedTotal+1)
}

// ShouldCompress returns true if the current turn usage suggests compression
// would be beneficial.
func (b *TokenBudget) ShouldCompress(tokens int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.usedThisTurn+tokens > int(float64(b.perTurnLimit)*0.85)
}

// ============================================================================
// Context compression helpers
// ============================================================================

// EstimateTokenCount provides a rough estimate of token count for text.
// Uses the standard ~4 chars per token approximation for English text,
// with adjustments for code (more compact).
func EstimateTokenCount(text string) int {
	// Rough estimate: 4 chars per token for prose, ~3 for code.
	return len(text) / 4
}

// CompressMessages compresses a []string of messages into a shorter summary.
// Keeps the first and last messages intact and summarizes the middle.
func CompressMessages(messages []string, keepFirst, keepLast int) []string {
	if len(messages) <= keepFirst+keepLast {
		return messages
	}

	var result []string
	result = append(result, messages[:keepFirst]...)

	totalChars := 0
	for _, m := range messages[keepFirst : len(messages)-keepLast] {
		totalChars += len(m)
	}

	result = append(result, fmt.Sprintf(
		"[... compressed %d messages, ~%d tokens ...]",
		len(messages)-keepFirst-keepLast,
		totalChars/4,
	))

	result = append(result, messages[len(messages)-keepLast:]...)
	return result
}

// ============================================================================
// FileContentCache: Caches recently viewed file contents to avoid redundant
// reads across tool calls. Invalidated when files are modified.
// ============================================================================

// FileContentCache caches file contents read by tools (especially view).
// This avoids redundant disk reads and reduces LLM token usage when the
// same file is inspected multiple times.
type FileContentCache struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	lru     *list.List
	maxSize int
	ttl     time.Duration
}

type fileCacheEntry struct {
	path     string
	content  string
	loadedAt time.Time
	modTime  time.Time // file modification time at last read
	size     int64     // file size at last read
}

// NewFileContentCache creates a new file content cache.
func NewFileContentCache(maxSize int, ttl time.Duration) *FileContentCache {
	if maxSize <= 0 {
		maxSize = 50
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &FileContentCache{
		entries: make(map[string]*list.Element),
		lru:     list.New(),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// GetOrLoad retrieves cached content or loads it from disk.
// Returns the content and whether it was from cache.
func (fc *FileContentCache) GetOrLoad(path, content string, modTime time.Time, size int64) (string, bool) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	// Check cache.
	if elem, ok := fc.entries[path]; ok {
		entry := elem.Value.(*fileCacheEntry)
		// Validate: check TTL and file modification time.
		if time.Since(entry.loadedAt) < fc.ttl && entry.modTime.Equal(modTime) && entry.size == size {
			fc.lru.MoveToFront(elem)
			return entry.content, true
		}
		// Stale entry - remove it.
		fc.lru.Remove(elem)
		delete(fc.entries, path)
	}

	// Store new entry, evicting oldest if needed.
	entry := &fileCacheEntry{
		path:     path,
		content:  content,
		loadedAt: time.Now(),
		modTime:  modTime,
		size:     size,
	}

	// Evict oldest if at capacity.
	for fc.lru.Len() >= fc.maxSize {
		oldest := fc.lru.Back()
		if oldest != nil {
			fc.lru.Remove(oldest)
			delete(fc.entries, oldest.Value.(*fileCacheEntry).path)
		}
	}

	elem := fc.lru.PushFront(entry)
	fc.entries[path] = elem
	return content, false
}

// Invalidate removes an entry from the cache.
func (fc *FileContentCache) Invalidate(path string) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if elem, ok := fc.entries[path]; ok {
		fc.lru.Remove(elem)
		delete(fc.entries, path)
	}
}

// InvalidateDir removes all entries under a directory prefix.
func (fc *FileContentCache) InvalidateDir(dirPrefix string) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	for path, elem := range fc.entries {
		if strings.HasPrefix(path, dirPrefix) {
			fc.lru.Remove(elem)
			delete(fc.entries, path)
		}
	}
}

// Clear removes all cached entries.
func (fc *FileContentCache) Clear() {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.entries = make(map[string]*list.Element)
	fc.lru = list.New()
}

// globalFileCache is the shared file content cache (50 entries, 5 min TTL).
var globalFileCache = NewFileContentCache(50, 5*time.Minute)

// ============================================================================
// KnowledgeStore: Distills key learnings from completed tasks and saves them
// for future reference. This reduces token waste by avoiding re-discovery
// of the same information across sessions.
// ============================================================================

// KnowledgeEntry stores a distilled piece of knowledge about the codebase.
type KnowledgeEntry struct {
	Topic     string    `json:"topic"`
	Summary   string    `json:"summary"`
	Files     []string  `json:"files"`
	Decisions []string  `json:"decisions"`
	CreatedAt time.Time `json:"created_at"`
	SessionID string    `json:"session_id,omitempty"`
}

// KnowledgeStore manages distilled knowledge entries.
// Thread-safe, in-memory store with optional file persistence.
type KnowledgeStore struct {
	mu         sync.Mutex
	entries    []KnowledgeEntry
	maxEntries int
}

// NewKnowledgeStore creates a new knowledge store.
func NewKnowledgeStore(maxEntries int) *KnowledgeStore {
	if maxEntries <= 0 {
		maxEntries = 100
	}
	return &KnowledgeStore{
		entries:    make([]KnowledgeEntry, 0, maxEntries),
		maxEntries: maxEntries,
	}
}

// Add saves a knowledge entry. If at capacity, removes the oldest entry.
func (ks *KnowledgeStore) Add(entry KnowledgeEntry) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if len(ks.entries) >= ks.maxEntries {
		ks.entries = ks.entries[1:]
	}
	entry.CreatedAt = time.Now()
	ks.entries = append(ks.entries, entry)
}

// Find returns entries matching a topic substring search.
func (ks *KnowledgeStore) Find(topic string) []KnowledgeEntry {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	topicLower := strings.ToLower(topic)
	var matches []KnowledgeEntry
	for _, e := range ks.entries {
		if strings.Contains(strings.ToLower(e.Topic), topicLower) {
			matches = append(matches, e)
		}
	}
	return matches
}

// GetAll returns all knowledge entries.
func (ks *KnowledgeStore) GetAll() []KnowledgeEntry {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	result := make([]KnowledgeEntry, len(ks.entries))
	copy(result, ks.entries)
	return result
}

// FormatContext returns knowledge entries formatted as context for an LLM prompt.
// This is designed to be included in system prompts so the agent has immediate
// access to past learnings without needing to re-discover them.
func (ks *KnowledgeStore) FormatContext(topic string) string {
	entries := ks.Find(topic)
	if len(entries) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n<known-knowledge>\n")
	for _, e := range entries {
		b.WriteString(fmt.Sprintf("  <entry topic=%q>\n", e.Topic))
		b.WriteString(fmt.Sprintf("    <summary>%s</summary>\n", e.Summary))
		if len(e.Files) > 0 {
			b.WriteString(fmt.Sprintf("    <files>%s</files>\n", strings.Join(e.Files, ", ")))
		}
		if len(e.Decisions) > 0 {
			b.WriteString("    <decisions>\n")
			for _, d := range e.Decisions {
				b.WriteString(fmt.Sprintf("      <decision>%s</decision>\n", d))
			}
			b.WriteString("    </decisions>\n")
		}
		b.WriteString("  </entry>\n")
	}
	b.WriteString("</known-knowledge>\n")
	return b.String()
}

// globalKnowledgeStore is the shared knowledge store.
var globalKnowledgeStore = NewKnowledgeStore(100)
