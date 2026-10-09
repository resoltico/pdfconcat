/*
Copyright 2026 The pdfcpu Authors.
Licensed under the Apache License, Version 2.0.
*/
package types

import (
	"reflect"
	"strings"
	"testing"
)

func TestDictionaryCanonicalBytesGovernEveryOperation(t *testing.T) {
	for _, keys := range [][2]string{{"AB", "A#42"}, {"A#42", "AB"}} {
		d := NewDict()
		for _, key := range keys {
			if !d.Insert(key, Name(key)) {
				t.Fatalf("distinct canonical insertion refused %q", key)
			}
		}
		if len(d) != 2 {
			t.Fatal("distinct names collapsed")
		}
		for _, key := range keys {
			value, found := d.Find(key)
			if !found || value != Name(key) {
				t.Fatalf("exact lookup %q=%v/%v", key, value, found)
			}
		}
		for attempt := range 64 {
			copy := d.Clone().(Dict)
			if !reflect.DeepEqual(copy, d) {
				t.Fatalf("clone lost canonical identity %d: %v", attempt, copy)
			}
		}
		if removed := d.Delete("AB"); removed != Name("AB") || len(d) != 1 || d["A#42"] != Name("A#42") {
			t.Fatalf("delete affected distinct private key: %v", d)
		}
		if value, found := d.Find("AB"); found || value != nil {
			t.Fatal("private literal-hash key acquired another identity")
		}
		if !d.Insert("AB", Integer(2)) {
			t.Fatal("absent exact key blocked by private name")
		}
		d.Update("AB", Integer(3))
		if d["AB"] != Integer(3) || d["A#42"] != Name("A#42") {
			t.Fatal("update collided with private name")
		}
	}
}

func TestCanonicalNamesPreserveLiteralHashesInSerializationAndDiagnostics(t *testing.T) {
	for _, name := range []string{"Cr#6fpBox", "A#42", "unfinished#", "bad#gg"} {
		d := Dict{name: Name(name)}
		if _, found := d.Find("CropBox"); found {
			t.Fatalf("unrelated canonical key %q influenced exact miss", name)
		}
		if name == "Cr#6fpBox" && !strings.Contains(d.PDFString(), "/Cr#236fpBox") {
			t.Fatal("writer did not encode literal hash once")
		}
		if !strings.Contains(d.String(), name) || !strings.Contains(Array{Name(name)}.String(), name) {
			t.Fatalf("diagnostic display invented decoded identity for %q", name)
		}
	}
}
