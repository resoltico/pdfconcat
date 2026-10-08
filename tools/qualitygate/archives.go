// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
)

type (
	// archiveSet is what is known about the archives of one release directory.
	archiveSet struct {
		sums     map[string]string
		versions map[string]bool
		targets  map[string]bool
		files    map[string]bool
	}
	// archiveNamespace includes explicit and implied directories, before content maps discard directories.
	archiveNamespace  map[string]archiveMemberKind
	archiveMemberKind uint8

	// printedVersion contains the identity fields verified against the archive and executable metadata.
	printedVersion struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Date    string `json:"date"`
	}
)

const (
	archiveNamePrefix                        = "PDFConcat_"
	archiveZipExtension                      = ".zip"
	archiveRegularMember   archiveMemberKind = 0
	archiveDirectoryMember archiveMemberKind = 1

	// maxArchiveFile bounds one extracted file; the archives hold a ~12 MiB executable.
	maxArchiveFile = 128 << 20
	// One executable plus documents/licenses must fit this aggregate decoded bound.
	maxArchiveDecoded = 256 << 20
	archiveTailChunk  = 32 << 10

	// checksumFields is the number of fields of a checksums.txt line: digest and file name.
	checksumFields      = 2
	printedCommitLength = 12
	amd64Arch           = "amd64"
	arm64Arch           = "arm64"
)

var archiveName = regexp.MustCompile(`^` + archiveNamePrefix + `(.+)_(darwin|linux|windows)_(amd64|arm64)\.(tar\.gz|zip)$`)

// archiveTargets are the six release platforms.
func archiveTargets() []string {
	return []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"}
}

// inspectArchives checks the release archives in a directory (default dist): checksums, the six
// targets, names and version, and exactly the intended contents.
func inspectArchives(ctx context.Context, args []string) error {
	set := newFlags(archivesCommand)
	versionArg := set.String(
		"version-arg",
		versionVerb,
		"argument that makes the executable print its version, run for the host platform's archive",
	)

	err := set.Parse(args)
	if err != nil {
		return fmt.Errorf(parseFlagsError, err)
	}

	root, err := repoRoot()
	if err != nil {
		return err
	}

	dist := "dist"
	if set.NArg() > 0 {
		dist = set.Arg(0)
	}

	problems, err := archiveProblems(ctx, root, dist, *versionArg)
	if err != nil {
		return err
	}

	return report(archivesCommand, problems, "six archives, checksums, contents and version metadata verified in "+dist)
}

func archiveProblems(ctx context.Context, root, dist, versionArg string) ([]string, error) {
	sums, err := readChecksums(dist)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dist)
	if err != nil {
		return nil, fmt.Errorf(readFileError, dist, err)
	}

	found := archiveSet{sums: sums, versions: map[string]bool{}, targets: map[string]bool{}, files: map[string]bool{}}

	var problems []string

	for _, entry := range entries {
		match := archiveName.FindStringSubmatch(entry.Name())
		if match != nil {
			problems = append(problems, found.inspect(ctx, root, filepath.Join(dist, entry.Name()), match, versionArg)...)
		}
	}

	versionProblems, err := archiveVersionProblems(ctx, root, found.versions)
	if err != nil {
		return nil, err
	}

	problems = append(problems, versionProblems...)

	return append(problems, found.completeness()...), nil
}

// inspect checks one archive and records its version and target.
func (a *archiveSet) inspect(ctx context.Context, root, archivePath string, match []string, versionArg string) []string {
	version, goos, goarch := match[1], match[2], match[3]

	a.versions[version] = true
	a.files[filepath.Base(archivePath)] = true
	target := goos + "/" + goarch
	duplicate := a.targets[target]
	a.targets[target] = true

	problems := checksumProblems(archivePath, a.sums)
	if (goos == windowsOS) != strings.HasSuffix(archivePath, archiveZipExtension) {
		problems = append(problems, "archive format does not match "+target)
	}

	if duplicate {
		problems = append(problems, "duplicate target "+target)
	}

	files, err := readArchive(archivePath)
	if err != nil {
		return append(problems, err.Error())
	}

	problems = append(problems, contentProblems(root, filepath.Base(archivePath), goos, files)...)
	problems = append(problems, binaryProblems(ctx, root, archivePath, version, goos, goarch, files[executableName(goos)])...)

	if goos+"/"+goarch == runtime.GOOS+"/"+runtime.GOARCH {
		problems = append(problems, versionProblems(ctx, archivePath, version, versionArg, files)...)
	}

	return problems
}

