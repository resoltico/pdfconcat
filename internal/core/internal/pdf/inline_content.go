// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

const drawingOperators = " q Q cm w J j M d ri i gs m l c v y h re S s f F f* B B* b b* n W W* BT ET Tc Tw Tz TL Tf Tr Ts Td TD Tm T* Tj TJ ' \" d0 d1 CS cs SC SCN sc scn G g RG rg K k sh Do MP DP BMC BDC EMC BX EX "

func pdfSpace(ch byte) bool { return strings.ContainsRune("\x00\t\n\f\r ", rune(ch)) }

// inlineTail recognizes an inline image boundary without treating its binary
// payload as PDF operators. Every candidate must leave a complete lexical
// content stream; ambiguous boundaries are refused rather than guessed. The
// bounded work budget also prevents adversarial binary data causing rescans.
func inlineTail(c context.Context, rest string, budget *int64, depth int) (string, error) {
	if depth > 64 {
		return "", errors.New("inline image nesting exceeds lexical limit")
	}
	// Inline image dictionary operands end at the ID operator.
	for {
		rest = strings.TrimLeft(rest, "\x00\t\n\f\r ")
		if rest == "" {
			return "", errors.New("inline image lacks ID")
		}
		if strings.HasPrefix(rest, "ID") && len(rest) > 2 && pdfSpace(rest[2]) {
			rest = rest[3:]
			break
		}
		if rest[0] == '%' {
			n := strings.IndexAny(rest, "\r\n")
			if n < 0 {
				return "", errors.New("truncated inline dictionary")
			}
			rest = rest[n:]
			continue
		}
		before := len(rest)
		value, err := model.ParseObjectWithPolicy(c, &rest, 0, model.ValidationStrict, 256)
		if err != nil || value.StrictFailure != nil || len(rest) >= before {
			return "", errors.New("invalid inline image dictionary")
		}
	}
	var chosen string
	matches := 0
	for offset := 0; offset+2 <= len(rest); {
		if err := c.Err(); err != nil {
			return "", err
		}
		index := strings.Index(rest[offset:], "EI")
		if index < 0 {
			break
		}
		index += offset
		offset = index + 2
		if index == 0 || !pdfSpace(rest[index-1]) || offset < len(rest) && !pdfSpace(rest[offset]) {
			continue
		}
		tail := rest[offset:]
		*budget -= int64(len(tail)) + 1
		if *budget < 0 {
			return "", errors.New("inline image boundary work limit exceeded")
		}
		if lexicalDrawingTail(c, tail, budget, depth+1) == nil {
			chosen = tail
			matches++
			if matches > 1 {
				return "", errors.New("ambiguous inline image boundary")
			}
		}
	}
	if matches != 1 {
		return "", errors.New("inline image lacks an unambiguous EI boundary")
	}
	return chosen, nil
}

func lexicalDrawingTail(c context.Context, rest string, budget *int64, depth int) error {
	compatibility := 0
	for len(rest) > 0 {
		if err := c.Err(); err != nil {
			return err
		}
		rest = strings.TrimLeft(rest, "\x00\t\n\f\r ")
		if rest == "" {
			break
		}
		if rest[0] == '%' {
			n := strings.IndexAny(rest, "\r\n")
			if n < 0 {
				return nil
			}
			rest = rest[n:]
			continue
		}
		ch := rest[0]
		if strings.ContainsRune("/[(<+-.0123456789", rune(ch)) || strings.HasPrefix(rest, "true") || strings.HasPrefix(rest, "false") || strings.HasPrefix(rest, "null") {
			before := len(rest)
			value, err := model.ParseObjectWithPolicy(c, &rest, 0, model.ValidationStrict, 256)
			if err != nil || value.StrictFailure != nil || len(rest) >= before {
				return errors.New("invalid content operand")
			}
			continue
		}
		n := strings.IndexAny(rest, "\x00\t\n\f\r /[]<>()%")
		if n < 0 {
			n = len(rest)
		}
		if n == 0 {
			return errors.New("invalid content delimiter")
		}
		op := rest[:n]
		rest = rest[n:]
		if op == "BI" {
			var err error
			rest, err = inlineTail(c, rest, budget, depth+1)
			if err != nil {
				return err
			}
			continue
		}
		if op == "BX" {
			compatibility++
		}
		if op == "EX" {
			compatibility--
			if compatibility < 0 {
				return errors.New("unbalanced compatibility section")
			}
		}
		if compatibility == 0 && !strings.Contains(drawingOperators, " "+op+" ") {
			return errors.New("unknown content operator after inline image")
		}
	}
	if compatibility != 0 {
		return errors.New("unclosed compatibility section")
	}
	return nil
}
