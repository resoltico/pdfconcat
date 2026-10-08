// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan_test

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/resoltico/pdfconcat/internal/plan"
)

// refusingLoader is the validator's resource loader: it loads nothing.
type refusingLoader struct{}

const (
	schemaURL     = "https://github.com/resoltico/pdfconcat/blob/main/internal/plan/plan.schema.json"
	metaSchemaURL = "https://json-schema.org/draft/2020-12/schema"
)

var errLoadRefused = errors.New("network or file load refused")

// Load refuses every load that is not an explicitly registered resource, so a schema can never make the
// validator fetch anything.
func (refusingLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("%w: %s", errLoadRefused, url)
}

// compileSchema compiles a schema with no ability to fetch anything. Compiling validates the schema
// against the 2020-12 metaschema bundled with the validator.
func compileSchema(schema []byte, schemaID string) (*jsonschema.Schema, error) {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		return nil, fmt.Errorf("parse schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(refusingLoader{})
	compiler.DefaultDraft(jsonschema.Draft2020)

	err = compiler.AddResource(schemaID, document)
	if err != nil {
		return nil, fmt.Errorf("add schema: %w", err)
	}

	compiled, err := compiler.Compile(schemaID)
	if err != nil {
		return nil, fmt.Errorf("compile schema: %w", err)
	}

	return compiled, nil
}

// schemaVerdict validates one document against a schema. A leading byte order mark is not part of JSON
// text, so it is stripped the way the decoder does.
func schemaVerdict(tb testing.TB, compiled *jsonschema.Schema, document []byte) verdict {
	tb.Helper()

	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(bytes.TrimPrefix(document, []byte("\xef\xbb\xbf"))))
	if err != nil {
		return schemaNotJSON
	}

	if compiled.Validate(instance) != nil {
		return schemaReject
	}

	return schemaAccept
}

func planSchema(tb testing.TB) *jsonschema.Schema {
	tb.Helper()

	compiled, err := compileSchema([]byte(plan.Schema()), schemaURL)
	if err != nil {
		tb.Fatal(err)
	}

	return compiled
}

// TestCorpusAgainstSchemaAndDecoder runs the shared accept/reject corpus through the pinned validator and
// the decoder: they must agree everywhere except the rows that document a gap, and each gap row must
// really diverge.
func TestCorpusAgainstSchemaAndDecoder(t *testing.T) {
	t.Parallel()

	compiled := planSchema(t)

	for _, row := range corpus() {
		t.Run(row.Name, func(t *testing.T) {
			t.Parallel()

			got := schemaVerdict(t, compiled, []byte(row.Doc))
			if got != row.Schema {
				t.Errorf("schema verdict %s, corpus says %s", got, row.Schema)
			}

			_, err := decodeString(t, row.Doc)
			decoderAccepts, schemaAccepts := err == nil, got == schemaAccept

			switch {
			case row.Gap != "" && decoderAccepts == schemaAccepts && got != schemaNotJSON:
				t.Error("documented as a gap, but the decoder and the schema agree")
			case row.Gap == "" && decoderAccepts != schemaAccepts:
				t.Errorf("undocumented divergence: decoder accepts=%v schema=%s", decoderAccepts, got)
			default:
			}
		})
	}
}

// TestGapsAreOnlyWhatSchemasCannotExpress keeps the documented gap list honest: a gap is something the
// schema accepts but the decoder rejects, never a structural rule the schema should state.
func TestGapsAreOnlyWhatSchemasCannotExpress(t *testing.T) {
	t.Parallel()

	for _, row := range corpus() {
		if row.Gap == "" {
			continue
		}

		if row.Schema != schemaAccept || row.Code == "" {
			t.Errorf("%s: a gap row must be one the schema accepts and the decoder rejects", row.Name)
		}
	}
}

func TestBrokenSchemasAreRejected(t *testing.T) {
	t.Parallel()

	for name, broken := range map[string]string{
		"bad type":     `{"$schema":"` + metaSchemaURL + `","type":"strng"}`,
		"bad minimum":  `{"$schema":"` + metaSchemaURL + `","properties":{"a":{"minimum":"1"}}}`,
		"dangling ref": `{"$schema":"` + metaSchemaURL + `","$ref":"#/$defs/missing"}`,
	} {
		_, err := compileSchema([]byte(broken), "https://example.test/broken.json")
		if err == nil {
			t.Errorf("%s: compiled", name)
		}
	}
}

func TestSchemaNeverFetches(t *testing.T) {
	t.Parallel()

	_, err := compileSchema(
		[]byte(`{"$schema":"`+metaSchemaURL+`","$ref":"https://example.com/other.json"}`),
		"https://example.test/remote.json",
	)
	if err == nil || !strings.Contains(err.Error(), errLoadRefused.Error()) {
		t.Fatalf("a remote $ref must fail rather than fetch: %v", err)
	}
}

func TestSchemaDeclaresItsIdentityAndDialect(t *testing.T) {
	t.Parallel()

	for _, want := range []string{`"$schema": "` + metaSchemaURL + `"`, `"$id": "` + schemaURL + `"`} {
		if !strings.Contains(plan.Schema(), want) {
			t.Errorf("schema lacks %s", want)
		}
	}
}

func TestIntegerSemanticsOfThePinnedValidator(t *testing.T) {
	t.Parallel()

	compiled, err := compileSchema([]byte(`{"$schema":"`+metaSchemaURL+`","type":"integer"}`), "https://example.test/integer.json")
	if err != nil {
		t.Fatal(err)
	}

	for text, want := range map[string]verdict{
		"1": schemaAccept, "1.0": schemaAccept, "1e0": schemaAccept, "10e-1": schemaAccept,
		"1.5": schemaReject, "1e-1": schemaReject,
	} {
		if got := schemaVerdict(t, compiled, []byte(text)); got != want {
			t.Errorf("integer %s: %s, want %s", text, got, want)
		}
	}
}