// completeness reports missing targets, mixed versions and checksum lines for non-archives.
func (a *archiveSet) completeness() []string {
	var problems []string

	for _, target := range archiveTargets() {
		if !a.targets[target] {
			problems = append(problems, "no archive for "+target)
		}
	}

	if len(a.versions) != 1 {
		problems = append(
			problems,
			fmt.Sprintf("archives carry %d different versions %v, want one", len(a.versions), slices.Sorted(maps.Keys(a.versions))),
		)
	}

	for name := range a.sums {
		if !a.files[name] {
			problems = append(problems, "checksums.txt lists missing archive "+name)
		}

		if !archiveName.MatchString(name) {
			problems = append(problems, "checksums.txt lists "+name+", which is not a release archive")
		}
	}

	return problems
}

func readChecksums(dist string) (map[string]string, error) {
	content, err := readInRoot(dist, checksumFile)
	if err != nil {
		return nil, err
	}

	sums := map[string]string{}

	for line := range strings.SplitSeq(strings.TrimSpace(string(content)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != checksumFields {
			return nil, fmt.Errorf("%w: checksums.txt: malformed line %q", errGate, line)
		}

		if _, duplicate := sums[fields[1]]; duplicate {
			return nil, fmt.Errorf("%w: duplicate checksum %s", errGate, fields[1])
		}

		digest, decodeErr := hex.DecodeString(fields[0])
		if decodeErr != nil || len(digest) != sha256.Size || path.Base(fields[1]) != fields[1] {
			return nil, fmt.Errorf("%w: invalid checksum line %q", errGate, line)
		}

		sums[fields[1]] = fields[0]
	}

	return sums, nil
}

func checksumProblems(archivePath string, sums map[string]string) []string {
	content, err := readInRoot(filepath.Dir(archivePath), filepath.Base(archivePath))
	if err != nil {
		return []string{err.Error()}
	}

	digest := sha256.Sum256(content)
	name := filepath.Base(archivePath)

	want, listed := sums[name]

	switch {
	case !listed:
		return []string{name + " is not listed in checksums.txt"}
	case want != hex.EncodeToString(digest[:]):
		return []string{name + " does not match its checksums.txt digest"}
	default:
		return nil
	}
}

// readArchive returns the regular files of a .tar.gz or .zip archive by their archive path.
func readArchive(archivePath string) (map[string][]byte, error) {
	content, err := readInRoot(filepath.Dir(archivePath), filepath.Base(archivePath))
	if err != nil {
		return nil, err
	}

	if strings.HasSuffix(archivePath, archiveZipExtension) {
		return readZip(archivePath, content)
	}

	return readTarGz(archivePath, content)
}

func readTarGz(archivePath string, content []byte) (map[string][]byte, error) {
	compressed := bytes.NewReader(content)

	gzipReader, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, fmt.Errorf(pathError, archivePath, err)
	}

	defer closeLogged(gzipReader)

	gzipReader.Multistream(false)
	decoded := &io.LimitedReader{R: gzipReader, N: maxArchiveDecoded + 1}
	reader := tar.NewReader(decoded)
	files := map[string][]byte{}
	namespace := archiveNamespace{}

	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			if footerErr := finishGzipArchive(archivePath, decoded, compressed); footerErr != nil {
				return nil, footerErr
			}

			return namespace.finish(archivePath, files)
		}

		if nextErr != nil {
			return nil, fmt.Errorf(pathError, archivePath, nextErr)
		}

		if memberErr := readTarMember(archivePath, reader, header, files, namespace); memberErr != nil {
			return nil, memberErr
		}
	}
}

