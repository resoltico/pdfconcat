// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"

	pdffont "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/font"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// serialize writes a reachable, unencrypted graph with canonical object
// numbering. Discovery validates objects and freezes PDF 2.0 font metrics
// before any write, without retaining a second graph of remapped objects.
func serialize(c context.Context, s *model.Context, w io.Writer) error {
	if s.Encrypt != nil {
		return errors.New("cannot serialize encrypted output")
	}
	e := encoder{ctx: c, source: s, ids: make(map[types.IndirectRef]int)}
	e.reference(*s.Root)
	if s.Info != nil {
		e.reference(*s.Info)
	}
	next := 0
	for next < len(e.order) {
		ref := e.order[next]
		next++
		value, err := s.Dereference(ref)
		if err != nil {
			return err
		}
		if value == nil {
			return fmt.Errorf("dangling output object %d", ref.ObjectNumber.Value())
		}
		if err := e.object(value, 0); err != nil {
			return err
		}
	}

	out := &generatedOutput{writer: w, context: c}
	e.sink = bufio.NewWriterSize(out, 64<<10)
	e.text("%PDF-" + s.XRefTable.Version().String() + "\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int64, len(e.order))
	for i, ref := range e.order {
		if err := c.Err(); err != nil {
			return err
		}
		offsets[i] = out.n + int64(e.sink.Buffered())
		e.integer(int64(i + 1))
		e.text(" 0 obj\n")
		value, err := s.Dereference(ref)
		if err != nil {
			return err
		}
		if err := e.object(value, 0); err != nil {
			return err
		}
		e.text("\nendobj\n")
		if e.err != nil {
			return e.err
		}
	}
	start := out.n + int64(e.sink.Buffered())
	e.text("xref\n0 ")
	e.integer(int64(len(offsets) + 1))
	e.text("\n0000000000 65535 f \n")
	for _, offset := range offsets {
		if offset > 9999999999 {
			return errors.New("output exceeds classic xref offset range")
		}
		var scratch [20]byte
		value := strconv.AppendInt(scratch[:0], offset, 10)
		e.text("0000000000"[:10-len(value)])
		e.bytes(value)
		e.text(" 00000 n \n")
	}
	e.text("trailer\n<<")
	if s.Info != nil {
		e.text("/Info ")
		e.reference(*s.Info)
	}
	e.text("/Root ")
	e.reference(*s.Root)
	e.text("/Size ")
	e.integer(int64(len(offsets) + 1))
	e.text(">>\nstartxref\n")
	e.integer(start)
	e.text("\n%%EOF\n")
	if e.err != nil {
		return e.err
	}
	if err := c.Err(); err != nil {
		return err
	}
	return e.sink.Flush()
}

type encoder struct {
	ctx    context.Context
	source *model.Context
	ids    map[types.IndirectRef]int
	order  []types.IndirectRef
	sink   *bufio.Writer
	err    error
}

func (e *encoder) text(s string) {
	if e.sink != nil && e.err == nil {
		_, e.err = e.sink.WriteString(s)
	}
}

func (e *encoder) bytes(b []byte) {
	if e.sink != nil && e.err == nil {
		_, e.err = e.sink.Write(b)
	}
}

func (e *encoder) integer(n int64) {
	if e.sink != nil {
		var scratch [20]byte
		e.bytes(strconv.AppendInt(scratch[:0], n, 10))
	}
}

func (e *encoder) reference(ref types.IndirectRef) {
	n := e.ids[ref]
	if e.sink == nil {
		if n == 0 {
			e.order = append(e.order, ref)
			e.ids[ref] = len(e.order)
		}
		return
	}
	if n == 0 && e.err == nil {
		e.err = fmt.Errorf("unindexed output reference %d", ref.ObjectNumber.Value())
		return
	}
	e.integer(int64(n))
	e.text(" 0 R")
}

// Dictionaries put spaces before atoms whose first byte is not a delimiter.
// Arrays retain the established byte format, including spaces before strings.
func (e *encoder) object(value types.Object, depth int) error {
	if err := e.ctx.Err(); err != nil {
		return err
	}
	if depth > 256 {
		return errors.New("output direct-object nesting exceeds 256")
	}
	switch v := value.(type) {
	case nil:
		e.text("null")
	case types.IndirectRef:
		e.reference(v)
	case types.Integer:
		e.integer(int64(v))
	case types.Float:
		if math.IsNaN(v.Value()) || math.IsInf(v.Value(), 0) {
			return errors.New("nonfinite output PDF scalar")
		}
		if e.sink != nil {
			e.text(v.PDFString())
		}
	case types.Dict:
		return e.dict(v, depth, nil)
	case types.StreamDict:
		if depth != 0 {
			return errors.New("output stream must be an indirect object")
		}
		n := len(v.Raw)
		if err := e.dict(v.Dict, depth+1, &n); err != nil {
			return err
		}
		e.text("\nstream\n")
		e.bytes(v.Raw)
		e.text("\nendstream")
	case types.Array:
		e.text("[")
		for i, entry := range v {
			if i > 0 {
				switch entry.(type) {
				case types.Dict, types.Array, types.Name:
				default:
					e.text(" ")
				}
			}
			if err := e.object(entry, depth+1); err != nil {
				return err
			}
		}
		e.text("]")
	case types.Name, types.Boolean, types.StringLiteral, types.HexLiteral:
		if e.sink != nil {
			e.text(v.PDFString())
		}
	default:
		return fmt.Errorf("unsupported output object %T", value)
	}
	return e.err
}

func (e *encoder) dict(dict types.Dict, depth int, streamLength *int) error {
	if depth > 256 {
		return errors.New("output direct-object nesting exceeds 256")
	}
	if e.sink == nil && e.source.PDF20() {
		typ, err := nameValue(e.source, dict["Type"])
		if err != nil {
			return err
		}
		if typ == "Font" {
			if _, err := pdffont.EnsureCoreFontMetrics(e.source.XRefTable, dict); err != nil {
				return err
			}
		}
	}
	var inline [12]string
	keys := inline[:0]
	for key := range dict {
		keys = append(keys, key)
	}
	if _, exists := dict["Length"]; !exists && streamLength != nil {
		keys = append(keys, "Length")
	}
	slices.Sort(keys)
	e.text("<<")
	for _, key := range keys {
		value := dict[key]
		if key == "Length" && streamLength != nil {
			value = types.Integer(*streamLength)
		}
		if e.sink != nil {
			e.text("/")
			e.text(types.EncodeName(key))
			switch value.(type) {
			case types.Dict, types.Array, types.Name, types.StringLiteral, types.HexLiteral:
			default:
				e.text(" ")
			}
		}
		if err := e.object(value, depth+1); err != nil {
			return err
		}
	}
	e.text(">>")
	return e.err
}
