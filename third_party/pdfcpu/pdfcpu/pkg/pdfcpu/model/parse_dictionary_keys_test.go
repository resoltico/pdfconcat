package model

import (
	"errors"
	"testing"
)

func TestDictionaryDecodedKeyUniquenessBeforeNullOmission(t *testing.T) {
	for _, input := range []string{
		"<< /chapter 1 /chapter 2 >>",
		"<< /chapter 1 /ch#61pter 2 >>",
		"<< /A null /A 2 >>", "<< /A 1 /A null >>", "<< /A null /A null >>",
		"<< /D << /D 1 /D 2 >> >>", "<< /S /URI /S /GoTo >>",
		"<< /Group << /CS /DeviceRGB /CS /DeviceCMYK >> >>",
	} {
		for _, relaxed := range []bool{false, true} {
			remaining := input
			if _, err := parseObject(t.Context(), &remaining, 0, DefaultResourceLimits().MaxRecursionDepth, relaxed); !errors.Is(err, errDictionaryDuplicateKey) {
				t.Fatalf("input=%q relaxed=%v: %v", input, relaxed, err)
			}
		}
		for _, mode := range []int{ValidationStrict, ValidationRelaxed} {
			remaining := input
			if _, err := ParseObjectWithPolicy(t.Context(), &remaining, 0, mode); !errors.Is(err, errDictionaryDuplicateKey) {
				t.Fatalf("policy input=%q mode=%d: %v", input, mode, err)
			}
		}
	}
	for _, input := range []string{
		"<< /A null >>", "<< /Chapter 1 /chapter 2 >>",
		"<< /A << /same 1 >> /B << /same 2 >> >>",
		"<< /A (/chapter /chapter) % /chapter /chapter\n /B 1 >>",
	} {
		remaining := input
		if _, err := ParseObject(t.Context(), &remaining, 0); err != nil {
			t.Fatalf("unique input=%q: %v", input, err)
		}
	}
	remaining := "<< junk /A 1 /A 2 >>"
	if _, err := ParseObjectWithPolicy(t.Context(), &remaining, 0, ValidationRelaxed); !errors.Is(err, errDictionaryDuplicateKey) {
		t.Fatalf("relaxed malformed-key retry concealed duplicate: %v", err)
	}
}
