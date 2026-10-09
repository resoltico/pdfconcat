/*
Copyright 2018 The pdfcpu Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package types

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/filter"
	"github.com/pdfcpu/pdfcpu/pkg/log"
)

// PDFFilter represents a PDF stream filter object.
type PDFFilter struct {
	Name        string
	DecodeParms Dict
}

// StreamDict represents a PDF stream dict object.
type StreamDict struct {
	Dict
	StreamOffset      int64
	StreamLength      *int64
	StreamLengthObjNr *int
	FilterPipeline    []PDFFilter
	Raw               []byte // Encoded
	Content           []byte // Decoded
	//DCTImage          image.Image
	IsPageContent bool
	CSComponents  int
}

// NewStreamDict creates a new PDFStreamDict for given PDFDict, stream offset and length.
func NewStreamDict(d Dict, streamOffset int64, streamLength *int64, streamLengthObjNr *int, filterPipeline []PDFFilter) StreamDict {
	return StreamDict{
		d,
		streamOffset,
		streamLength,
		streamLengthObjNr,
		filterPipeline,
		nil,
		nil,
		//nil,
		false,
		0,
	}
}

// Clone returns a clone of sd.
func (sd StreamDict) Clone() Object {
	sd1 := sd
	sd1.Dict = sd.Dict.Clone().(Dict)
	pl := make([]PDFFilter, len(sd.FilterPipeline))
	for k, v := range sd.FilterPipeline {
		f := PDFFilter{}
		f.Name = v.Name
		if v.DecodeParms != nil {
			f.DecodeParms = v.DecodeParms.Clone().(Dict)
		}
		pl[k] = f
	}
	sd1.FilterPipeline = pl
	return sd1
}

// HasSoleFilterNamed returns true if sd has a
// filterPipeline with 1 filter named filterName.
func (sd StreamDict) HasSoleFilterNamed(filterName string) bool {
	fpl := sd.FilterPipeline
	if fpl == nil || len(fpl) != 1 {
		return false
	}
	return fpl[0].Name == filterName
}

// Image returns the image stream dictionary data.
func (sd StreamDict) Image() bool {
	s := sd.Type()
	if s == nil || *s != "XObject" {
		return false
	}
	s = sd.Subtype()
	if s == nil || *s != "Image" {
		return false
	}
	return true
}

// DecodeLazyObjectStreamObjectFunc decodes one serialized object from an object stream.
type DecodeLazyObjectStreamObjectFunc func(c context.Context, s string) (Object, error)

// LazyObjectStreamObject defers decoding an object-stream entry until it is accessed.
type LazyObjectStreamObject struct {
	osd         *ObjectStreamDict
	startOffset int
	endOffset   int

	decodeFunc    DecodeLazyObjectStreamObjectFunc
	decodedObject Object
	decodedError  error
}

// NewLazyObjectStreamObject returns a lazy object stream object.
func NewLazyObjectStreamObject(osd *ObjectStreamDict, startOffset, endOffset int, decodeFunc DecodeLazyObjectStreamObjectFunc) Object {
	return LazyObjectStreamObject{
		osd:         osd,
		startOffset: startOffset,
		endOffset:   endOffset,

		decodeFunc: decodeFunc,
	}
}

// Clone returns a copy of sd.
func (l LazyObjectStreamObject) Clone() Object {
	return LazyObjectStreamObject{
		osd:         l.osd,
		startOffset: l.startOffset,
		endOffset:   l.endOffset,

		decodeFunc:    l.decodeFunc,
		decodedObject: l.decodedObject,
		decodedError:  l.decodedError,
	}
}

// PDFString returns a PDF string representation of sd.
func (l LazyObjectStreamObject) PDFString() string {
	data, err := l.GetData()
	if err != nil {
		panic(err)
	}

	return string(data)
}

// String returns the string value of l.
func (l LazyObjectStreamObject) String() string {
	return l.PDFString()
}

// GetData returns the stream data.
func (l *LazyObjectStreamObject) GetData() ([]byte, error) {
	return l.GetDataContext(context.Background())
}

// GetDataContext materializes lazy object-stream data under its operation context and full cap.
func (l *LazyObjectStreamObject) GetDataContext(c context.Context) ([]byte, error) {
	if err := l.osd.DecodeWithContextAndLimit(c, l.osd.MaxDecodeBytes); err != nil {
		return nil, err
	}

	var data []byte
	if l.endOffset == -1 {
		if l.startOffset < 0 || l.startOffset > len(l.osd.Content) {
			return nil, fmt.Errorf("object stream offset %d out of bounds", l.startOffset)
		}
		data = l.osd.Content[l.startOffset:]
	} else {
		if l.startOffset < 0 || l.startOffset > l.endOffset || l.endOffset > len(l.osd.Content) {
			return nil, fmt.Errorf("object stream offset range [%d:%d] out of bounds", l.startOffset, l.endOffset)
		}
		data = l.osd.Content[l.startOffset:l.endOffset]
	}
	return data, nil
}

// DecodedObject returns the decoded object at index i.
func (l *LazyObjectStreamObject) DecodedObject(c context.Context) (Object, error) {
	if err := c.Err(); err != nil {
		return nil, err
	}
	if l.decodedObject == nil && l.decodedError == nil {
		data, err := l.GetDataContext(c)
		if err != nil {
			return nil, err
		}

		if log.ReadEnabled() {
			log.Read.Printf("parseObjectStream: objString = %s\n", string(data))
		}

		value, parseErr := l.decodeFunc(c, string(data))
		if err := c.Err(); err != nil {
			return nil, err
		}
		l.decodedObject, l.decodedError = value, parseErr
		if l.decodedError != nil {
			return nil, l.decodedError
		}

		if log.ReadEnabled() {
			//log.Read.Printf("parseObjectStream: [%d] = obj %s:\n%s\n", i/2-1, objs[i-2], o)
		}
	}
	return l.decodedObject, l.decodedError
}

// ObjectStreamDict represents a object stream dictionary.
type ObjectStreamDict struct {
	StreamDict
	Prolog         []byte
	ObjCount       int
	FirstObjOffset int
	MaxDecodeBytes int64
	ObjArray       Array
}

// NewObjectStreamDict creates a new ObjectStreamDict object.
func NewObjectStreamDict() *ObjectStreamDict {
	sd := StreamDict{Dict: NewDict()}
	sd.Insert("Type", Name("ObjStm"))
	sd.Insert("Filter", Name(filter.Flate))
	sd.FilterPipeline = []PDFFilter{{Name: filter.Flate, DecodeParms: nil}}
	return &ObjectStreamDict{StreamDict: sd}
}

func parmsForFilter(d Dict) map[string]int {
	m := map[string]int{}

	if d == nil {
		return m
	}

	for k, v := range d {

		i, ok := v.(Integer)
		if ok {
			m[k] = i.Value()
			continue
		}

		// Encode boolean values: false -> 0, true -> 1
		b, ok := v.(Boolean)
		if ok {
			m[k] = 0
			if b.Value() {
				m[k] = 1
			}
			continue
		}

	}

	return m
}

// Encode applies sd's filter pipeline to sd.Content in order to produce sd.Raw.
func (sd *StreamDict) Encode() error {
	if sd.Content == nil && sd.Raw != nil {
		// Not decoded yet, no need to encode.
		return nil
	}

	// No filter specified, nothing to encode.
	if sd.FilterPipeline == nil {
		if log.TraceEnabled() {
			log.Trace.Println("encodeStream: returning uncompressed stream.")
		}
		sd.Raw = sd.Content
		streamLength := int64(len(sd.Raw))
		sd.StreamLength = &streamLength
		sd.Update("Length", Integer(streamLength))
		return nil
	}

	var b, c io.Reader
	b = bytes.NewReader(sd.Content)

	// Apply each filter in the pipeline to result of preceding filter.

	for i := len(sd.FilterPipeline) - 1; i >= 0; i-- {
		f := sd.FilterPipeline[i]
		if log.TraceEnabled() {
			if f.DecodeParms != nil {
				log.Trace.Printf("encodeStream: encoding filter:%s\ndecodeParms:%s\n", f.Name, f.DecodeParms)
			} else {
				log.Trace.Printf("encodeStream: encoding filter:%s\n", f.Name)
			}
		}

		// Make parms map[string]int
		parms := parmsForFilter(f.DecodeParms)

		fi, err := filter.NewFilter(f.Name, parms)
		if err != nil {
			return err
		}

		c, err = fi.Encode(b)
		if err != nil {
			return err
		}

		b = c
	}

	if bb, ok := c.(*bytes.Buffer); ok {
		sd.Raw = bb.Bytes()
	} else {
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, c); err != nil {
			return err
		}

		sd.Raw = buf.Bytes()
	}

	streamLength := int64(len(sd.Raw))
	sd.StreamLength = &streamLength
	sd.Update("Length", Integer(streamLength))

	return nil
}

func fixParms(f PDFFilter, parms map[string]int, sd *StreamDict) error {
	if f.Name == filter.CCITTFax {
		// x/image/ccitt needs the optional decode parameter "Rows"
		// if not available we supply image "Height".
		// Xref-aware image callers resolve Rows and Height before Decode.
		// This direct fallback supports programmatically constructed stream dictionaries.
		_, ok := parms["Rows"]
		if !ok {
			ip := sd.IntEntry("Height")
			if ip == nil {
				return errors.New("ccitt: \"Height\" required")
			}
			parms["Rows"] = *ip
		}
	}
	return nil
}

func preserveEncodedImageFilter(name string) bool {
	return name == filter.JPX || name == filter.JBIG2
}

func decodedContent(r io.Reader) ([]byte, error) {
	return decodedContentWithContext(context.Background(), r, -1)
}

func decodedContentWithContext(c context.Context, r io.Reader, maxDecodeBytes int64) ([]byte, error) {
	if err := c.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, errors.New("copy decoded content: missing reader")
	}
	limit := filter.FullDecodeLimit(maxDecodeBytes)
	if buffer, ok := r.(*bytes.Buffer); ok {
		if limit >= 0 && int64(buffer.Len()) > limit {
			return nil, filter.ErrDecodeLimitExceeded
		}
		return buffer.Bytes(), c.Err()
	}
	reader := filter.ContextReader(c, r)
	if limit >= 0 && limit < math.MaxInt64 {
		reader = io.LimitReader(reader, limit+1)
	}
	var buffer bytes.Buffer
	if _, err := io.Copy(&buffer, reader); err != nil {
		return nil, fmt.Errorf("copy decoded content: %w", err)
	}
	if err := c.Err(); err != nil {
		return nil, err
	}
	if limit >= 0 && int64(buffer.Len()) > limit {
		return nil, filter.ErrDecodeLimitExceeded
	}
	return buffer.Bytes(), nil
}

// Decode applies sd's filter pipeline to sd.Raw in order to produce sd.Content.
func (sd *StreamDict) Decode() error {
	return sd.DecodeWithContextAndLimit(context.Background(), filter.DefaultMaxDecodeBytes)
}

// DecodeWithLimit decodes a complete stream with the maintained full output cap.
func (sd *StreamDict) DecodeWithLimit(limit int64) error {
	return sd.DecodeWithContextAndLimit(context.Background(), limit)
}

// DecodeWithContextAndLimit enforces actual full output length on cached, aliased, eager and lazy
// results while propagating cooperative decoding cancellation. The cap is not total memory usage.
func (sd *StreamDict) DecodeWithContextAndLimit(c context.Context, limit int64) error {
	_, err := sd.DecodeLengthWithContextAndLimit(c, -1, limit)
	return err
}

func (sd *StreamDict) decodeLengthContext(ctx context.Context, maxLen, maxDecodeBytes int64) ([]byte, error) {
	var reader io.Reader = bytes.NewReader(sd.Raw)
	for index, stage := range sd.FilterPipeline {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		opaque := preserveEncodedImageFilter(stage.Name) || stage.Name == filter.DCT && sd.CSComponents != 4
		if opaque {
			if index != len(sd.FilterPipeline)-1 {
				return nil, fmt.Errorf("stream filter[%d] %q: decode: %w", index, stage.Name, filter.ErrUnsupportedFilter)
			}
			break
		}
		parms := parmsForFilter(stage.DecodeParms)
		if err := fixParms(stage, parms, sd); err != nil {
			return nil, fmt.Errorf("stream filter[%d] %q: prepare parameters: %w", index, stage.Name, err)
		}
		codec, err := filter.NewFilterWithContext(ctx, stage.Name, parms, maxDecodeBytes)
		if err != nil {
			return nil, fmt.Errorf("stream filter[%d] %q: construct: %w", index, stage.Name, err)
		}
		partial := maxLen >= 0 && index == len(sd.FilterPipeline)-1
		if partial {
			reader, err = codec.DecodeLength(reader, maxLen)
		} else {
			reader, err = codec.Decode(reader)
		}
		if err != nil {
			return nil, fmt.Errorf("stream filter[%d] %q: decode: %w", index, stage.Name, err)
		}
		stageLimit := maxDecodeBytes
		if partial {
			stageLimit = -1 // Partial codecs may return a full predictor row before slicing the prefix.
		}
		data, err := decodedContentWithContext(ctx, reader, stageLimit)
		if err != nil {
			return nil, fmt.Errorf("stream filter[%d] %q: output: %w", index, stage.Name, err)
		}
		reader = bytes.NewBuffer(data)
	}
	finalLimit := maxDecodeBytes
	if maxLen >= 0 {
		finalLimit = -1
	} // Maintained partial-prefix semantics are distinct from full caps.
	data, err := decodedContentWithContext(ctx, reader, finalLimit)
	if err != nil {
		return nil, err
	}
	if maxLen >= 0 {
		if maxLen > int64(len(data)) {
			return nil, io.ErrUnexpectedEOF
		}
		return data[:maxLen], ctx.Err()
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	sd.Content = data
	return data, nil
}

// DecodeLength decodes at least the requested prefix under the maintained partial semantics.
func (sd *StreamDict) DecodeLength(maxLen int64) ([]byte, error) {
	return sd.DecodeLengthWithLimit(maxLen, filter.DefaultMaxDecodeBytes)
}

// DecodeLengthWithLimit retains partial-prefix limit precedence; use the full method for a hard cap.
func (sd *StreamDict) DecodeLengthWithLimit(maxLen, maxDecodeBytes int64) ([]byte, error) {
	return sd.DecodeLengthWithContextAndLimit(context.Background(), maxLen, maxDecodeBytes)
}

// DecodeLengthWithContextAndLimit carries cancellation through partial decode and full decode.
func (sd *StreamDict) DecodeLengthWithContextAndLimit(ctx context.Context, maxLen, maxDecodeBytes int64) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if sd.Content != nil {
		if maxLen < 0 && filter.FullDecodeLimit(maxDecodeBytes) >= 0 && int64(len(sd.Content)) > filter.FullDecodeLimit(maxDecodeBytes) {
			return nil, filter.ErrDecodeLimitExceeded
		}
		if maxLen < 0 {
			return sd.Content, ctx.Err()
		}
		if maxLen > int64(len(sd.Content)) {
			return nil, io.ErrUnexpectedEOF
		}
		return sd.Content[:maxLen], ctx.Err()
	}
	pipeline := sd.FilterPipeline
	if len(pipeline) == 0 || len(pipeline) == 1 && (pipeline[0].Name == filter.DCT && sd.CSComponents != 4 || preserveEncodedImageFilter(pipeline[0].Name)) {
		if maxLen < 0 && filter.FullDecodeLimit(maxDecodeBytes) >= 0 && int64(len(sd.Raw)) > filter.FullDecodeLimit(maxDecodeBytes) {
			return nil, filter.ErrDecodeLimitExceeded
		}
		if maxLen >= 0 && maxLen > int64(len(sd.Raw)) {
			return nil, io.ErrUnexpectedEOF
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sd.Content = sd.Raw
		if maxLen < 0 {
			return sd.Content, nil
		}
		return sd.Content[:maxLen], nil
	}
	return sd.decodeLengthContext(ctx, maxLen, maxDecodeBytes)
}

// IndexedObject returns the object at given index from a ObjectStreamDict.
func (osd *ObjectStreamDict) IndexedObject(index int) (Object, error) {
	if osd.ObjArray == nil || index < 0 || index >= len(osd.ObjArray) {
		return nil, fmt.Errorf("IndexedObject(%d): object not available", index)
	}
	return osd.ObjArray[index], nil
}

// AddObject adds another object to this object stream.
// Relies on decoded content!
func (osd *ObjectStreamDict) AddObject(objNumber int, pdfString string) error {
	offset := len(osd.Content)
	s := ""
	if osd.ObjCount > 0 {
		s = " "
	}
	s = s + fmt.Sprintf("%d %d", objNumber, offset)
	osd.Prolog = append(osd.Prolog, []byte(s)...)
	//pdfString := entry.Object.PDFString()
	osd.Content = append(osd.Content, []byte(pdfString)...)
	osd.ObjCount++
	if log.TraceEnabled() {
		log.Trace.Printf("AddObject end : ObjCount:%d prolog = <%s> Content = <%s>\n", osd.ObjCount, osd.Prolog, osd.Content)
	}
	return nil
}

// Finalize prepares the final content of the objectstream.
func (osd *ObjectStreamDict) Finalize() {
	osd.Content = append(osd.Prolog, osd.Content...)
	osd.FirstObjOffset = len(osd.Prolog)
	if log.TraceEnabled() {
		log.Trace.Printf("Finalize : firstObjOffset:%d Content = <%s>\n", osd.FirstObjOffset, osd.Content)
	}
}

// XRefStreamDict represents a cross reference stream dictionary.
type XRefStreamDict struct {
	StreamDict
	Size           int
	Objects        []int
	W              [3]int
	PreviousOffset *int64
}
