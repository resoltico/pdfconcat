// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package capture

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

type (
	// Role is what a file is used for in one job. A file may not hold two different roles.
	Role int

	// AliasError reports two different roles naming the same file.
	AliasError struct {
		// Path is the resource being added.
		Path string
		// OtherPath is the earlier resource it collides with.
		OtherPath string
		// Role is the role of the resource being added.
		Role Role
		// OtherRole is the role of the earlier resource.
		OtherRole Role
	}

	// ArtifactTargetError reports an output or report path that exists but cannot be replaced safely.
	ArtifactTargetError struct {
		// Path is the path the caller named.
		Path string
		// Problem says what is wrong with the existing entry.
		Problem string
		// Role is the role the path was given.
		Role Role
	}

	claim struct {
		path string
		role Role
	}

	// Registry records every resource of a job and rejects a file used in two roles. Existing files
	// are matched by filesystem Identity, so hard links, symbolic links (resolved for every role but
	// output and report, which may not be symbolic links) and case aliases collide; files that do not
	// exist yet are conservatively matched by their directory identity and Unicode-normalized,
	// case-folded basename, including on case-sensitive filesystems. Lookup cost is one map access per resource, never all pairs.
	// A Registry is safe for concurrent use.
	Registry struct {
		byIdentity map[Identity]claim
		byName     map[Identity]map[string]claim
		mu         sync.Mutex
	}
)

// The roles a job's files can hold.
const (
	// RolePlan is the plan file (not used for inline or stdin plans).
	RolePlan Role = iota + 1
	// RoleSource is an input PDF.
	RoleSource
	// RoleFont is a supplied font file.
	RoleFont
	// RoleOutput is the assembled PDF destination.
	RoleOutput
	// RoleReport is a saved report destination.
	RoleReport
)

// errEmptyPath is returned when a resource has no path.
var errEmptyPath = errors.New("path is empty")

// String names the role for messages.
func (r Role) String() string {
	switch r {
	case RolePlan:
		return "plan file"
	case RoleSource:
		return "source PDF"
	case RoleFont:
		return "font file"
	case RoleOutput:
		return "output PDF"
	case RoleReport:
		return "report file"
	default:
		return "file"
	}
}

func (r Role) isArtifact() bool { return r == RoleOutput || r == RoleReport }

// Error names both resources and the repair.
func (e *AliasError) Error() string {
	return fmt.Sprintf("%s %q is the same file as %s %q; choose a different path for one of them",
		e.Role, e.Path, e.OtherRole, e.OtherPath)
}

// Error names the path and the problem.
func (e *ArtifactTargetError) Error() string {
	return fmt.Sprintf("%s %q %s; choose a regular-file path", e.Role, e.Path, e.Problem)
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		byIdentity: make(map[Identity]claim),
		byName:     make(map[Identity]map[string]claim),
	}
}

// Add records path under role. Inputs (plan, source, font) must exist and are resolved through
// symbolic links. Output and report paths may be new, but an existing one must be a regular file,
// not a symbolic link. Adding a file already recorded under a different role returns an
// *AliasError; repeating a role for the same file is allowed (sources are repeated by design).
// It returns the file's Identity when it exists and the zero Identity for a new file.
func (r *Registry) Add(role Role, path string) (Identity, error) {
	if path == "" {
		return Identity{}, &SourceError{Path: path, Operation: "register " + role.String(), Err: errEmptyPath}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if !role.isArtifact() {
		return r.addInput(role, path)
	}

	return r.addArtifact(role, path)
}

func (r *Registry) addInput(role Role, path string) (Identity, error) {
	identity, err := IdentityOf(path)
	if err != nil {
		return Identity{}, &SourceError{Path: path, Operation: "inspect " + role.String(), Err: err}
	}

	return identity, r.claimIdentity(role, path, identity)
}

func (r *Registry) addArtifact(role Role, path string) (Identity, error) {
	info, err := os.Lstat(path)

	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Identity{}, r.addNewArtifact(role, path)
	case err != nil:
		return Identity{}, &SourceError{Path: path, Operation: "inspect " + role.String(), Err: err}
	}

	problem := artifactProblem(info.Mode())
	if problem != "" {
		return Identity{}, &ArtifactTargetError{Path: path, Role: role, Problem: problem}
	}

	return r.addInput(role, path)
}

func (r *Registry) addNewArtifact(role Role, path string) error {
	dir := filepath.Dir(path)

	identity, isDirectory, err := inspectIdentity(dir)
	if err != nil {
		return &SourceError{Path: dir, Operation: "identify directory of " + role.String(), Err: err}
	}

	if !isDirectory {
		return &ArtifactTargetError{Path: dir, Role: role, Problem: "is not a directory for the artifact"}
	}

	name := cases.Fold().String(norm.NFC.String(filepath.Base(path)))

	names := r.byName[identity]
	if names == nil {
		names = make(map[string]claim)
		r.byName[identity] = names
	}

	other, found := names[name]
	if found && other.role != role {
		return &AliasError{Path: path, Role: role, OtherPath: other.path, OtherRole: other.role}
	}

	if !found {
		names[name] = claim{role: role, path: path}
	}

	return nil
}

func (r *Registry) claimIdentity(role Role, path string, identity Identity) error {
	other, found := r.byIdentity[identity]
	if !found {
		r.byIdentity[identity] = claim{role: role, path: path}

		return nil
	}

	if other.role != role {
		return &AliasError{Path: path, Role: role, OtherPath: other.path, OtherRole: other.role}
	}

	return nil
}

func artifactProblem(mode fs.FileMode) string {
	switch {
	case mode&fs.ModeSymlink != 0:
		return "is a symbolic link"
	case mode.IsDir():
		return "is a directory"
	case !mode.IsRegular():
		return "is not a regular file"
	default:
		return ""
	}
}
