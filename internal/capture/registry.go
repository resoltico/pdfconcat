// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
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

	artifactBinding struct {
		path string
		role Role
	}

	claim struct {
		path string
		role Role
	}

	// Registry records every resource of a job and rejects a file used in two roles. Existing files
	// are matched by filesystem Identity, so hard links, symbolic links (resolved for every role but
	// output and report, which may not be symbolic links) and case aliases collide; files that do not
	// exist yet are conservatively matched by their directory identity and Unicode-normalized,
	// case-folded basename, including on case-sensitive filesystems. Input lookup uses one identity-map access.
	// Artifact registration refreshes the small artifact-path set to account for replacement, without scanning inputs.
	// A Registry is safe for concurrent use.
	Registry struct {
		byIdentity       map[Identity]claim
		artifactBindings map[artifactBinding]Identity
		byName           map[Identity]map[string]claim
		mu               sync.Mutex
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
		byIdentity:       make(map[Identity]claim),
		artifactBindings: make(map[artifactBinding]Identity),
		byName:           make(map[Identity]map[string]claim),
	}
}

// Add records path under role. Inputs (plan, source, font) must exist and are resolved through
// symbolic links. Output and report paths may be new, but an existing one must be a regular file,
// not a symbolic link. Adding a file already recorded under a different role returns an
// *AliasError; repeating a role for the same file is allowed (sources are repeated by design).
// Re-registering an artifact path updates its object identity after replacement; independently
// registered artifact aliases and input identities remain protected.
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

	absolute, err := filepath.Abs(path)
	if err != nil {
		return Identity{}, &SourceError{Path: path, Operation: "resolve " + role.String(), Err: err}
	}

	return r.registerArtifact(artifactBinding{role: role, path: absolute}, inspectArtifact)
}

