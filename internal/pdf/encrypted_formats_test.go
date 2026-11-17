// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestEncryptedInputPolicyAcrossSecurityHandlers(t *testing.T) {
	tools := oracle.RequireTools(t)
	source := filepath.Join(t.TempDir(), "plain.pdf")
	if err := pdffixture.Plain("Encrypted policy control").WriteFile(source); err != nil {
		t.Fatal(err)
	}
	for _, cipher := range []struct {
		name, bits string
		options    []string
	}{
		{"RC4-R2", "40", nil},
		{"RC4-R3", "128", []string{"--use-aes=n"}},
		{"AES-R4", "128", []string{"--use-aes=y"}},
		{"AES-R5", "256", []string{"--force-R5"}},
		{"AES-R6", "256", nil},
	} {
		for _, password := range []string{"", "synthetic-password"} {
			for _, streams := range []string{"disable", "generate"} {
				t.Run(cipher.name+"/"+password+"/"+streams, func(t *testing.T) {
					encrypted := filepath.Join(t.TempDir(), "encrypted.pdf")
					args := []string{"--allow-weak-crypto", "--object-streams=" + streams, "--encrypt", password, "synthetic-owner", cipher.bits}
					args = append(args, cipher.options...)
					args = append(args, "--", source, encrypted)
					if output, err := exec.CommandContext(t.Context(), tools.QPDF, args...).CombinedOutput(); err != nil {
						t.Fatalf("qpdf encryption fixture: %v %s", err, output)
					}
					data, err := os.ReadFile(encrypted)
					if err != nil {
						t.Fatal(err)
					}
					digest := sha256.Sum256(data)
					// An independent producer and the unchanged default reader establish the
					// fixture's real encryption/key path before testing product refusal.
					conf := model.NewStatelessConfiguration()
					conf.Offline = true
					conf.ValidationMode = model.ValidationStrict
					conf.UserPW = password
					native, err := api.ReadContext(t.Context(), bytes.NewReader(data), conf)
					if err != nil || native == nil || native.Encrypt == nil || native.EncKey == nil {
						t.Fatal("default reader control did not decrypt valid fixture", err)
					}
					if streams == "generate" && !native.Read.UsingXRefStreams {
						t.Fatal("xref-stream control stayed classic")
					}
					for _, op := range []app.Operation{app.Check, app.Build} {
						result, _, output := selectiveRequest(t, data, "A4", op)
						if result.Status != "invalid" || !codePresent(result.Diagnostics, diag.SourceEncrypted) || result.Output.Published {
							t.Fatal("encrypted input escaped refusal", op, result.Diagnostics)
						}
						if _, err := os.Stat(output); !os.IsNotExist(err) {
							t.Fatal("refused source published output", op, err)
						}
					}
					after, err := os.ReadFile(encrypted)
					if err != nil {
						t.Fatal(err)
					}
					if sha256.Sum256(data) != digest || sha256.Sum256(after) != digest {
						t.Fatal("encryption policy changed source bytes")
					}
				})
			}
		}
	}
}
