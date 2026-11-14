// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type markedFacts struct {
	complete bool
	covered  bool
	ids      map[int]map[int]bool
}
type markedScope struct{ artifact, semantic bool }

// markedContent checks ownership without rewriting source operators. Operands
// use the backend's strict object parser, so strings, dictionaries and comments
// cannot masquerade as BDC/EMC operators. Inline image payloads stay opaque and
// byte-preserved; ambiguous binary boundaries are refused.
func markedContent(c context.Context, s *model.Context) (markedFacts, error) {
	facts := markedFacts{complete: true, ids: map[int]map[int]bool{}}
	pages, err := pageGeometries(c, s)
	if err != nil {
		return facts, err
	}
	root, err := structureRoot(s)
	if err != nil {
		return facts, err
	}
	inverse, err := numberTree(c, s, root["ParentTree"])
	if err != nil {
		return facts, err
	}
	objectOwned := func(d types.Dict) (bool, error) {
		key, e := s.DereferenceInteger(d["StructParent"])
		if e != nil {
			return false, e
		}
		if key == nil {
			return false, nil
		}
		parent, e := s.DereferenceDict(inverse[key.Value()])
		if e != nil {
			return false, e
		}
		if parent == nil || parent["S"] == nil {
			return false, errors.New("object lacks a valid structural parent")
		}
		return true, nil
	}
	active := map[int]bool{}
	var scan func([]byte, types.Dict, types.IndirectRef, markedScope, int) error
	scan = func(data []byte, resources types.Dict, owner types.IndirectRef, inherited markedScope, depth int) error {
		if depth > 256 {
			return errors.New("marked-content Form nesting exceeds 256")
		}
		scope := inherited
		stack := []markedScope{}
		args := []types.Object{}
		rest := string(data)
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
					break
				}
				rest = rest[n:]
				continue
			}
			ch := rest[0]
			operand := strings.ContainsRune("/[(<+-.0123456789", rune(ch)) || strings.HasPrefix(rest, "true") || strings.HasPrefix(rest, "false") || strings.HasPrefix(rest, "null")
			if operand {
				before := len(rest)
				value, e := model.ParseObjectWithPolicy(c, &rest, 0, model.ValidationStrict, 256)
				if e != nil || value.StrictFailure != nil || len(rest) >= before {
					return errors.New("malformed marked-content operand")
				}
				args = append(args, value.Object)
				if len(args) > 256 {
					return errors.New("marked-content operand limit exceeded")
				}
				continue
			}
			n := strings.IndexAny(rest, "\x00\t\n\f\r /[]<>()%")
			if n < 0 {
				n = len(rest)
			}
			if n == 0 {
				return errors.New("malformed marked-content operator")
			}
			op := rest[:n]
			rest = rest[n:]
			switch op {
			case "BMC", "BDC":
				if len(args) < 1 {
					return errors.New("marked-content operator lacks a tag")
				}
				tag, ok := args[0].(types.Name)
				if !ok {
					return errors.New("marked-content tag is not a name")
				}
				stack = append(stack, scope)
				scope.artifact = scope.artifact || tag == "Artifact"
				if op == "BDC" {
					if len(args) != 2 {
						return errors.New("BDC lacks property list")
					}
					property := args[1]
					if name, ok := property.(types.Name); ok {
						props, e := s.DereferenceDict(resources["Properties"])
						if e != nil {
							return e
						}
						property = props[string(name)]
					}
					dict, e := s.DereferenceDict(property)
					if e != nil || dict == nil {
						return errors.New("BDC property list is invalid")
					}
					if dict["MCID"] != nil {
						id, e := s.DereferenceInteger(dict["MCID"])
						if e != nil || id == nil || id.Value() < 0 || id.Value() > 1_000_000 {
							return errors.New("invalid marked-content MCID")
						}
						number := owner.ObjectNumber.Value()
						if facts.ids[number] == nil {
							facts.ids[number] = map[int]bool{}
						}
						if facts.ids[number][id.Value()] {
							return errors.New("duplicate MCID within content owner")
						}
						facts.ids[number][id.Value()] = true
						container, e := s.Dereference(owner)
						if e != nil {
							return e
						}
						var dict types.Dict
						switch x := container.(type) {
						case types.Dict:
							dict = x
						case types.StreamDict:
							dict = x.Dict
						}
						key, e := s.DereferenceInteger(dict["StructParents"])
						if e != nil {
							return e
						}
						if key != nil {
							parents, e := s.DereferenceArray(inverse[key.Value()])
							if e != nil {
								return e
							}
							if id.Value() < len(parents) && parents[id.Value()] != nil {
								scope.semantic = true
								facts.covered = true
							}
						}
					}
				}
			case "EMC":
				if len(stack) == 0 {
					return errors.New("unbalanced EMC")
				}
				scope = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			case "BI":
				if !scope.artifact && !scope.semantic {
					facts.complete = false
				}
				budget := int64(len(rest))*32 + 1
				var err error
				rest, err = inlineTail(c, rest, &budget, 0)
				if err != nil {
					return err
				}
			case "Do":
				if len(args) != 1 {
					return errors.New("XObject invocation lacks a name")
				}
				name, ok := args[0].(types.Name)
				if !ok {
					return errors.New("invalid XObject name")
				}
				objects, e := s.DereferenceDict(resources["XObject"])
				if e != nil {
					return e
				}
				ref, ok := objects[string(name)].(types.IndirectRef)
				if !ok {
					return errors.New("missing XObject")
				}
				object, e := s.Dereference(ref)
				if e != nil {
					return e
				}
				stream, ok := object.(types.StreamDict)
				if !ok {
					return errors.New("XObject is not a stream")
				}
				subtype, e := nameValue(s, stream.Dict["Subtype"])
				if e != nil {
					return e
				}
				owned, e := objectOwned(stream.Dict)
				if e != nil {
					return e
				}
				if owned {
					facts.covered = true
				}
				if subtype == "Form" {
					number := ref.ObjectNumber.Value()
					if active[number] {
						return errors.New("cyclic marked-content Form")
					}
					active[number] = true
					res := resources
					if stream.Dict["Resources"] != nil {
						res, e = s.DereferenceDict(stream.Dict["Resources"])
						if e != nil {
							return e
						}
					}
					content, e := decodeDrawingStream(c, s, stream)
					if e != nil {
						return e
					}
					childOwner := owner
					if stream.Dict["StructParents"] != nil {
						childOwner = ref
					}
					childScope := scope
					childScope.semantic = childScope.semantic || owned
					if e = scan(content, res, childOwner, childScope, depth+1); e != nil {
						return e
					}
					delete(active, number)
				} else if !scope.artifact && !scope.semantic && !owned {
					facts.complete = false
				}
			case "Tj", "TJ", "'", "\"", "S", "s", "f", "F", "f*", "B", "B*", "b", "b*", "sh":
				if !scope.artifact && !scope.semantic {
					facts.complete = false
				}
			}
			args = args[:0]
		}
		if len(stack) != 0 {
			return errors.New("unclosed marked-content sequence")
		}
		return nil
	}
	contents, err := capturePageContent(c, s)
	if err != nil {
		return facts, err
	}
	for i, page := range pages {
		data, e := joinedPageContent(c, s, contents[i], i+1)
		if e != nil {
			return facts, e
		}
		if e = scan(data, page.attrs.Resources, page.ref, markedScope{}, 0); e != nil {
			return facts, e
		}
		annotations, e := s.DereferenceArray(page.dict["Annots"])
		if e != nil {
			return facts, e
		}
		for _, object := range annotations {
			annotation, e := s.DereferenceDict(object)
			if e != nil {
				return facts, e
			}
			if annotation == nil {
				continue
			}
			owned, e := objectOwned(annotation)
			if e != nil {
				return facts, e
			}
			if !owned && annotation["Subtype"] == types.Name("Popup") {
				parent, e := s.DereferenceDict(annotation["Parent"])
				if e != nil {
					return facts, e
				}
				owned, e = objectOwned(parent)
				if e != nil {
					return facts, e
				}
			}
			if owned {
				facts.covered = true
			} else {
				facts.complete = false
			}
		}
	}
	return facts, nil
}

func (f markedFacts) coverage() string {
	if !f.covered {
		return "none"
	}
	if f.complete {
		return "complete"
	}
	return "partial"
}
