// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

// Command contractgen regenerates checked-in semantic schema/reference fragments.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/resoltico/pdfconcat/internal/cli"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

func schema(data []byte, values map[string]any) ([]byte, error) {
	var document map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	for path, value := range values {
		parts := strings.Split(path, "/")
		node := any(document)
		for _, part := range parts[:len(parts)-1] {
			switch n := node.(type) {
			case map[string]any:
				node = n[part]
			case []any:
				var i int
				if _, err := fmt.Sscan(part, &i); err != nil {
					return nil, err
				}
				node = n[i]
			}
		}
		m, ok := node.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("missing schema owner path %s", path)
		}
		m[parts[len(parts)-1]] = value
	}
	return json.MarshalIndent(document, "", "  ")
}
func generated(root string) (map[string][]byte, error) {
	products := map[string][]byte{}
	path := "internal/plan/plan.schema.json"
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return nil, err
	}
	values := map[string]any{
		"$defs/page/properties/text/properties/anchor/enum":   packet.Anchor("").Values(),
		"$defs/page/properties/text/properties/align/enum":    packet.Align("").Values(),
		"$defs/page/properties/text/properties/wrap/enum":     packet.Wrap("").Values(),
		"$defs/page/properties/text/properties/overflow/enum": packet.Overflow("").Values(),
	}
	numbers := packet.DefaultPageNumbers()
	values["properties/page_numbers/properties/mode/default"] = numbers.Mode
	values["properties/page_numbers/properties/position/default"] = numbers.Position
	values["properties/page_numbers/properties/size/default"] = numbers.Size
	values["properties/page_numbers/properties/start/default"] = numbers.Start
	values["properties/page_numbers/properties/kinds/default"] = numbers.Kinds
	values["properties/page_numbers/properties/start/maximum"] = packet.MaxPageIndex
	values["properties/timestamp/maximum"] = packet.MaxTimestamp
	values["$defs/items/items/oneOf/0/properties/pages/maxItems"] = packet.MaxSelectionPairs
	values["$defs/items/items/oneOf/1/properties/count/maximum"] = packet.MaxGeneratedPages
	for _, field := range []string{"title", "author", "subject", "language"} {
		values["properties/metadata/properties/"+field+"/maxLength"] = packet.MaxAuthoredFieldRunes
	}
	values["$defs/title/maxLength"] = packet.MaxAuthoredFieldRunes
	values["$defs/page/properties/text/properties/value/maxLength"] = packet.MaxAuthoredFieldRunes
	for _, kind := range []string{"length", "nonnegativeLength", "positiveLength"} {
		values["$defs/"+kind+"/oneOf/0/maximum"] = packet.MaxSide
	}
	products[path], err = schema(data, values)
	if err != nil {
		return nil, err
	}
	products[path] = append(products[path], '\n')
	path = "docs/CLI.md"
	data, err = os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return nil, err
	}
	const start = "<!-- BEGIN GENERATED DIAGNOSTICS -->"
	const end = "<!-- END GENERATED DIAGNOSTICS -->"
	before, rest, ok := strings.Cut(string(data), start)
	if !ok {
		return nil, errors.New("missing diagnostics marker")
	}
	_, after, ok := strings.Cut(rest, end)
	if !ok {
		return nil, errors.New("missing diagnostics end")
	}
	var table strings.Builder
	table.WriteString("\n| Code | Severity | Class | Meaning |\n| --- | --- | --- | --- |\n")
	for _, entry := range append(diag.Entries(), cli.InvocationEntries()...) {
		d := entry.Definition
		class := string(d.Class)
		if class == "" {
			class = "—"
		}
		fmt.Fprintf(&table, "| `%s` | %s | %s | %s |\n", entry.Code, d.Severity, class, strings.ReplaceAll(d.Description, "|", " / "))
	}
	products[path] = []byte(before + start + table.String() + end + after)
	return products, nil
}
func run(root string, check bool) error {
	products, err := generated(root)
	if err != nil {
		return err
	}
	for path, data := range products {
		full := filepath.Join(root, path)
		if check {
			old, err := os.ReadFile(full)
			if err != nil {
				return err
			}
			if !bytes.Equal(old, data) {
				return fmt.Errorf("stale generated artifact: %s; run go generate ./internal/plan", path)
			}
		} else if err := os.WriteFile(full, data, 0644); err != nil {
			return err
		}
	}
	return nil
}
func main() {
	root := flag.String("root", ".", "repository root")
	check := flag.Bool("check", false, "refuse stale artifacts")
	flag.Parse()
	if err := run(*root, *check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
