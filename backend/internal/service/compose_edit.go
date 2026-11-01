// Package service: compose_edit.go — rewrites one service of a docker-compose
// document in place. Works on the yaml.v3 node tree so comments, key order,
// quoting of untouched values and the rest of the file survive the edit.
// Disk and path-allowlist concerns stay with the handler.
package service

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrComposeServiceNotFound is returned when the document has no
// `services.<name>` mapping to patch.
var ErrComposeServiceNotFound = errors.New("service: compose service not found")

// ComposeServicePatch carries the editable fields of a compose service.
// A nil field is left untouched; an empty value removes the key.
type ComposeServicePatch struct {
	Image       *string
	Ports       *[]string
	Environment *map[string]string
}

// PatchComposeService applies p to services.<name> and returns the
// rewritten document. Only the edited service is re-encoded; its text is
// spliced into the original bytes so blank lines, comments and the
// formatting of everything else survive (yaml.v3 drops blank lines when it
// writes a whole document). `environment` keeps the style the file already
// uses (mapping or KEY=value list) and its key order; port mappings are
// always double-quoted, as the Compose docs recommend.
func PatchComposeService(src []byte, name string, p ComposeServicePatch) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidComposeYAML, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, ErrComposeServiceNotFound
	}
	root := doc.Content[0]
	services := mappingValue(root, "services")
	if services == nil || services.Kind != yaml.MappingNode {
		return nil, ErrComposeServiceNotFound
	}
	idx := mappingIndex(services, name)
	if idx < 0 || services.Content[idx+1].Kind != yaml.MappingNode {
		return nil, ErrComposeServiceNotFound
	}
	svc := services.Content[idx+1]

	if p.Image != nil {
		if *p.Image == "" {
			deleteKey(svc, "image")
		} else {
			setKey(svc, "image", scalarNode(*p.Image))
		}
	}
	if p.Ports != nil {
		if len(*p.Ports) == 0 {
			deleteKey(svc, "ports")
		} else {
			setKey(svc, "ports", quotedSeqNode(*p.Ports))
		}
	}
	if p.Environment != nil {
		existing := mappingValue(svc, "environment")
		keys := envKeyOrder(existing, *p.Environment)
		switch {
		case len(*p.Environment) == 0:
			deleteKey(svc, "environment")
		case existing != nil && existing.Kind == yaml.SequenceNode:
			setKey(svc, "environment", envSeqNode(keys, *p.Environment))
		default:
			setKey(svc, "environment", envMapNode(keys, *p.Environment))
		}
	}

	if out, ok := spliceService(src, root, services, idx); ok {
		return out, nil
	}
	// Flow-style or single-line service: re-encode the whole document.
	return encodeNode(&doc)
}

// spliceService re-encodes services.Content[idx+1] and replaces the
// service's lines in src with it. The block runs from the line after the
// service key to the line before the next key at the same level (or the
// next top-level key, or EOF), minus trailing blank / comment lines, which
// belong to whatever follows. Returns false when the service is written in
// flow style or on the key's own line.
func spliceService(src []byte, root, services *yaml.Node, idx int) ([]byte, bool) {
	key, svc := services.Content[idx], services.Content[idx+1]
	if svc.Line <= key.Line || svc.Style&yaml.FlowStyle != 0 {
		return nil, false
	}
	lines := strings.Split(string(src), "\n")
	start := key.Line // 0-based index of the first line after the key
	end := len(lines) + 1
	if idx+2 < len(services.Content) {
		end = services.Content[idx+2].Line
	} else {
		for i := 0; i+1 < len(root.Content); i += 2 {
			if l := root.Content[i].Line; l > key.Line && l < end {
				end = l
			}
		}
	}
	for end-1 > start && isBlankOrComment(lines[end-2]) {
		end--
	}
	indent := strings.Repeat(" ", key.Column+1)
	for i := start; i < end-1 && i < len(lines); i++ {
		if l := lines[i]; !isBlankOrComment(l) {
			indent = l[:len(l)-len(strings.TrimLeft(l, " \t"))]
			break
		}
	}
	body, err := encodeNode(svc)
	if err != nil {
		return nil, false
	}
	out := append([]string{}, lines[:start]...)
	for _, l := range strings.Split(strings.TrimRight(string(body), "\n"), "\n") {
		if l == "" {
			out = append(out, "")
		} else {
			out = append(out, indent+l)
		}
	}
	out = append(out, lines[end-1:]...)
	return []byte(strings.Join(out, "\n")), true
}

func isBlankOrComment(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "#")
}

func encodeNode(n *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return nil, fmt.Errorf("service: encode compose: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("service: encode compose: %w", err)
	}
	return buf.Bytes(), nil
}

// mappingIndex returns the index of the key node for key, or -1.
func mappingIndex(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// mappingValue returns the value node for key in a mapping node, or nil.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// setKey replaces the value for key (keeping the key node and its comments)
// or appends the pair when the key is absent.
func setKey(m *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content, scalarNode(key), val)
}

func deleteKey(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

func scalarNode(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

// quotedSeqNode writes each item double-quoted ("8080:80" would otherwise
// be read as a base-60 number by YAML 1.1 parsers).
func quotedSeqNode(items []string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, it := range items {
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: it, Style: yaml.DoubleQuotedStyle})
	}
	return n
}

// envKeyOrder keeps the order the file already lists keys in (sequence
// items "KEY=value" or mapping keys), drops removed ones and appends new
// keys sorted, so an edit reads as a diff of the lines that changed.
func envKeyOrder(existing *yaml.Node, env map[string]string) []string {
	seen := make(map[string]bool, len(env))
	var keys []string
	if existing != nil {
		switch existing.Kind {
		case yaml.SequenceNode:
			for _, item := range existing.Content {
				k := item.Value
				if i := strings.IndexByte(k, '='); i >= 0 {
					k = k[:i]
				}
				if _, ok := env[k]; ok && !seen[k] {
					seen[k] = true
					keys = append(keys, k)
				}
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(existing.Content); i += 2 {
				k := existing.Content[i].Value
				if _, ok := env[k]; ok && !seen[k] {
					seen[k] = true
					keys = append(keys, k)
				}
			}
		}
	}
	var fresh []string
	for k := range env {
		if !seen[k] {
			fresh = append(fresh, k)
		}
	}
	sort.Strings(fresh)
	return append(keys, fresh...)
}

func envSeqNode(keys []string, env map[string]string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, k := range keys {
		n.Content = append(n.Content, scalarNode(k+"="+env[k]))
	}
	return n
}

func envMapNode(keys []string, env map[string]string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, k := range keys {
		n.Content = append(n.Content, scalarNode(k), scalarNode(env[k]))
	}
	return n
}
