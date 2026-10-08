// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdforacle

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

type (
	failFunc func(check, format string, args ...any)

	// destinationPage classifies a destination: the output page index it reaches, or why it does not.
	destinationPage struct {
		kind  string // "ok", "null", "orphan", "none", "missing"
		index int
	}

	// linkChecker accumulates link destination findings.
	linkChecker struct {
		doc       *Document
		names     map[string][]any
		pageIndex map[string]int
		exp       Expectation

		dangling, orphan, crossed, wrongPage []string
	}

	// formWidget is a widget annotation and the index of the page that lists it.
	formWidget struct {
		ref  any
		page int
	}
)

const (
	// sampleLimit is how many example items a finding quotes.
	sampleLimit = 3
	// textSampleLimit is how many mismatching page numbers a text finding quotes.
	textSampleLimit = 8
	// geometrySampleLimit is how many mismatching attributes a geometry finding quotes.
	geometrySampleLimit = 4

	// maxNameTreeDepth bounds the recursion through a destination name tree.
	maxNameTreeDepth = 20
	// maxOutlineDepth bounds the recursion through an outline tree.
	maxOutlineDepth = 50
	// maxFieldDepth bounds how far a form field's parents or kids are followed.
	maxFieldDepth = 30

	// boxTolerance is how far a box coordinate may differ from the expected one.
	boxTolerance = 1e-6
	// unitTolerance is how far a UserUnit may differ from the expected one.
	unitTolerance = 1e-9
	// fullTurn is the number of degrees in a rotation's period.
	fullTurn        = 360
	pageLabelFormat = "page %d"
)

// sample returns at most limit leading items, to quote in a finding.
func sample[T any](items []T, limit int) []T { return items[:min(len(items), limit)] }

// Verify runs every check against exp and returns the failures; no findings means the output matches.
func (d *Document) Verify(exp Expectation) []Finding {
	var findings []Finding

	fail := func(check, format string, args ...any) {
		findings = append(findings, Finding{Check: check, Detail: fmt.Sprintf(format, args...)})
	}

	if err := d.tools.Check(d.Path); err != nil {
		fail(CheckStructure, "%v", err)
	}

	if len(d.pages) != len(exp.Pages) {
		fail(CheckPageCount, "%d pages, expected %d", len(d.pages), len(exp.Pages))
	}

	d.checkText(exp, fail)

	pageIndex := map[string]int{}
	for index, ref := range d.pages {
		if _, seen := pageIndex[ref]; !seen {
			pageIndex[ref] = index
		}
	}

	if len(pageIndex) != len(d.pages) {
		fail(CheckDistinctPages, "%d page objects reused", len(d.pages)-len(pageIndex))
	}

	pageDicts := make([]map[string]any, len(d.pages))
	annots := make([][]any, len(d.pages))
	owners := map[string][]int{}

	for index, page := range d.pages {
		pageDicts[index] = d.dict(page)
		annots[index] = d.annotationsOf(pageDicts[index])

		for _, annot := range annots[index] {
			if ref, isRef := reference(annot); isRef {
				owners[ref] = append(owners[ref], index)
			}
		}
	}

	d.checkAnnotations(annots, owners, fail)

	names := d.namedDestinations()
	links := d.newLinkChecker(exp, names, pageIndex)
	links.run(pageDicts, annots)
	links.report(fail)

	d.checkNamed(names, pageIndex, fail)
	d.checkForms(annots, owners, fail)
	d.checkGeometry(exp, pageDicts, fail)
	d.checkVersion(exp, fail)
	d.checkOutlines(pageIndex, fail)
	d.checkOrphans(pageIndex, fail)

	return findings
}

func (d *Document) checkText(exp Expectation, fail failFunc) {
	var bad []int

	for index, page := range exp.Pages {
		if d.FirstLine(index+1) != page.Text {
			bad = append(bad, index+1)
		}
	}

	if len(bad) > 0 {
		fail(CheckPageText, "mismatching pages %v (of %d)", sample(bad, textSampleLimit), len(bad))
	}
}

// annotationsOf lists a page's /Annots entries, which may be a direct or indirect array.
func (d *Document) annotationsOf(page map[string]any) []any {
	if page == nil {
		return nil
	}

	return d.list(page["/Annots"])
}