func readZip(archivePath string, content []byte) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf(pathError, archivePath, err)
	}

	files := map[string][]byte{}
	namespace := archiveNamespace{}
	total := uint64(0)

	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			if file.UncompressedSize64 != 0 || !namespace.accept(strings.TrimSuffix(file.Name, "/"), archiveDirectoryMember) {
				return nil, fmt.Errorf("%w: %s: unsafe directory %q", errGate, archivePath, file.Name)
			}

			continue
		}

		if !file.Mode().IsRegular() || !namespace.accept(file.Name, archiveRegularMember) || file.UncompressedSize64 > maxArchiveFile {
			return nil, fmt.Errorf("%w: %s: unsafe, duplicate or oversized member %q", errGate, archivePath, file.Name)
		}

		if file.UncompressedSize64 > uint64(maxArchiveDecoded)-total {
			return nil, fmt.Errorf("%w: %s: aggregate decoded archive limit exceeded", errGate, archivePath)
		}

		total += file.UncompressedSize64

		data, readErr := readZipMember(file)
		if readErr != nil {
			return nil, fmt.Errorf(pathError, archivePath, readErr)
		}

		files[file.Name] = data
	}

	return namespace.finish(archivePath, files)
}

func readZipMember(file *zip.File) ([]byte, error) {
	handle, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf(openFileError, file.Name, err)
	}

	data, readErr := io.ReadAll(io.LimitReader(handle, maxArchiveFile+1))
	if readErr != nil {
		readErr = fmt.Errorf(readFileError, file.Name, readErr)
	}

	if len(data) > maxArchiveFile {
		readErr = fmt.Errorf("%w: oversized archive member %s", errGate, file.Name)
	}

	return data, errors.Join(readErr, handle.Close())
}

// executableName is the release executable's name on an operating system.
func executableName(goos string) string {
	if goos == windowsOS {
		return "pdfconcat" + exeSuffix
	}

	return "pdfconcat"
}

// contentProblems checks that an archive holds the executable and the notices, licenses, schemas and
// contract documents byte-identical to the repository's, and nothing else.
func contentProblems(root, name, goos string, files map[string][]byte) []string {
	var problems []string

	executable := executableName(goos)

	requiredFiles := []string{
		executable, "LICENSE", "README.md", "THIRD_PARTY_NOTICES.md",
		"docs/CLI.md", "docs/PLAN.md", "docs/BLANK_PAGES.md",
	}
	for _, required := range requiredFiles {
		if len(files[required]) == 0 {
			problems = append(problems, name+": missing "+required)
		}
	}

	for _, member := range slices.Sorted(maps.Keys(files)) {
		allowed, mirrored := archiveMember(member, executable)

		switch {
		case !allowed:
			problems = append(problems, name+": unexpected file "+member)
		case mirrored != "":
			want, err := readInRoot(root, mirrored)
			if err != nil || !bytes.Equal(want, files[member]) {
				problems = append(problems, fmt.Sprintf("%s: %s does not equal the repository's %s", name, member, mirrored))
			}
		default:
		}
	}

	problems = append(problems, licenseCopyProblems(root, name, files)...)

	return append(problems, schemaProblems(root, name, files)...)
}

// archiveMember says whether a member path is allowed in an archive and, for files copied from the
// repository, which repository file it must equal.
func archiveMember(member, executable string) (bool, string) {
	directory, base := path.Split(member)

	switch {
	case member == executable:
		return true, ""
	case member == "LICENSE" || member == "README.md" || member == "THIRD_PARTY_NOTICES.md":
		return true, member
	case directory == "third_party/licenses/":
		return true, member
	case directory == "docs/" && strings.HasSuffix(base, ".md"):
		return true, member
	case directory == "schemas/" && strings.HasSuffix(base, ".schema.json"):
		return true, "" // The source directory varies; equality is checked against the repository's schema files.
	default:
		return false, ""
	}
}

// licenseCopyProblems requires every repository license text to be in the archive.
func licenseCopyProblems(root, name string, files map[string][]byte) []string {
	licenses, err := os.ReadDir(filepath.Join(root, "third_party", "licenses"))
	if err != nil {
		return []string{fmt.Sprintf("read third_party/licenses: %v", err)}
	}

	var problems []string

	for _, entry := range licenses {
		if _, found := files["third_party/licenses/"+entry.Name()]; !found {
			problems = append(problems, name+": missing third_party/licenses/"+entry.Name())
		}
	}

	for member := range files {
		if strings.HasPrefix(member, "docs/") {
			return problems
		}
	}

	return append(problems, name+": no contract document under docs/")
}

