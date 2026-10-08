// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly

import "strconv"

type (
	// Ref identifies a node of a job's source. A JSON plan numbers its nodes in reading order from the root
	// (Ref 0); a command line numbers its operands from zero, excluding the executable name. The Source that
	// created a job turns a Ref into a pointer.
	Ref uint32

	// Origin locates one value in the job's Source. It is small because every styled field carries one.
	Origin struct {
		// Offset is the absolute byte offset of the value in the original document, including any
		// leading byte order mark. It is zero for command-line sources.
		Offset int64
		// Ref is the item (or the root) that contains the value.
		Ref Ref
	}

	// Source describes the document a job was read from, so an Origin can be explained. Implementations
	// are the plan decoder's document and ArgumentSource.
	Source interface {
		// Name is the file name, "<stdin>", "<inline>", or "argv".
		Name() string
		// Pointer returns the JSON pointer of the node (such as "/items/42/items/3"), with member appended
		// for a field of that node (such as "/count"), or "argv:N" for a command-line operand.
		Pointer(ref Ref, member string) string
		// Position returns the 1-based line and byte column of an offset, or 0, 0 when the source has none.
		Position(offset int64) (int, int)
	}

	// Location is an Origin explained: where a value was written.
	Location struct {
		Source  string
		Pointer string
		Offset  int64
		Line    int
		Column  int
	}

	// ArgumentSource is the Source of jobs compiled from command-line operands. Ref N is the operand at
	// zero-based position N, excluding the executable name.
	ArgumentSource struct{}

	// Field is a value of a layered style together with where it was written. The zero Field is unset.
	Field[T comparable] struct {
		Value  T
		Origin Origin
		set    bool
	}
)

// Locate explains origin in src. member is the pointer suffix of a field below the node ("" for the
// node itself), such as "/count" or "/blank/text/font".
func Locate(src Source, origin Origin, member string) Location {
	line, column := src.Position(origin.Offset)

	return Location{
		Source:  src.Name(),
		Pointer: src.Pointer(origin.Ref, member),
		Offset:  origin.Offset,
		Line:    line,
		Column:  column,
	}
}

// String formats the location as "name:line:column (byte N) at /pointer".
func (l Location) String() string {
	text := l.Source
	if l.Line > 0 {
		text += ":" + strconv.Itoa(l.Line) + ":" + strconv.Itoa(l.Column) + " (byte " + strconv.FormatInt(l.Offset, 10) + ")"
	}

	if l.Pointer != "" {
		text += " at " + l.Pointer
	}

	return text
}

// Name returns "argv".
func (ArgumentSource) Name() string { return "argv" }

// Pointer returns "argv:N" for operand N; member is ignored.
func (ArgumentSource) Pointer(ref Ref, _ string) string {
	return "argv:" + strconv.FormatUint(uint64(ref), 10)
}

// Position returns 0, 0: operands have no line or column.
func (ArgumentSource) Position(int64) (int, int) { return 0, 0 }

// Set returns a set Field.
func Set[T comparable](value T, origin Origin) Field[T] {
	return Field[T]{Value: value, Origin: origin, set: true}
}

// IsSet reports whether the field holds a value.
func (f Field[T]) IsSet() bool {
	return f.set
}

// ApplyTo stores the held value in *target, and leaves *target alone when the field is unset.
func (f Field[T]) ApplyTo(target *T) {
	if f.set {
		*target = f.Value
	}
}

// Over returns f when it is set, otherwise base. The winner keeps its own Origin, so a resolved style
// can say which layer supplied each value.
func (f Field[T]) Over(base Field[T]) Field[T] {
	if f.set {
		return f
	}

	return base
}
