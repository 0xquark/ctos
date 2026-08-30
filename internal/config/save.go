package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// SaveLayout rewrites a dashboard's "rows:" and "bar:" keys in place, leaving
// everything else in the file — comments, key order, widget settings —
// untouched.
//
// The file is re-read and re-parsed rather than re-serialised from the loaded
// Dashboard, for two reasons: the loaded copy has already had ${VAR} and ~
// expanded, and writing those resolved values back would bake a machine's
// secrets and home directory into a file meant to be shared.
func SaveLayout(path string, rows [][]string, bar Bar) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("%s: not a dashboard file", path)
	}
	root := doc.Content[0]

	setKey(root, "rows", rowsNode(rows))
	if !bar.Empty() {
		setKey(root, "bar", barNode(bar))
	}

	// Encode the document node, not the mapping: a comment block at the top
	// of the file attaches to the document, and encoding the mapping alone
	// would silently drop it.
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}

	return writeAtomic(path, buf.Bytes())
}

// setKey replaces one top-level key's value, appending it if the file does not
// have the key yet.
//
// Comments attached to the old value are carried over, so the note explaining
// a layout survives the layout being changed.
func setKey(root *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != key {
			continue
		}
		old := root.Content[i+1]
		value.HeadComment = old.HeadComment
		value.FootComment = old.FootComment
		root.Content[i+1] = value
		return
	}
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}

// barNode builds the YAML for a bar.
//
// The list form is kept where it still says everything: "bar: [vitals]" is a
// top bar with nothing at its trailing end, and a save should not expand that
// into three lines to state a default. Anything else is written as a mapping,
// with the group keys the bar's orientation actually takes.
func barNode(b Bar) *yaml.Node {
	if b.Position == BarTop && len(b.End) == 0 {
		return namesNode(b.Start)
	}

	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	put := func(key string, value *yaml.Node) {
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
	}

	put("position", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(b.Position)})
	if b.Position.Vertical() && b.Width > 0 {
		put("width", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(b.Width)})
	}
	startKey, endKey := b.Position.groupKeys()
	if len(b.Start) > 0 {
		put(startKey, namesNode(b.Start))
	}
	if len(b.End) > 0 {
		put(endKey, namesNode(b.End))
	}
	return m
}

// namesNode is a flow-style list of widget names, so it reads as
// "[clock, notes]" rather than a nested block.
func namesNode(names []string) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	for _, name := range names {
		seq.Content = append(seq.Content, &yaml.Node{
			Kind: yaml.ScalarNode, Tag: "!!str", Value: name,
		})
	}
	return seq
}

// rowsNode builds the YAML for a layout, with each row in flow style so it
// reads as "- [clock, notes]" rather than a nested block.
func rowsNode(rows [][]string) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, row := range rows {
		seq.Content = append(seq.Content, namesNode(row))
	}
	return seq
}

// writeAtomic replaces a file via a temporary file and a rename, so an
// interrupted save cannot leave a half-written dashboard behind.
func writeAtomic(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temp file next to %s: %w", path, err)
	}
	tmpName := tmp.Name()
	// No-op once the rename succeeds; nothing useful to do if it fails.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// SaveTheme rewrites "theme: name:" in a config file, leaving everything else
// — comments, key order, every other setting — untouched.
//
// It also removes "theme: accent:". A theme owns its accent, so an override
// left over from a previous one would tint the new palette in the old one's
// colour, which is the whole look of every theme reduced to one of them. The
// key stays supported for someone who writes it by hand and stays put; asking
// for a different theme is what discards it.
//
// A config file that does not exist yet is created holding just the theme:
// ctOS runs happily without one, so choosing a theme should not require
// running `ctos init` first.
func SaveTheme(path, name string) error {
	var doc yaml.Node

	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
		}
		doc = yaml.Node{
			Kind:    yaml.DocumentNode,
			Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}},
		}
	case err != nil:
		return fmt.Errorf("read %s: %w", path, err)
	default:
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if len(doc.Content) == 0 {
			// An empty or comment-only file parses to no content.
			doc.Kind = yaml.DocumentNode
			doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
		}
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: not a settings file", path)
	}

	// "theme:" may be absent, or present but empty ("theme:" with nothing
	// under it), which parses as a null scalar rather than a mapping.
	block := findKey(root, "theme")
	if block == nil || block.Kind != yaml.MappingNode {
		block = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setKey(root, "theme", block)
	}
	setKey(block, "name", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name})
	deleteKey(block, "accent")

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}

	return writeAtomic(path, buf.Bytes())
}

// deleteKey removes one key and its value from a mapping. A comment attached
// to the key goes with it: it was explaining a setting that is no longer there.
func deleteKey(root *yaml.Node, key string) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			root.Content = append(root.Content[:i], root.Content[i+2:]...)
			return
		}
	}
}

// findKey returns a mapping's value for one key, or nil.
func findKey(root *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			return root.Content[i+1]
		}
	}
	return nil
}