// schemaProblems requires every *.schema.json under internal/ to be in the archive, byte-identical.
func schemaProblems(root, name string, files map[string][]byte) []string {
	schemas := map[string][]byte{}

	walkErr := fs.WalkDir(os.DirFS(filepath.Join(root, "internal")), ".", func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(file, ".schema.json") {
			return err
		}

		content, readErr := readInRoot(filepath.Join(root, "internal"), file)
		schemas[entry.Name()] = content

		return readErr
	})
	if walkErr != nil {
		return []string{fmt.Sprintf("find schemas: %v", walkErr)}
	}

	if len(schemas) == 0 {
		return []string{"the repository has no *.schema.json under internal/ to ship"}
	}

	var problems []string

	for schema, content := range schemas {
		if !bytes.Equal(files["schemas/"+schema], content) {
			problems = append(problems, name+": schemas/"+schema+" is missing or differs from the repository's")
		}
	}

	return problems
}

// versionProblems extracts the executable of the host platform's archive and runs it: its version
// output must contain the version in the archive name.
func versionProblems(ctx context.Context, archivePath, version, versionArg string, files map[string][]byte) []string {
	executable := executableName(runtime.GOOS)

	dir, err := os.MkdirTemp("", "qualitygate-archive-")
	if err != nil {
		return []string{"create scratch directory: " + err.Error()}
	}

	defer removeAll(dir)

	target := filepath.Join(dir, executable)

	err = os.WriteFile(target, files[executable], execMode)
	if err != nil {
		return []string{"write executable: " + err.Error()}
	}

	var out bytes.Buffer

	err = (&command{dir: dir, name: target, args: []string{versionArg}, stdout: &out, stderr: &out}).run(ctx)
	if err != nil {
		return []string{fmt.Sprintf("%s: running %s %s failed: %v\n%s", archivePath, executable, versionArg, err, indent(out.String()))}
	}

	log.Printf("archives: host executable reports: %s", strings.TrimSpace(strings.SplitN(out.String(), "\n", firstLineParts)[0]))

	problems := reportedVersionProblems(archivePath, version, out.String(), files[executable])

	return problems
}

// versionOutputProblems requires the version command's output to carry the archive's version and a
// real commit and commit date, which the Go toolchain stamps into builds made inside a Git checkout
// (GoReleaser sets only main.version).
func versionOutputProblems(archivePath, version, output string) []string {
	return reportedVersionProblems(archivePath, version, output, nil)
}

func reportedVersionProblems(archivePath, version, output string, executable []byte) []string {
	var fields printedVersion
	if err := json.Unmarshal([]byte(output), &fields); err != nil {
		return []string{archivePath + ": invalid version JSON: " + err.Error()}
	}

	var problems []string
	if fields.Version != version {
		problems = append(problems, archivePath+": version output does not exactly match "+version)
	}

	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(fields.Commit) {
		problems = append(problems, archivePath+": invalid printed commit")
	}

	if date, err := time.Parse(time.RFC3339, fields.Date); err != nil || date.IsZero() {
		problems = append(problems, archivePath+": invalid printed commit date")
	}

	if executable != nil {
		problems = append(problems, printedIdentityProblems(archivePath, fields, executable)...)
	}

	return problems
}

func printedIdentityProblems(archivePath string, fields printedVersion, executable []byte) []string {
	info, err := buildinfo.Read(bytes.NewReader(executable))
	if err != nil {
		return []string{archivePath + ": unreadable executable build metadata: " + err.Error()}
	}

	var problems []string

	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			expected := setting.Value[:min(len(setting.Value), printedCommitLength)]
			if fields.Commit != expected {
				problems = append(problems, archivePath+": printed commit differs from executable metadata")
			}
		case "vcs.time":
			if fields.Date != setting.Value {
				problems = append(problems, archivePath+": printed date differs from executable metadata")
			}
		default:
		}
	}

	return problems
}

