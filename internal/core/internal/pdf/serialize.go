// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	pdffont "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/font"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// serialize writes the reachable, unencrypted object graph with canonical object
// numbering. It uses public backend objects, but deliberately does not invoke the
// backend writer: that writer invents dates and changes trailer IDs. Ordinary
// xref tables avoid object-stream allocation and compression-order dependencies.
func serialize(c context.Context, s *model.Context, w io.Writer) error {
	if s.Encrypt != nil {
		return errors.New("cannot serialize encrypted output")
	}
	type identity struct{ number, generation int }
	ids := map[identity]int{}
	var objects []types.Object
	var pending []types.IndirectRef
	var convert func(types.Object, int) (types.Object, error)
	convert = func(o types.Object, depth int) (types.Object, error) {
		if err := c.Err(); err != nil {
			return nil, err
		}
		if depth > 256 {
			return nil, errors.New("output direct-object nesting exceeds 256")
		}
		switch v := o.(type) {
		case types.IndirectRef:
			key := identity{v.ObjectNumber.Value(), v.GenerationNumber.Value()}
			n, found := ids[key]
			if !found {
				n = len(objects) + 1
				ids[key] = n
				objects = append(objects, nil)
				pending = append(pending, v)
			}
			return *types.NewIndirectRef(n, 0), nil
		case types.Dict:
			if s.PDF20() {
				if typ, e := nameValue(s, v["Type"]); e != nil {
					return nil, e
				} else if typ == "Font" {
					if _, e = pdffont.EnsureCoreFontMetrics(s.XRefTable, v); e != nil {
						return nil, e
					}
				}
			}
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			d := types.Dict{}
			for _, k := range keys {
				x, err := convert(v[k], depth+1)
				if err != nil {
					return nil, err
				}
				d[k] = x
			}
			return d, nil
		case types.StreamDict:
			d := clonedDirectDict(v.Dict)
			d["Length"] = types.Integer(len(v.Raw))
			x, err := convert(d, depth+1)
			if err != nil {
				return nil, err
			}
			dict, ok := x.(types.Dict)
			if !ok {
				return nil, errors.New("invalid converted stream dictionary")
			}
			v.Dict = dict
			return v, nil
		case types.Array:
			a := make(types.Array, len(v))
			for i, x := range v {
				y, err := convert(x, depth+1)
				if err != nil {
					return nil, err
				}
				a[i] = y
			}
			return a, nil
		case nil, types.Name, types.Integer, types.Float, types.Boolean, types.StringLiteral, types.HexLiteral:
			return o, nil
		default:
			return nil, fmt.Errorf("unsupported output object %T", o)
		}
	}
	root, err := convert(*s.Root, 0)
	if err != nil {
		return err
	}
	trailer := types.Dict{"Root": root}
	if s.Info != nil {
		info, e := convert(*s.Info, 0)
		if e != nil {
			return e
		}
		trailer["Info"] = info
	}
	// Queue indirect objects so long legal reference chains cannot exhaust the
	// call stack. Array order and sorted dictionary keys determine allocation.
	for i := 0; i < len(pending); {
		value, e := s.Dereference(pending[i])
		if e != nil {
			return e
		}
		if value == nil {
			return fmt.Errorf("dangling output object %d", pending[i].ObjectNumber.Value())
		}
		objects[i], e = convert(value, 0)
		if e != nil {
			return e
		}
		i++
	}
	trailer["Size"] = types.Integer(len(objects) + 1)
	out := &generatedOutput{writer: w, context: c}
	write := func(text string) error { _, e := io.WriteString(out, text); return e }
	if err = write("%PDF-" + s.XRefTable.Version().String() + "\n%\xe2\xe3\xcf\xd3\n"); err != nil {
		return err
	}
	offsets := make([]int64, len(objects))
	for i, o := range objects {
		offsets[i] = out.n
		if err = write(fmt.Sprintf("%d 0 obj\n", i+1)); err != nil {
			return err
		}
		if stream, ok := o.(types.StreamDict); ok {
			if err = write(stream.PDFString() + "\nstream\n"); err != nil {
				return err
			}
			if _, err = out.Write(stream.Raw); err != nil {
				return err
			}
			if err = write("\nendstream"); err != nil {
				return err
			}
		} else if err = write(o.PDFString()); err != nil {
			return err
		}
		if err = write("\nendobj\n"); err != nil {
			return err
		}
	}
	start := out.n
	if err = write(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)); err != nil {
		return err
	}
	for _, offset := range offsets {
		if offset > 9999999999 {
			return errors.New("output exceeds classic xref offset range")
		}
		if err = write(fmt.Sprintf("%010d 00000 n \n", offset)); err != nil {
			return err
		}
	}
	return write(fmt.Sprintf("trailer\n%s\nstartxref\n%d\n%%%%EOF\n", trailer.PDFString(), start))
}