// ProtectOutput reserves the intended PDF path and its current identity against other roles.
// It checks aliases independently of output usability: a failed PDF target must not prevent
// saving evidence to a distinct safe report. Publication still requires Add and destination checks.
func (r *Registry) ProtectOutput(path string) error {
	if path == "" {
		return &SourceError{Path: path, Operation: "protect output", Err: errEmptyPath}
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return &SourceError{Path: path, Operation: "resolve protected output", Err: err}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// An absent or non-directory parent proves there is no output object to alias yet.
	// Keep its planned name; a later verification repeats identity/name checks if the parent appears.
	if artifactParentUnavailable(absolute) {
		r.artifactBindings[artifactBinding{role: RoleOutput, path: absolute}] = Identity{}
		return nil
	}

	_, err = r.registerArtifact(artifactBinding{role: RoleOutput, path: absolute}, inspectArtifactIdentity)

	return err
}

func inspectArtifactIdentity(role Role, path string) (Identity, error) {
	identity, err := IdentityOf(path)
	if errors.Is(err, fs.ErrNotExist) || (err != nil && artifactParentUnavailable(path)) {
		return Identity{}, nil
	}

	if err != nil {
		return Identity{}, &SourceError{Path: path, Operation: "identify " + role.String(), Err: err}
	}

	return identity, nil
}

// artifactParentUnavailable requires observed absence or a regular-file ancestor, not an I/O guess.
func artifactParentUnavailable(path string) bool {
	for parent := filepath.Dir(path); filepath.Dir(parent) != parent; parent = filepath.Dir(parent) {
		_, directory, err := inspectIdentity(parent)
		if err == nil {
			return !directory
		}

		if errors.Is(err, fs.ErrNotExist) {
			return true
		}
	}

	return false
}

// RetireReport abandons one failed report destination before publishing a distinct recovery file.
// Inputs, other artifact paths and conservative name reservations remain protected.
func (r *Registry) RetireReport(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return &SourceError{Path: path, Operation: "resolve retired report", Err: err}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	binding := artifactBinding{role: RoleReport, path: absolute}
	next := make(map[artifactBinding]Identity, len(r.artifactBindings))

	claims := make(map[Identity]claim, len(r.artifactBindings))
	for current, identity := range r.artifactBindings {
		if current == binding {
			continue
		}

		next[current] = identity
		if identity != (Identity{}) {
			claims[identity] = claim(current)
		}
	}

	r.commitArtifactBindings(next, claims)

	return nil
}

func (r *Registry) addInput(role Role, path string) (Identity, error) {
	identity, err := IdentityOf(path)
	if err != nil {
		return Identity{}, &SourceError{Path: path, Operation: "inspect " + role.String(), Err: err}
	}

	return identity, r.claimIdentity(role, path, identity)
}

func inspectArtifact(role Role, path string) (Identity, error) {
	info, err := os.Lstat(path)

	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Identity{}, nil
	case err != nil:
		return Identity{}, &SourceError{Path: path, Operation: "inspect " + role.String(), Err: err}
	}

	problem := artifactProblem(info.Mode())
	if problem != "" {
		return Identity{}, &ArtifactTargetError{Path: path, Role: role, Problem: problem}
	}

	identity, err := IdentityOf(path)
	if err != nil {
		return Identity{}, &SourceError{Path: path, Operation: "identify " + role.String(), Err: err}
	}

	return identity, nil
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

// registerArtifact refreshes only the small artifact set, never the immutable input inventory.
// Inspection and conflict checks complete before any existing claim is changed.
func (r *Registry) registerArtifact(binding artifactBinding, inspect func(Role, string) (Identity, error)) (Identity, error) {
	next, err := r.refreshArtifactBindings(binding, inspect)
	if err != nil {
		return Identity{}, err
	}

	claims, err := r.validateArtifactBindings(next, binding)
	if err != nil {
		return Identity{}, err
	}

	identity := next[binding]
	if identity == (Identity{}) {
		if reservationErr := r.addNewArtifact(binding.role, binding.path); reservationErr != nil {
			return Identity{}, reservationErr
		}
	}

	r.commitArtifactBindings(next, claims)

	return identity, nil
}

func (r *Registry) refreshArtifactBindings(
	binding artifactBinding,
	inspect func(Role, string) (Identity, error),
) (map[artifactBinding]Identity, error) {
	next := make(map[artifactBinding]Identity, len(r.artifactBindings)+1)
	for current := range r.artifactBindings {
		if current == binding {
			continue
		}

		identity, err := inspectArtifactIdentity(current.role, current.path)
		if err != nil {
			return nil, err
		}

		next[current] = identity
	}

	identity, err := inspect(binding.role, binding.path)
	if err != nil {
		return nil, err
	}

	next[binding] = identity

	return next, nil
}

func (r *Registry) validateArtifactBindings(bindings map[artifactBinding]Identity, candidate artifactBinding) (map[Identity]claim, error) {
	claims := make(map[Identity]claim, len(bindings))
	for binding, identity := range bindings {
		if binding == candidate {
			continue
		}

		if err := r.checkArtifactIdentity(claims, binding, identity); err != nil {
			return nil, err
		}
	}

	if err := r.checkArtifactIdentity(claims, candidate, bindings[candidate]); err != nil {
		return nil, err
	}

	return claims, nil
}

func (r *Registry) checkArtifactIdentity(claims map[Identity]claim, binding artifactBinding, identity Identity) error {
	if identity == (Identity{}) {
		return nil
	}

	if other, found := r.byIdentity[identity]; found && !other.role.isArtifact() {
		return &AliasError{Path: binding.path, Role: binding.role, OtherPath: other.path, OtherRole: other.role}
	}

	if other, found := claims[identity]; found && other.role != binding.role {
		return &AliasError{Path: binding.path, Role: binding.role, OtherPath: other.path, OtherRole: other.role}
	}

	claims[identity] = claim(binding)

	return nil
}

func (r *Registry) commitArtifactBindings(bindings map[artifactBinding]Identity, claims map[Identity]claim) {
	for _, identity := range r.artifactBindings {
		if other, found := r.byIdentity[identity]; found && other.role.isArtifact() {
			delete(r.byIdentity, identity)
		}
	}

	maps.Copy(r.byIdentity, claims)

	r.artifactBindings = bindings
}