// accept rejects duplicate files, file/directory aliases, and files used as parents of another member.
func (n archiveNamespace) accept(name string, kind archiveMemberKind) bool {
	if !safeArchivePath(name) {
		return false
	}

	if prior, present := n[name]; present && (kind != archiveDirectoryMember || prior != archiveDirectoryMember) {
		return false
	}

	for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
		if prior, present := n[parent]; present && prior != archiveDirectoryMember {
			return false
		}
	}

	for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
		n[parent] = archiveDirectoryMember
	}

	n[name] = kind

	return true
}

// finish permits only directories implied by actual regular members, with no platform alias table.
func (n archiveNamespace) finish(archivePath string, files map[string][]byte) (map[string][]byte, error) {
	populated := map[string]bool{}

	for name := range files {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			populated[parent] = true
		}
	}

	for name, kind := range n {
		if kind == archiveDirectoryMember && !populated[name] {
			return nil, fmt.Errorf("%w: %s: directory %q has no regular-file descendant", errGate, archivePath, name)
		}
	}

	return files, nil
}

func safeArchivePath(name string) bool {
	return name != "." && !strings.ContainsAny(name, "\\:\x00") && path.Clean(name) == name &&
		!path.IsAbs(name) && name != ".." && !strings.HasPrefix(name, "../")
}

// binaryProblems checks the executable format independently of its embedded Go target metadata.
func binaryProblems(ctx context.Context, root, archivePath, version, goos, goarch string, content []byte) []string {
	var problems []string

	validHeader := binaryHeaderMatches(goos, goarch, content)
	if !validHeader {
		problems = append(problems, archivePath+": executable header does not match "+goos+"/"+goarch)
	}

	info, err := buildinfo.Read(bytes.NewReader(content))
	if err != nil {
		return append(problems, archivePath+": invalid Go build information: "+err.Error())
	}

	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}

	module, err := modulePath(root)
	if err != nil {
		return append(problems, err.Error())
	}

	gomod, err := readInRoot(root, moduleFileName)
	if err != nil {
		return append(problems, err.Error())
	}

	toolchain := regexp.MustCompile(`(?m)^go (\S+)$`).FindSubmatch(gomod)

	if info.Main.Path != module || info.Path != module+"/cmd/pdfconcat" {
		problems = append(problems, archivePath+": incorrect module/package identity")
	}

	if len(toolchain) != 2 || info.GoVersion != "go"+string(toolchain[1]) {
		problems = append(problems, archivePath+": incorrect Go toolchain "+info.GoVersion)
	}

	problems = append(problems, buildSettingsProblems(ctx, root, archivePath, version, goos, goarch, settings)...)

	return problems
}

// binaryHeaderMatches rejects renamed binaries, shared libraries and universal binaries.
func binaryHeaderMatches(goos, goarch string, content []byte) bool {
	switch goos {
	case "linux":
		return elfHeaderMatches(goarch, content)
	case "darwin":
		return machoHeaderMatches(goarch, content)
	case windowsOS:
		return peHeaderMatches(goarch, content)
	default:
		return false
	}
}

func elfHeaderMatches(goarch string, content []byte) bool {
	executable, err := elf.NewFile(bytes.NewReader(content))
	if err != nil {
		return false
	}
	defer closeLogged(executable)

	machines := map[string]elf.Machine{amd64Arch: elf.EM_X86_64, arm64Arch: elf.EM_AARCH64}

	return executable.Class == elf.ELFCLASS64 && executable.Type == elf.ET_EXEC && executable.Machine == machines[goarch]
}

func machoHeaderMatches(goarch string, content []byte) bool {
	executable, err := macho.NewFile(bytes.NewReader(content))
	if err != nil {
		return false
	}
	defer closeLogged(executable)

	machines := map[string]macho.Cpu{amd64Arch: macho.CpuAmd64, arm64Arch: macho.CpuArm64}

	return executable.Type == macho.TypeExec && executable.Cpu == machines[goarch]
}

