// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type destinationValue struct {
	page     int
	operands string
}

type navigationOwner struct {
	dict any
	key  string
}

func ownerOf(d types.Dict, key string) navigationOwner {
	return navigationOwner{reflect.ValueOf(d).UnsafePointer(), key}
}

type navigationObservation struct {
	named    map[string]namedDestinationValue
	bindings []observedNavigation
}

type namedDestinationValue struct {
	destinationValue
	legacy bool
}

type observedNavigation struct {
	value     destinationValue
	name      string
	symbolic  bool
	nameToken bool
}

func (d explicitDestination) value(page int) destinationValue {
	return destinationValue{page: page, operands: d.tail().PDFString()}
}

func observeNavigation(c context.Context, s *model.Context, named map[string]destination, target func(navigationOwner, observedNavigation) error) error {
	origins, err := sourceOriginsFor(c, s)
	if err != nil {
		return err
	}
	return walkRetained(c, s, origins, nil, func(binding retainedTarget) (bool, error) {
		o, site := binding.value, binding.site
		if o == nil {
			return true, nil
		}
		var observed observedNavigation
		if array, ok := o.(types.Array); ok {
			d, e := readExplicitDestination(s, array)
			if e != nil {
				return false, site.wrap(e)
			}
			observed.value = d.value(origins.pages[d.page])
			if observed.value.page == 0 {
				return false, site.wrap(errors.New("local navigation target is not a physical page of the document"))
			}
		} else {
			name, e := destinationName(o)
			if e != nil {
				return false, site.wrap(e)
			}
			d, ok := named[name]
			if !ok {
				return false, site.wrap(errors.New("local navigation has no named destination definition"))
			}
			_, isName := o.(types.Name)
			observed = observedNavigation{nameToken: isName, value: destinationValue{page: d.page, operands: d.tail.PDFString()}, name: name, symbolic: true}
		}
		return true, target(ownerOf(binding.owner, binding.key), observed)
	}, nil, false)
}

func captureOutputNavigation(c context.Context, s *model.Context, imported map[navigationOwner]observedNavigation, definitions map[string]destination) (navigationObservation, error) {
	named, err := readDestinations(c, s)
	if err != nil {
		return navigationObservation{}, err
	}
	result := navigationObservation{named: map[string]namedDestinationValue{}}
	for name, d := range named {
		if d.page < 1 {
			return navigationObservation{}, errors.New("named destination target is not a physical page of the document")
		}
		result.named[name] = namedDestinationValue{destinationValue{page: d.page, operands: d.tail.PDFString()}, d.legacy}
	}
	if imported != nil {
		if len(named) != len(definitions) {
			return navigationObservation{}, errors.New("composed named destination definitions differ from the imported definitions")
		}
		for name, d := range definitions {
			if result.named[name] != (namedDestinationValue{destinationValue{page: d.page, operands: d.tail.PDFString()}, d.legacy}) {
				return navigationObservation{}, errors.New("composed named destination differs from its imported target")
			}
		}
	}
	remaining := maps.Clone(imported)
	err = observeNavigation(c, s, named, func(owner navigationOwner, found observedNavigation) error {
		if expected, known := imported[owner]; known {
			if expected != found {
				return errors.New("composed local destination differs from its occurrence target")
			}
			delete(remaining, owner)
		}
		result.bindings = append(result.bindings, found)
		return nil
	})
	if err == nil && len(remaining) != 0 {
		err = errors.New("composed local navigation lost an imported binding")
	}
	return result, err
}

func (expected navigationObservation) verify(c context.Context, s *model.Context) error {
	actual, err := captureOutputNavigation(c, s, nil, nil)
	if err != nil {
		return err
	}
	if !maps.Equal(expected.named, actual.named) || !slices.Equal(expected.bindings, actual.bindings) {
		return errors.New("reopened local navigation differs from the composed destinations")
	}
	return nil
}
