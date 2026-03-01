package gui

import (
	"path/filepath"
	"strings"
	"sync"
)

// NoteIndex tracks wikilink relationships between notes.
type NoteIndex struct {
	mu sync.RWMutex
	// forwardLinks: source → list of targets
	forwardLinks map[string][]string
	// backLinks: target → list of sources
	backLinks map[string][]string
}

// NewNoteIndex creates a new empty index.
func NewNoteIndex() *NoteIndex {
	return &NoteIndex{
		forwardLinks: make(map[string][]string),
		backLinks:    make(map[string][]string),
	}
}

// noteKey creates a unique key for a note within a folder.
func noteKey(folderID, relPath string) string {
	return folderID + "::" + filepath.ToSlash(relPath)
}

// noteKeyToDisplay extracts just the filename without extension.
func noteKeyToDisplay(key string) string {
	parts := strings.SplitN(key, "::", 2)
	if len(parts) < 2 {
		return key
	}
	name := filepath.Base(parts[1])
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// IndexNote indexes a note, extracting its wikilinks and updating the graph.
func (idx *NoteIndex) IndexNote(folderID, relPath string, content []byte) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	srcKey := noteKey(folderID, relPath)
	srcDir := filepath.ToSlash(filepath.Dir(relPath))

	// Remove old forward links from backlinks
	if oldTargets, ok := idx.forwardLinks[srcKey]; ok {
		for _, t := range oldTargets {
			idx.removeBacklink(t, srcKey)
		}
	}

	// Extract new links
	wikilinks := ExtractWikiLinks(content)

	var targets []string
	for _, link := range wikilinks {
		// Resolve link relative to the note's directory
		resolved := link
		if !strings.HasSuffix(resolved, ".md") {
			resolved += ".md"
		}
		// If the link doesn't contain a path separator, it's relative to the same dir
		if !strings.Contains(link, "/") && srcDir != "." && srcDir != "" {
			resolved = srcDir + "/" + resolved
		}
		targetKey := noteKey(folderID, resolved)
		targets = append(targets, targetKey)

		// Add backlink
		idx.backLinks[targetKey] = appendUnique(idx.backLinks[targetKey], srcKey)
	}

	idx.forwardLinks[srcKey] = targets
}

// RemoveNote removes a note from the index.
func (idx *NoteIndex) RemoveNote(folderID, relPath string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	srcKey := noteKey(folderID, relPath)

	// Remove forward links from backlinks
	if targets, ok := idx.forwardLinks[srcKey]; ok {
		for _, t := range targets {
			idx.removeBacklink(t, srcKey)
		}
	}
	delete(idx.forwardLinks, srcKey)

	// Remove any remaining backlinks pointing to this note
	delete(idx.backLinks, srcKey)
}

// GetBacklinks returns notes that link TO the given note.
func (idx *NoteIndex) GetBacklinks(folderID, relPath string) []BacklinkInfo {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	key := noteKey(folderID, relPath)
	sources := idx.backLinks[key]

	var result []BacklinkInfo
	for _, src := range sources {
		parts := strings.SplitN(src, "::", 2)
		if len(parts) == 2 {
			result = append(result, BacklinkInfo{
				FolderID: parts[0],
				Path:     parts[1],
				Name:     noteKeyToDisplay(src),
			})
		}
	}
	return result
}

// GraphData represents the full note graph for visualization.
type GraphData struct {
	Nodes []GraphNode `json:"nodes"`
	Links []GraphLink `json:"links"`
}

// GraphNode represents a note in the graph.
type GraphNode struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	FolderID string `json:"folder_id"`
	HasFile  bool   `json:"has_file"`
}

// GraphLink represents a link between two notes.
type GraphLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// BacklinkInfo describes a note that links to another.
type BacklinkInfo struct {
	FolderID string `json:"folder_id"`
	Path     string `json:"path"`
	Name     string `json:"name"`
}

// GetGraph returns the full graph data for a folder (or all folders if folderID is empty).
func (idx *NoteIndex) GetGraph(folderID string) GraphData {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	nodeSet := make(map[string]bool)
	var links []GraphLink

	for src, targets := range idx.forwardLinks {
		// Filter by folder if specified
		if folderID != "" && !strings.HasPrefix(src, folderID+"::") {
			continue
		}

		nodeSet[src] = true
		for _, t := range targets {
			nodeSet[t] = true
			links = append(links, GraphLink{
				Source: src,
				Target: t,
			})
		}
	}

	// Also include nodes that have backlinks but no forward links
	for target := range idx.backLinks {
		if folderID != "" && !strings.HasPrefix(target, folderID+"::") {
			continue
		}
		nodeSet[target] = true
	}

	var nodes []GraphNode
	for key := range nodeSet {
		parts := strings.SplitN(key, "::", 2)
		fID := ""
		path := key
		if len(parts) == 2 {
			fID = parts[0]
			path = parts[1]
		}

		// A node "has file" if it has forward links (meaning we indexed it)
		_, hasFile := idx.forwardLinks[key]

		nodes = append(nodes, GraphNode{
			ID:       key,
			Name:     noteKeyToDisplay(key),
			Path:     path,
			FolderID: fID,
			HasFile:  hasFile,
		})
	}

	return GraphData{Nodes: nodes, Links: links}
}

// ── Helpers ──

func (idx *NoteIndex) removeBacklink(targetKey, srcKey string) {
	bl := idx.backLinks[targetKey]
	for i, v := range bl {
		if v == srcKey {
			idx.backLinks[targetKey] = append(bl[:i], bl[i+1:]...)
			break
		}
	}
	if len(idx.backLinks[targetKey]) == 0 {
		delete(idx.backLinks, targetKey)
	}
}

func appendUnique(slice []string, item string) []string {
	for _, v := range slice {
		if v == item {
			return slice
		}
	}
	return append(slice, item)
}
