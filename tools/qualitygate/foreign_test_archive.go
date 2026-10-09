// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"archive/tar"
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

type (
	foreignFixture struct {
		digest string
		size   int64
	}
	foreignTarBoundary struct {
		seen      map[string]byte
		prefix    string
		revision  string
		committed bool
	}
)

const (
	maxForeignTestDecoded = 512 << 20
	foreignGitSpecialMode = 0o7000
)

// stageForeignFixtures keeps candidate output private until every member and stream check succeeds.
func stageForeignFixtures(
	ctx context.Context,
	root, artifact, destination string,
	source repopolicy.ForeignSource,
) (map[string]foreignFixture, error) {
	base, err := foreignZIPDigests(ctx, filepath.Join(root, filepath.FromSlash(source.SourceArtifact)), source)
	if err != nil {
		return nil, err
	}

	output, err := os.OpenRoot(destination)
	if err != nil {
		return nil, fmt.Errorf("open owned fixture quarantine: %w", err)
	}
	defer closeLogged(output)

	fixtures, common := map[string]foreignFixture{}, map[string]bool{}

	err = validatedForeignStream(ctx, artifact, source.TestInputs.SHA256, func(reader *tar.Reader, stream *foreignDecodedStream) error {
		boundary := foreignTarBoundary{
			prefix:   path.Base(source.Repository) + "-" + source.Revision,
			revision: source.Revision,
			seen:     map[string]byte{},
		}

		visit := func(name string, header *tar.Header, body io.Reader) error {
			return recordQuarantinedFixture(ctx, output, name, header, body, base, fixtures, common)
		}
		if consumeErr := boundary.consume(reader, stream, visit); consumeErr != nil {
			return consumeErr
		}

		if !boundary.committed {
			return fmt.Errorf("%w: Git archive omits commit", errGate)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(common) != len(base) {
		return nil, fmt.Errorf("%w: Git artifact omitted module ZIP members", errGate)
	}

	if domainErr := validateFixtureDomains(fixtures, base); domainErr != nil {
		return nil, domainErr
	}

	return fixtures, nil
}

func quarantineForeignMember(
	ctx context.Context,
	output *os.Root,
	name string,
	header *tar.Header,
	body io.Reader,
	base map[string]foreignFixture,
) (foreignFixture, error) {
	digest := sha256.New()
	target := io.Writer(digest)

	var file *os.File

	if _, common := base[name]; !common {
		if supplementalProgramFile(name) || !strings.HasPrefix(name, "pkg/samples/") && !strings.HasPrefix(name, "pkg/testdata/") {
			return foreignFixture{}, fmt.Errorf("%w: forbidden supplemental input %s", errGate, name)
		}

		if err := output.MkdirAll(path.Dir(name), foreignStageDirectoryMode); err != nil {
			return foreignFixture{}, fmt.Errorf("create quarantined fixture directory: %w", err)
		}

		var err error

		file, err = output.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
		if err != nil {
			return foreignFixture{}, fmt.Errorf("create nonoverwriting quarantined fixture: %w", err)
		}

		target = io.MultiWriter(file, digest)
	}

	size, copyErr := io.Copy(target, &foreignContextReader{cancellation: ctx.Err, source: body})
	if file != nil {
		copyErr = errors.Join(copyErr, file.Close())
	}

	if copyErr != nil {
		return foreignFixture{}, fmt.Errorf("copy/hash quarantined member: %w", copyErr)
	}

	if size != header.Size {
		return foreignFixture{}, fmt.Errorf("%w: fixture member size differs from header", errGate)
	}

	return foreignFixture{digest: hex.EncodeToString(digest.Sum(nil)), size: size}, nil
}

func foreignZIPDigests(ctx context.Context, name string, source repopolicy.ForeignSource) (map[string]foreignFixture, error) {
	archive, err := zip.OpenReader(name)
	if err != nil {
		return nil, fmt.Errorf("open verified module ZIP: %w", err)
	}
	defer closeLogged(archive)

	files := map[string]foreignFixture{}
	prefix := source.Module + "@" + source.Version + "/"

	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() {
			continue
		}

		if !strings.HasPrefix(entry.Name, prefix) {
			return nil, fmt.Errorf("%w: invalid module ZIP prefix", errGate)
		}

		reader, openErr := entry.Open()
		if openErr != nil {
			return nil, fmt.Errorf("open ZIP member: %w", openErr)
		}

		digest := sha256.New()
		size, copyErr := io.Copy(
			digest,
			io.LimitReader(&foreignContextReader{cancellation: ctx.Err, source: reader}, maxForeignTestDecoded+1),
		)

		closeErr := reader.Close()
		if copyErr != nil || closeErr != nil {
			return nil, fmt.Errorf("read ZIP member: %w", errors.Join(copyErr, closeErr))
		}

		if size > maxForeignTestDecoded {
			return nil, fmt.Errorf("%w: ZIP member exceeds decoded limit", errGate)
		}

		files[strings.TrimPrefix(entry.Name, prefix)] = foreignFixture{digest: hex.EncodeToString(digest.Sum(nil)), size: size}
	}

	return files, nil
}

func (boundary *foreignTarBoundary) member(header *tar.Header) (string, error) {
	if header.Typeflag == tar.TypeXGlobalHeader {
		return "", boundary.commit(header)
	}

	name := strings.TrimSuffix(header.Name, "/")
	if name == boundary.prefix && header.Typeflag == tar.TypeDir {
		return "", boundary.register(name, header.Typeflag)
	}

	if !strings.HasPrefix(name, boundary.prefix+"/") {
		return "", fmt.Errorf("%w: foreign Git member prefix mismatch", errGate)
	}

	name = strings.TrimPrefix(name, boundary.prefix+"/")
	if !safeForeignArchiveName(name) {
		return "", fmt.Errorf("%w: unsafe foreign Git member %q", errGate, name)
	}

	if err := boundary.register(name, header.Typeflag); err != nil {
		return "", err
	}

	if header.Typeflag == tar.TypeDir {
		return "", nil
	}

	if header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > maxForeignTestDecoded || header.Mode&foreignGitSpecialMode != 0 {
		return "", fmt.Errorf("%w: nonregular foreign Git member %s", errGate, name)
	}

	return name, nil
}

func (boundary *foreignTarBoundary) commit(header *tar.Header) error {
	if boundary.committed || header.PAXRecords["comment"] != boundary.revision {
		return fmt.Errorf("%w: foreign Git commit mismatch", errGate)
	}

	boundary.committed = true

	return nil
}

func safeForeignArchiveName(name string) bool {
	if name == "." || name == ".." || name == "" || path.IsAbs(name) || path.Clean(name) != name {
		return false
	}

	if strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\:") || !utf8.ValidString(name) {
		return false
	}

	for component := range strings.SplitSeq(name, "/") {
		if !safeForeignArchiveComponent(component) {
			return false
		}
	}

	return true
}

func safeForeignArchiveComponent(component string) bool {
	if strings.TrimRight(component, " .") != component {
		return false
	}

	base, _, _ := strings.Cut(strings.ToUpper(component), ".")
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return false
	default:
	}

	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return false
	}

	return true
}

