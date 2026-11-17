// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdfcpu

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type encryptionDictionaryCanary struct {
	reads int
	err   error
}

func (r *encryptionDictionaryCanary) Read([]byte) (int, error)     { r.reads++; return 0, r.err }
func (*encryptionDictionaryCanary) Seek(int64, int) (int64, error) { return 0, nil }

func TestEncryptedInputReadPolicy(t *testing.T) {
	t.Run("before-dictionary-access", func(t *testing.T) {
		for _, reject := range []bool{false, true} {
			conf := model.NewStatelessConfiguration()
			conf.RejectEncryptedInput = reject
			ctx, err := model.NewContext(bytes.NewReader(nil), conf)
			if err != nil {
				t.Fatal(err)
			}
			canary := &encryptionDictionaryCanary{err: errors.New("encryption dictionary read canary")}
			ctx.Read.RS = canary
			ctx.Encrypt = types.NewIndirectRef(7, 0)
			entry := model.NewXRefTableEntryGen0(nil)
			offset := int64(0)
			entry.Offset = &offset
			ctx.Table[7] = entry
			err = checkForEncryption(t.Context(), ctx)
			if reject {
				if !errors.Is(err, ErrEncrypted) || canary.reads != 0 || ctx.E != nil || ctx.EncKey != nil {
					t.Fatal("refusal ran encryption setup", err, canary.reads, ctx.E, ctx.EncKey)
				}
			} else if !errors.Is(err, canary.err) || canary.reads == 0 {
				t.Fatal("default reader did not reach dictionary", err, canary.reads)
			}
		}
	})
	t.Run("default-clone-and-unencrypted", func(t *testing.T) {
		conf := model.NewStatelessConfiguration()
		if conf.RejectEncryptedInput {
			t.Fatal("upstream default changed")
		}
		conf.RejectEncryptedInput = true
		clone := conf.Clone()
		if !clone.RejectEncryptedInput {
			t.Fatal("clone lost read policy")
		}
		clone.RejectEncryptedInput = false
		if !conf.RejectEncryptedInput {
			t.Fatal("clone changed original")
		}
		ctx, err := model.NewContext(bytes.NewReader(nil), conf)
		if err != nil {
			t.Fatal(err)
		}
		if err = checkForEncryption(t.Context(), ctx); err != nil {
			t.Fatal("unencrypted context refused", err)
		}
	})
	t.Run("context-before-refusal", func(t *testing.T) {
		conf := model.NewStatelessConfiguration()
		conf.RejectEncryptedInput = true
		ctx, err := model.NewContext(bytes.NewReader(nil), conf)
		if err != nil {
			t.Fatal(err)
		}
		ctx.Encrypt = types.NewIndirectRef(7, 0)
		cancelled, cancel := context.WithCancel(t.Context())
		cancel()
		if err = checkForEncryption(cancelled, ctx); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if err = checkForEncryption(nil, ctx); !errors.Is(err, model.ErrMissingContext) {
			t.Fatal(err)
		}
	})
	// This existing fixture uses real R4 key/password computation, with Identity
	// content filters. It can be read without a password on the default path.
	base := cryptFilterTestPDF(t, []byte("q Q"), "/Crypt", "<< /Name /Identity >>", "/StmF /Identity /StrF /Identity", nil)
	for _, format := range []string{"classic", "incremental", "stream", "hybrid"} {
		t.Run(format, func(t *testing.T) {
			data := encryptedReadRepresentation(base, format)
			for _, reject := range []bool{false, true} {
				for _, password := range []string{"", "incorrect"} {
					conf := model.NewStatelessConfiguration()
					conf.ValidationMode = model.ValidationStrict
					conf.RejectEncryptedInput = reject
					conf.UserPW = password
					got, err := Read(t.Context(), bytes.NewReader(data), conf)
					if reject {
						if !errors.Is(err, ErrEncrypted) || got != nil {
							t.Fatal("encrypted input passed policy", err, got)
						}
					} else if password != "" {
						if !errors.Is(err, ErrWrongPassword) {
							t.Fatal("negative control did not exercise password validation", err)
						}
					} else {
						if err != nil || got == nil || got.E == nil || got.EncKey == nil {
							t.Fatal("default encryption support changed", err, got)
						}
						if format == "stream" && !got.Read.UsingXRefStreams {
							t.Fatal("stream control did not use xref stream")
						}
						if format == "hybrid" && !got.Read.Hybrid {
							t.Fatal("hybrid control did not use hybrid xref")
						}
					}
				}
			}
		})
	}
}

// Preserve the same encrypted objects while independently writing the xref forms.
func encryptedReadRepresentation(base []byte, format string) []byte {
	if format == "classic" {
		return base
	}
	oldXRef := bytes.Index(base, []byte("\nxref\n")) + 1
	ids := "/ID [<" + strings.Repeat("22", 16) + "> <" + strings.Repeat("22", 16) + ">]"
	if format == "incremental" {
		var b bytes.Buffer
		b.Write(base)
		offset := b.Len()
		b.WriteString("6 0 obj\n<< /Unused 42 >>\nendobj\n")
		xref := b.Len()
		fmt.Fprintf(&b, "xref\n6 1\n%010d 00000 n \ntrailer\n<< /Size 7 /Root 1 0 R /Prev %d >>\nstartxref\n%d\n%%%%EOF\n", offset, oldXRef, xref)
		return b.Bytes()
	}
	var b bytes.Buffer
	b.Write(base[:oldXRef])
	offsets := make([]int, 7)
	for n := 1; n <= 5; n++ {
		offsets[n] = bytes.Index(base, []byte(strconv.Itoa(n)+" 0 obj\n"))
	}
	offsets[6] = b.Len()
	records := make([]byte, 7*7)
	binary.BigEndian.PutUint16(records[5:7], 65535)
	for n := 1; n <= 6; n++ {
		record := records[n*7 : (n+1)*7]
		record[0] = 1
		binary.BigEndian.PutUint32(record[1:5], uint32(offsets[n]))
	}
	fmt.Fprintf(&b, "6 0 obj\n<< /Type /XRef /Size 7 /Root 1 0 R /Encrypt 5 0 R %s /W [1 4 2] /Length %d >>\nstream\n", ids, len(records))
	b.Write(records)
	b.WriteString("\nendstream\nendobj\n")
	if format == "hybrid" {
		xref := b.Len()
		b.WriteString("xref\n0 7\n0000000000 65535 f \n")
		for n := 1; n <= 6; n++ {
			fmt.Fprintf(&b, "%010d 00000 n \n", offsets[n])
		}
		fmt.Fprintf(&b, "trailer\n<< /Size 7 /Root 1 0 R /Encrypt 5 0 R %s /XRefStm %d >>\nstartxref\n%d\n%%%%EOF\n", ids, offsets[6], xref)
	} else {
		fmt.Fprintf(&b, "startxref\n%d\n%%%%EOF\n", offsets[6])
	}
	return b.Bytes()
}
