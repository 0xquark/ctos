package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// SaveRows rewrites a dashboard's "rows:" key in place, leaving everything else
// in the file — comments, key order, widget settings — untouched.
//
// The file is re-read and re-parsed rather than re-serialised from the loaded
// Dashboard, for two reasons: the loaded copy has already had ${VAR} and ~
// expanded, and writing those resolved values back would bake a machine's
// secrets and home directory into a file meant to be shared.
func SaveRows(path string, rows [][]string) error {
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

	seq := rowsNode(rows)

	var replaced bool
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "rows" {
			continue
		}
		// Carry over comments attached to the old value so an explanatory
		// note above the layout survives a save.
		old := root.Content[i+1]
		seq.HeadComment = old.HeadComment
		seq.FootComment = old.FootComment
		root.Content[i+1] = seq
		replaced = true
		break
	}

	if !replaced {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "rows"},
			seq,
		)
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

// rowsNode builds the YAML for a layout, with each row in flow style so it
// reads as "- [clock, notes]" rather than a nested block.
func rowsNode(rows [][]string) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, row := range rows {
		inner := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
		for _, name := range row {
			inner.Content = append(inner.Content, &yaml.Node{
				Kind: yaml.ScalarNode, Tag: "!!str", Value: name,
			})
		}
		seq.Content = append(seq.Content, inner)
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
	defer os.Remove(tmpName) // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
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
