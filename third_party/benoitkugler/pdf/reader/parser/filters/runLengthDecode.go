package filters

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

type SkipperRunLength struct{}

const eodRunLength = 0x80

func unexpectedEOF(err error) error {
	if err == io.EOF {
		return errors.New("missing EOD marker in encoded stream")
	}
	return err
}

func decodeRunLength(w io.ByteWriter, src io.ByteReader) error {
	for b, err := src.ReadByte(); ; b, err = src.ReadByte() {
		// EOF is an error since we expect the EOD marker
		if err != nil {
			return unexpectedEOF(err)
		}
		if b == eodRunLength { // eod
			return nil
		}
		if b < 0x80 {
			c := int(b) + 1
			for j := 0; j < c; j++ {
				nextChar, err := src.ReadByte()
				if err != nil {
					return unexpectedEOF(err) // EOF here is an error
				}
				if err := w.WriteByte(nextChar); err != nil {
					return err
				}
			}
			continue
		}
		c := 257 - int(b)
		nextChar, err := src.ReadByte()
		if err != nil {
			return unexpectedEOF(err) // EOF here is an error
		}
		for j := 0; j < c; j++ {
			if err := w.WriteByte(nextChar); err != nil {
				return err
			}
		}
	}
}

// Skip implements Skipper for an RunLengthDecode filter.
func (f SkipperRunLength) Skip(encoded io.Reader) (int, error) {
	// we make sure not to read passed EOD
	r := newCountReader(encoded)
	w := &discardByteWriter{}
	if b, ok := encoded.(*boundedInput); ok {
		w.bounded = true
		w.remaining = b.decoded
	}
	err := decodeRunLength(w, r)
	if b, ok := encoded.(*boundedInput); ok {
		b.discarded = b.decoded - w.remaining
	}
	return r.totalRead, err
}

func runLengthDecoder(src io.Reader) (io.Reader, error) {
	var dst bytes.Buffer
	err := decodeRunLength(&dst, bufio.NewReader(src))
	if err != nil {
		return nil, err
	}
	return &dst, nil
}