func (d *Document) checkAnnotations(annots [][]any, owners map[string][]int, fail failFunc) {
	shared := 0

	for _, pages := range owners {
		if len(pages) > 1 {
			shared++
		}
	}

	if shared > 0 {
		fail(CheckAnnotMembership, "%d annotation objects are listed on several pages", shared)
	}

	var wrong []string

	for index, list := range annots {
		for _, annot := range list {
			dict := d.dict(annot)
			if parent, found := dict["/P"]; found && parent != d.pages[index] {
				wrong = append(wrong, fmt.Sprintf("page %d has /P %v", index+1, parent))
			}
		}
	}

	if len(wrong) > 0 {
		fail(CheckAnnotParent, "%d annotations with /P elsewhere, e.g. %v", len(wrong), sample(wrong, sampleLimit))
	}
}

// namedDestinations collects destination names from the name tree and the legacy /Dests dictionary.
func (d *Document) namedDestinations() map[string][]any {
	names := map[string][]any{}

	var collect func(node any, depth int)

	collect = func(node any, depth int) {
		dict := d.dict(node)
		if dict == nil || depth > maxNameTreeDepth {
			return
		}

		entries := d.list(dict["/Names"])
		for index := 0; index+1 < len(entries); index += 2 {
			key := trimStringPrefix(asString(entries[index]))
			names[key] = append(names[key], entries[index+1])
		}

		for _, kid := range d.list(dict["/Kids"]) {
			collect(kid, depth+1)
		}
	}

	root := d.catalog()
	if tree := d.dict(root["/Names"]); tree != nil {
		collect(tree["/Dests"], 0)
	}

	for key, value := range d.dict(root["/Dests"]) {
		key = strings.TrimPrefix(key, "/")
		names[key] = append(names[key], value)
	}

	return names
}

func trimStringPrefix(text string) string {
	if strings.HasPrefix(text, "u:") || strings.HasPrefix(text, "b:") {
		return text[2:]
	}

	return text
}

func (d *Document) destinationPage(dest any, pageIndex map[string]int) destinationPage {
	dest = d.deref(dest)
	if dict, isDict := dest.(map[string]any); isDict {
		if inner, found := dict["/D"]; found {
			dest = d.deref(inner)
		}
	}

	array, isArray := dest.([]any)
	if !isArray || len(array) == 0 {
		return destinationPage{kind: "none"}
	}

	if array[0] == nil {
		return destinationPage{kind: "null"}
	}

	ref, isRef := reference(array[0])
	if !isRef {
		return destinationPage{kind: "none"}
	}

	if index, found := pageIndex[ref]; found {
		return destinationPage{kind: "ok", index: index}
	}

	return destinationPage{kind: "orphan"}
}

func (d *Document) resolveName(name string, names map[string][]any, pageIndex map[string]int) destinationPage {
	key := strings.TrimPrefix(trimStringPrefix(name), "/")

	values := names[key]
	if len(values) == 0 {
		return destinationPage{kind: "missing"}
	}

	return d.destinationPage(values[0], pageIndex)
}

func (d *Document) checkNamed(names map[string][]any, pageIndex map[string]int, fail failFunc) {
	var bad []string

	for key, values := range names {
		for _, value := range values {
			if result := d.destinationPage(value, pageIndex); result.kind != "ok" {
				bad = append(bad, key+":"+result.kind)
			}
		}
	}

	if len(bad) > 0 {
		slices.Sort(bad)
		fail(CheckNamedDests, "%d names do not reach an output page, e.g. %v", len(bad), sample(bad, sampleLimit))
	}
}

func (d *Document) newLinkChecker(exp Expectation, names map[string][]any, pageIndex map[string]int) *linkChecker {
	return &linkChecker{doc: d, exp: exp, names: names, pageIndex: pageIndex}
}

// run checks the destination of every link annotation and every page-level GoTo action.
func (l *linkChecker) run(pages []map[string]any, annots [][]any) {
	for index, list := range annots {
		for _, annot := range list {
			if dest, found := l.linkDestination(l.doc.dict(annot)); found {
				l.check(index, dest)
			}
		}
	}

	for index, page := range pages {
		for _, action := range l.doc.dict(page["/AA"]) {
			if dest, found := goToDestination(l.doc.dict(action)); found {
				l.check(index, dest)
			}
		}
	}
}

// linkDestination returns the destination of a link annotation, given directly or by a GoTo action.
func (l *linkChecker) linkDestination(annot map[string]any) (any, bool) {
	if annot["/Subtype"] != "/Link" {
		return nil, false
	}

	if dest, found := annot["/Dest"]; found {
		return dest, true
	}

	return goToDestination(l.doc.dict(annot["/A"]))
}

// goToDestination returns the destination of a GoTo action dictionary.
func goToDestination(action map[string]any) (any, bool) {
	if action["/S"] != "/GoTo" {
		return nil, false
	}

	dest, found := action["/D"]

	return dest, found
}