func (boundary *foreignTarBoundary) register(name string, kind byte) error {
	folded := strings.ToLower(norm.NFC.String(name))
	if _, exists := boundary.seen[folded]; exists {
		return fmt.Errorf("%w: duplicate/colliding foreign Git member %s", errGate, name)
	}

	for parent := path.Dir(folded); parent != "."; parent = path.Dir(parent) {
		if existing, present := boundary.seen[parent]; present && existing != tar.TypeDir {
			return fmt.Errorf("%w: archive file/ancestor collision %s", errGate, name)
		}
	}

	if kind != tar.TypeDir {
		for existing := range boundary.seen {
			if strings.HasPrefix(existing, folded+"/") {
				return fmt.Errorf("%w: archive parent/file collision %s", errGate, name)
			}
		}
	}

	boundary.seen[folded] = kind

	return nil
}

func validateFixtureDomains(fixtures, base map[string]foreignFixture) error {
	roots := map[string]bool{}

	for name := range fixtures {
		if path.Base(name) == moduleFileName {
			roots[path.Dir(name)] = true
		}
	}

	if len(roots) != 2 || !roots["pkg/samples"] || !roots["pkg/testdata"] {
		return fmt.Errorf("%w: unexpected omitted nested fixture modules", errGate)
	}

	for name := range fixtures {
		if supplementalProgramFile(name) {
			return fmt.Errorf("%w: supplemental program source %s", errGate, name)
		}

		if !strings.HasPrefix(name, "pkg/samples/") && !strings.HasPrefix(name, "pkg/testdata/") {
			return fmt.Errorf("%w: supplemental file outside nested fixture modules: %s", errGate, name)
		}

		if err := fixtureAncestorCollisions(name, fixtures, base); err != nil {
			return err
		}
	}

	return nil
}

