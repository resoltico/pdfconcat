// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package plan

import (
	"bytes"
	"slices"
	"strconv"
	"sync"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

type (
	// document is the retained original bytes of a plan plus the compact node arena of its items. It is the
	// assembly.Source of a decoded Job: it turns an Origin into a pointer, a line, and a column on demand.
	document struct {
		name   string
		data   []byte // every byte read so far, byte order mark included
		nodes  []node // nodes[0] is the root object
		blocks []lineBlock
		// lastRef is the Ref of the most recently added node; the root is Ref 0.
		lastRef assembly.Ref

		indexOnce sync.Once
		bomLen    int
	}

	// node is one item (or the root) in the arena; the JSON pointer is derived by walking parents, never stored.
	node struct {
		parent assembly.Ref
		index  int32 // position within the parent's "items" array
	}

	// lineBlock records the newlines before one block of the data.
	lineBlock struct {
		linesBefore int   // newlines in data before the block
		lastNewline int64 // offset of the last newline before the block, or -1
	}
)

const (
	lineBlockSize = 4096
	// pointerSlack is the room reserved for the "/items/N" steps of a typical pointer.
	pointerSlack = 16
	decimalRadix = 10
)

// Name returns the plan's source name.
func (d *document) Name() string { return d.name }

// Pointer returns the JSON pointer of a node, with member appended, such as "/items/42/items/3/count".
func (d *document) Pointer(ref assembly.Ref, member string) string {
	var steps []int32

	for ref != 0 {
		current := d.nodes[ref]
		steps = append(steps, current.index)
		ref = current.parent
	}

	pointer := make([]byte, 0, pointerSlack+len(member))

	for _, step := range slices.Backward(steps) {
		pointer = append(pointer, "/items/"...)
		pointer = strconv.AppendInt(pointer, int64(step), decimalRadix)
	}

	return string(append(pointer, member...))
}

// Position returns the 1-based line and byte column of an absolute offset. The first call builds a sparse
// newline index in one pass; each call after that costs one block. Columns count bytes from the line
// start, excluding a leading byte order mark.
func (d *document) Position(offset int64) (int, int) {
	d.indexOnce.Do(d.buildIndex)

	if len(d.blocks) == 0 {
		return 1, 1
	}

	offset = max(0, min(offset, int64(len(d.data))))
	block := int(min(offset/lineBlockSize, int64(len(d.blocks)-1)))
	start := int64(block * lineBlockSize)
	prefix := d.data[start:offset]
	line := d.blocks[block].linesBefore + 1 + bytes.Count(prefix, []byte{'\n'})
	lineStart := d.blocks[block].lastNewline + 1

	if last := bytes.LastIndexByte(prefix, '\n'); last >= 0 {
		lineStart = start + int64(last) + 1
	}

	if line == 1 {
		lineStart = int64(d.bomLen)
	}

	return line, int(max(offset-lineStart, 0)) + 1
}

func (d *document) buildIndex() {
	count := (len(d.data) + lineBlockSize - 1) / lineBlockSize
	d.blocks = make([]lineBlock, count)
	lines, last := 0, int64(-1)

	for block := range count {
		d.blocks[block] = lineBlock{linesBefore: lines, lastNewline: last}
		end := min((block+1)*lineBlockSize, len(d.data))
		text := d.data[block*lineBlockSize : end]
		lines += bytes.Count(text, []byte{'\n'})

		if index := bytes.LastIndexByte(text, '\n'); index >= 0 {
			last = int64(block*lineBlockSize + index)
		}
	}
}

// addItem appends an item node and returns its Ref. Refs count nodes in reading order, and the decoder
// charges every node against assembly.MaxNodes first, so the count never approaches the range of a Ref.
func (d *document) addItem(parent assembly.Ref, index int32) assembly.Ref {
	d.nodes = append(d.nodes, node{parent: parent, index: index})
	d.lastRef++

	return d.lastRef
}