func (l *linkChecker) check(index int, dest any) {
	var target destinationPage

	if name, isName := l.doc.deref(dest).(string); isName {
		target = l.doc.resolveName(name, l.names, l.pageIndex)
	} else {
		target = l.doc.destinationPage(dest, l.pageIndex)
	}

	label := fmt.Sprintf(pageLabelFormat, index+1)

	switch target.kind {
	case "null", "missing":
		l.dangling = append(l.dangling, label+" "+target.kind)
	case "orphan":
		l.orphan = append(l.orphan, label)
	case "ok":
		l.checkTarget(index, target.index, label)
	default:
	}
}

func (l *linkChecker) checkTarget(index, targetIndex int, label string) {
	if index >= len(l.exp.Pages) || targetIndex >= len(l.exp.Pages) {
		return
	}

	origin, reached := l.exp.Pages[index], l.exp.Pages[targetIndex]
	if origin.Source == "" {
		return
	}

	if reached.Source != origin.Source || reached.Occurrence != origin.Occurrence {
		l.crossed = append(
			l.crossed,
			fmt.Sprintf("%s -> output page %d (%s#%d p%d)", label, targetIndex+1, reached.Source, reached.Occurrence, reached.SourcePage),
		)

		return
	}

	want := l.exp.Sources[origin.Source].Links[origin.SourcePage]
	if len(want) > 0 && !slices.Contains(want, reached.SourcePage) {
		l.wrongPage = append(l.wrongPage, fmt.Sprintf("%s -> source page %d", label, reached.SourcePage))
	}
}

func (l *linkChecker) report(fail failFunc) {
	for _, entry := range []struct {
		check string
		items []string
	}{
		{CheckLinkDangling, l.dangling},
		{CheckLinkInTree, l.orphan},
		{CheckLinkOccurrence, l.crossed},
		{CheckLinkPage, l.wrongPage},
	} {
		if len(entry.items) > 0 {
			fail(entry.check, "%d, e.g. %v", len(entry.items), sample(entry.items, sampleLimit))
		}
	}
}

func (d *Document) checkForms(annots [][]any, owners map[string][]int, fail failFunc) {
	form := d.dict(d.catalog()["/AcroForm"])
	widgets := d.widgetsOf(annots)

	if form == nil && len(widgets) == 0 {
		return
	}

	fields := d.list(form["/Fields"])
	onPage := d.checkFieldRoots(widgets, fields, fail)

	dead := 0

	for _, field := range fields {
		if isReference(field) && !d.reachesWidget(field, onPage, 0) {
			dead++
		}
	}

	if dead > 0 {
		fail(CheckFormWidgets, "%d /Fields entries with no widget on an output page", dead)
	}

	shared := 0

	for _, widget := range widgets {
		if ref, isRef := widget.ref.(string); isRef && len(owners[ref]) > 1 {
			shared++
		}
	}

	if shared > 0 {
		fail(CheckFormWidgetsShared, "%d widget objects on several pages", shared)
	}
}

// widgetsOf lists the widget annotations of the pages, with the index of the page that lists each.
func (d *Document) widgetsOf(annots [][]any) []formWidget {
	var widgets []formWidget

	for index, list := range annots {
		for _, annot := range list {
			if d.dict(annot)["/Subtype"] == "/Widget" {
				widgets = append(widgets, formWidget{ref: annot, page: index})
			}
		}
	}

	return widgets
}

// checkFieldRoots reports widgets whose field root is not listed in fields or whose /Parent chain is
// broken, and returns the set of widget objects that sit on a page.
func (d *Document) checkFieldRoots(widgets []formWidget, fields []any, fail failFunc) map[any]bool {
	fieldSet := map[any]bool{}

	for _, field := range fields {
		if isReference(field) {
			fieldSet[field] = true
		}
	}

	var unrooted, danglingParents []string

	onPage := map[any]bool{}

	for _, widget := range widgets {
		onPage[widget.ref] = true
		top, broken := d.fieldRoot(widget.ref)

		switch {
		case broken:
			danglingParents = append(danglingParents, fmt.Sprintf(pageLabelFormat, widget.page+1))
		case !fieldSet[top]:
			unrooted = append(unrooted, fmt.Sprintf(pageLabelFormat, widget.page+1))
		default:
		}
	}

	if len(unrooted) > 0 {
		fail(CheckFormRoots, "%d widgets whose field root is not in /Fields, e.g. %v", len(unrooted), sample(unrooted, sampleLimit))
	}

	if len(danglingParents) > 0 {
		fail(CheckFormParents, "%d dangling /Parent, e.g. %v", len(danglingParents), sample(danglingParents, sampleLimit))
	}

	return onPage
}