func fixtureAncestorCollisions(name string, fixtures, base map[string]foreignFixture) error {
	for ancestor := name; ancestor != "."; ancestor = path.Dir(ancestor) {
		if _, exists := base[ancestor]; exists {
			return fmt.Errorf("%w: fixture overwrites canonical input/ancestor %s", errGate, ancestor)
		}

		if ancestor != name {
			if _, exists := fixtures[ancestor]; exists {
				return fmt.Errorf("%w: fixture file/ancestor collision %s", errGate, ancestor)
			}
		}
	}

	return nil
}

func supplementalProgramFile(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".go", ".c", ".h", ".cc", ".cpp", ".cxx", ".m", ".mm", ".f", ".s", ".syso", ".swig", ".swigcxx":
		return true
	default:
		return false
	}
}

func (boundary *foreignTarBoundary) consume(
	reader *tar.Reader,
	stream *foreignDecodedStream,
	visit func(string, *tar.Header, io.Reader) error,
) error {
	var padding int64

	for {
		before := stream.consumed

		header, nextErr := reader.Next()
		if nextErr == io.EOF {
			return stream.footer(before, padding)
		}

		if nextErr != nil {
			return fmt.Errorf("read foreign Git tar: %w", nextErr)
		}

		if shapeErr := validateForeignTarShape(header); shapeErr != nil {
			return shapeErr
		}

		name, memberErr := boundary.member(header)
		if memberErr != nil {
			return memberErr
		}

		padding = (foreignTarBlockBytes - header.Size%foreignTarBlockBytes) % foreignTarBlockBytes
		if name == "" {
			padding = 0
			continue
		}

		if err := visit(name, header, reader); err != nil {
			return err
		}
	}
}

func recordQuarantinedFixture(
	ctx context.Context,
	output *os.Root,
	name string,
	header *tar.Header,
	body io.Reader,
	base, fixtures map[string]foreignFixture,
	common map[string]bool,
) error {
	value, err := quarantineForeignMember(ctx, output, name, header, body, base)
	if err != nil {
		return err
	}

	if original, exists := base[name]; exists {
		if original != value {
			return fmt.Errorf("%w: Git/ZIP shared bytes differ: %s", errGate, name)
		}

		common[name] = true
	} else {
		fixtures[name] = value
	}

	return nil
}

func validateForeignTarShape(header *tar.Header) error {
	if header.Typeflag == tar.TypeDir && header.Size != 0 {
		return fmt.Errorf("%w: nonzero Git directory body", errGate)
	}

	for key := range header.PAXRecords {
		if strings.HasPrefix(key, "GNU.sparse.") {
			return fmt.Errorf("%w: sparse fixture metadata unsupported", errGate)
		}
	}

	if header.Typeflag == tar.TypeGNUSparse {
		return fmt.Errorf("%w: sparse Git member unsupported", errGate)
	}

	return nil
}