func peHeaderMatches(goarch string, content []byte) bool {
	executable, err := pe.NewFile(bytes.NewReader(content))
	if err != nil {
		return false
	}
	defer closeLogged(executable)

	_, header64 := executable.OptionalHeader.(*pe.OptionalHeader64)
	machines := map[string]uint16{amd64Arch: pe.IMAGE_FILE_MACHINE_AMD64, arm64Arch: pe.IMAGE_FILE_MACHINE_ARM64}

	return header64 && executable.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE != 0 &&
		executable.Characteristics&pe.IMAGE_FILE_DLL == 0 && executable.Machine == machines[goarch]
}

func buildSettingsProblems(ctx context.Context, root, archivePath, version, goos, goarch string, settings map[string]string) []string {
	var problems []string
	if settings["GOOS"] != goos || settings["GOARCH"] != goarch {
		problems = append(problems, archivePath+": Go target does not match archive target")
	}

	validRevision := regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(settings["vcs.revision"])
	if !validRevision || settings["vcs.time"] == "" || settings["vcs.modified"] != "false" {
		problems = append(problems, archivePath+": missing or dirty VCS metadata")
	}

	revision, revisionErr := (&command{dir: root, name: gitTool, args: []string{"rev-parse", gitHead}}).output(ctx)
	if revisionErr != nil || strings.TrimSpace(revision) != settings["vcs.revision"] {
		problems = append(problems, archivePath+": VCS revision differs from the inspected source tree")
	}

	if !sourceTimeMatches(ctx, root, settings["vcs.time"]) {
		problems = append(problems, archivePath+": invalid VCS commit time")
	}

	problems = append(problems, releaseVersionProblems(archivePath, version, settings["-ldflags"])...)

	return problems
}

// readTarMember validates one archive member before retaining its bounded contents.
func readTarMember(archivePath string, reader *tar.Reader, header *tar.Header, files map[string][]byte, namespace archiveNamespace) error {
	if header.Typeflag == tar.TypeDir {
		if header.Size != 0 || !namespace.accept(strings.TrimSuffix(header.Name, "/"), archiveDirectoryMember) {
			return fmt.Errorf("%w: %s: unsafe directory %q", errGate, archivePath, header.Name)
		}

		return nil
	}

	if header.Typeflag != tar.TypeReg || !namespace.accept(header.Name, archiveRegularMember) || header.Size > maxArchiveFile {
		return fmt.Errorf("%w: %s: unsafe, duplicate or oversized member %q", errGate, archivePath, header.Name)
	}

	data, readErr := io.ReadAll(io.LimitReader(reader, maxArchiveFile+1))
	if readErr != nil {
		return fmt.Errorf(pathError, archivePath, readErr)
	}

	if len(data) > maxArchiveFile {
		return fmt.Errorf("%w: %s: oversized member", errGate, archivePath)
	}

	files[header.Name] = data

	return nil
}

func sourceTimeMatches(ctx context.Context, root, recorded string) bool {
	expected, err := (&command{dir: root, name: gitTool, args: []string{"show", "-s", "--format=%cI", gitHead}}).output(ctx)
	if err != nil {
		return false
	}

	sourceDate, sourceErr := time.Parse(time.RFC3339, strings.TrimSpace(expected))
	recordedDate, recordedErr := time.Parse(time.RFC3339, recorded)

	return sourceErr == nil && recordedErr == nil && recordedDate.Equal(sourceDate)
}

// finishGzipArchive consumes the gzip footer instead of mistaking tar's end marker for integrity proof.
func finishGzipArchive(name string, decoded *io.LimitedReader, compressed *bytes.Reader) error {
	buffer := make([]byte, archiveTailChunk)
	for {
		count, err := decoded.Read(buffer)
		if decoded.N == 0 {
			return fmt.Errorf("%w: %s: aggregate decoded archive limit exceeded", errGate, name)
		}

		for _, value := range buffer[:count] {
			if value != 0 {
				return fmt.Errorf("%w: %s: nonzero data after tar end marker", errGate, name)
			}
		}

		if errors.Is(err, io.EOF) {
			if compressed.Len() != 0 {
				return fmt.Errorf("%w: %s: trailing compressed bytes or concatenated gzip member", errGate, name)
			}

			return nil
		}

		if err != nil {
			return fmt.Errorf("%s: gzip footer: %w", name, err)
		}
	}
}