// fieldRoot climbs /Parent links from a widget and returns the topmost field; broken is true when a
// parent does not resolve.
func (d *Document) fieldRoot(widget any) (any, bool) {
	current := widget

	for range maxFieldDepth {
		parent := d.dict(current)["/Parent"]
		if parent == nil {
			return current, false
		}

		if d.deref(parent) == nil {
			return nil, true
		}

		current = parent
	}

	return current, false
}

func (d *Document) reachesWidget(field any, onPage map[any]bool, depth int) bool {
	if onPage[field] {
		return true
	}

	if depth > maxFieldDepth {
		return false
	}

	for _, kid := range d.list(d.dict(field)["/Kids"]) {
		if d.reachesWidget(kid, onPage, depth+1) {
			return true
		}
	}

	return false
}

func sameBox(got any, want []float64) bool {
	array, isArray := got.([]any)
	if !isArray || len(array) != len(want) {
		return false
	}

	for index, value := range array {
		number, isNumber := value.(float64)
		if !isNumber || math.Abs(number-want[index]) > boxTolerance {
			return false
		}
	}

	return true
}

func (d *Document) checkGeometry(exp Expectation, pages []map[string]any, fail failFunc) {
	bad := make([]string, 0, len(pages))

	for index, page := range pages {
		if index >= len(exp.Pages) {
			break
		}

		want := exp.Pages[index].Geometry
		if want == nil && exp.Pages[index].Source != "" {
			if geometry, found := exp.Sources[exp.Pages[index].Source].Geometry[exp.Pages[index].SourcePage]; found {
				want = &geometry
			}
		}

		if want == nil {
			continue
		}

		bad = append(bad, d.geometryMismatches(index+1, page, want)...)
	}

	if len(bad) > 0 {
		fail(CheckGeometry, "%d, e.g. %v", len(bad), sample(bad, geometrySampleLimit))
	}
}

func (d *Document) geometryMismatches(number int, page map[string]any, want *Geometry) []string {
	var bad []string

	if !sameBox(d.effective(page, "/MediaBox"), want.MediaBox) {
		bad = append(bad, fmt.Sprintf("page %d MediaBox %v", number, d.effective(page, "/MediaBox")))
	}

	crop := d.effective(page, "/CropBox")
	if want.CropBox == nil && crop != nil || want.CropBox != nil && !sameBox(crop, want.CropBox) {
		bad = append(bad, fmt.Sprintf("page %d CropBox %v", number, crop))
	}

	rotate := asNumber(d.effective(page, "/Rotate"))
	if (int(rotate)%fullTurn+fullTurn)%fullTurn != want.Rotate {
		bad = append(bad, fmt.Sprintf("page %d Rotate %v", number, rotate))
	}

	units := 1.0
	if value, isNumber := d.deref(page["/UserUnit"]).(float64); isNumber {
		units = value
	}

	wantUnits := want.UserUnit
	if wantUnits == 0 {
		wantUnits = 1
	}

	if math.Abs(units-wantUnits) > unitTolerance {
		bad = append(bad, fmt.Sprintf("page %d UserUnit %v", number, units))
	}

	return bad
}

func (d *Document) checkVersion(exp Expectation, fail failFunc) {
	need := 0

	for _, page := range exp.Pages {
		if page.Source != "" {
			need = max(need, exp.Sources[page.Source].Version)
		}
	}

	if got := d.EffectiveVersion(); got < need {
		fail(CheckVersion, "output %d < required %d", got, need)
	}
}

func (d *Document) checkOutlines(pageIndex map[string]int, fail failFunc) {
	root := d.catalog()
	if root["/Outlines"] == nil {
		return
	}

	bad := 0

	var walk func(node any, depth int)

	walk = func(node any, depth int) {
		for current := d.dict(node); current != nil && depth < maxOutlineDepth; current = d.dict(current["/Next"]) {
			if dest, found := current["/Dest"]; found && d.destinationPage(dest, pageIndex).kind != "ok" {
				bad++
			}

			if first, found := current["/First"]; found {
				walk(first, depth+1)
			}
		}
	}

	walk(root["/Outlines"], 0)

	if bad > 0 {
		fail(CheckOutlines, "%d outline items dangling or orphaned", bad)
	}
}

func (d *Document) checkOrphans(pageIndex map[string]int, fail failFunc) {
	orphans := 0

	for ref, object := range d.objects {
		value := asMap(asMap(object)["value"])
		if value["/Type"] == "/Page" {
			if _, inTree := pageIndex[ref]; !inTree {
				orphans++
			}
		}
	}

	if orphans > 0 {
		fail(CheckOrphanPages, "%d /Type /Page objects outside the page tree", orphans)
	}
}
