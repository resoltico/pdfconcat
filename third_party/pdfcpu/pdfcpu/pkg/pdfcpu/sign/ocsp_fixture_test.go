// Copyright 2026 The pdfcpu Authors.
// SPDX-License-Identifier: Apache-2.0

package sign

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ocsp"
)

type (
	ocspFixtureEnvelope struct {
		Status asn1.Enumerated
		Bytes  ocspFixtureResponseBytes `asn1:"explicit,optional,tag:0"`
	}
	ocspFixtureResponseBytes struct {
		Type     asn1.ObjectIdentifier
		Response []byte
	}
	ocspFixtureResponseData struct {
		Responder  asn1.RawValue
		ProducedAt time.Time `asn1:"generalized"`
		Responses  []asn1.RawValue
	}
	ocspFixtureBasicResponse struct {
		Data         asn1.RawValue
		Algorithm    pkix.AlgorithmIdentifier
		Signature    asn1.BitString
		Certificates []asn1.RawValue `asn1:"explicit,tag:0,optional"`
	}
)

func archivedOCSPFixtureTime() time.Time {
	return time.Date(2020, time.January, 2, 12, 0, 0, 0, time.UTC)
}

// CreateResponse ignores Response.ProducedAt and reads the current minute itself.
// Archived fixtures need an explicit historical time, authenticated by the real
// responder key, rather than racing certificate expiry against RSA generation.
func testHistoricalOCSPResponse(t *testing.T, encoded []byte, key *rsa.PrivateKey, producedAt time.Time) []byte {
	t.Helper()
	envelope, basic := testOCSPFixtureProducedAt(t, encoded, producedAt)
	digest := sha256.Sum256(basic.Data.FullBytes)
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	basic.Signature = asn1.BitString{Bytes: signature, BitLength: len(signature) * 8}
	return testEncodeOCSPFixture(t, envelope, basic)
}

func testOCSPFixtureProducedAt(t *testing.T, encoded []byte, producedAt time.Time) (*ocspFixtureEnvelope, *ocspFixtureBasicResponse) {
	t.Helper()
	parsed, err := ocsp.ParseResponse(encoded, nil)
	if err != nil || parsed.SignatureAlgorithm != x509.SHA256WithRSA {
		t.Fatalf("historical fixture requires an authenticated SHA256WithRSA response: %v", err)
	}
	var envelope ocspFixtureEnvelope
	testUnmarshalOCSPFixture(t, encoded, &envelope)
	testCanonicalOCSPFixture(t, encoded, envelope)
	if envelope.Status != 0 || !envelope.Bytes.Type.Equal(asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 1}) {
		t.Fatal("historical fixture requires a successful BasicOCSPResponse")
	}
	var basic ocspFixtureBasicResponse
	testUnmarshalOCSPFixture(t, envelope.Bytes.Response, &basic)
	testCanonicalOCSPFixture(t, envelope.Bytes.Response, basic)
	var data ocspFixtureResponseData
	testUnmarshalOCSPFixture(t, basic.Data.FullBytes, &data)
	testCanonicalOCSPFixture(t, basic.Data.FullBytes, data)
	data.ProducedAt = producedAt.UTC()
	basic.Data = asn1.RawValue{FullBytes: testMarshalOCSPFixture(t, data)}
	return &envelope, &basic
}

func testUnmarshalOCSPFixture(t *testing.T, encoded []byte, value any) {
	t.Helper()
	remainder, err := asn1.Unmarshal(encoded, value)
	if err != nil || len(remainder) != 0 {
		t.Fatalf("invalid or trailing ASN.1 in historical OCSP fixture: %v", err)
	}
}

func testCanonicalOCSPFixture(t *testing.T, encoded []byte, decoded any) {
	t.Helper()
	if !bytes.Equal(encoded, testMarshalOCSPFixture(t, decoded)) {
		t.Fatal("unexpected fields or noncanonical ASN.1 in historical OCSP fixture")
	}
}

func testMarshalOCSPFixture(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := asn1.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func testEncodeOCSPFixture(t *testing.T, envelope *ocspFixtureEnvelope, basic *ocspFixtureBasicResponse) []byte {
	t.Helper()
	envelope.Bytes.Response = testMarshalOCSPFixture(t, *basic)
	return testMarshalOCSPFixture(t, *envelope)
}

func TestHistoricalOCSPFixtureRejectsUnsignedTimeChange(t *testing.T) {
	producedAt := archivedOCSPFixtureTime()
	_, issuer, responder, encoded := testArchivedOCSPFixture(
		t, producedAt, producedAt.Add(-time.Hour), producedAt.Add(time.Hour), producedAt.Add(2*time.Hour),
	)
	parsed, err := ocsp.ParseResponse(encoded, issuer)
	if err != nil {
		t.Fatal(err)
	}
	if err = parsed.CheckSignatureFrom(responder); err != nil {
		t.Fatal(err)
	}
	if err = responder.CheckSignatureFrom(issuer); err != nil {
		t.Fatal(err)
	}
	if !parsed.ProducedAt.Equal(producedAt) {
		t.Fatal("authentic fixture did not retain its explicit ProducedAt")
	}
	envelope, basic := testOCSPFixtureProducedAt(t, encoded, producedAt.Add(time.Minute))
	tampered := testEncodeOCSPFixture(t, envelope, basic)
	if _, err = ocsp.ParseResponse(tampered, issuer); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("unsigned historical time alteration was not rejected by cryptographic authentication: %v", err)
	}
}
