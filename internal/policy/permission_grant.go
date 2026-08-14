package policy

import (
	"path/filepath"
	"strings"
)

// PermissionGrant is the in-memory representation of an "allow for this
// session" decision. The matcher is deliberately explicit: file grants are
// directory-scoped, command grants are exact, and external grants use a key.
type PermissionGrantKind string

const (
	GrantReadDirectory PermissionGrantKind = "read_directory"
	GrantEditDirectory PermissionGrantKind = "edit_directory"
	GrantCommandExact  PermissionGrantKind = "command_exact"
	GrantExternalKey   PermissionGrantKind = "external_key"
)

type PermissionGrant struct {
	Kind      PermissionGrantKind
	Directory string
	Command   *CommandApprovalKey
	Key       string
}

func ReadDirectoryGrant(path string) PermissionGrant {
	return PermissionGrant{Kind: GrantReadDirectory, Directory: filepath.Clean(strings.TrimSpace(path))}
}

func EditDirectoryGrant(path string) PermissionGrant {
	return PermissionGrant{Kind: GrantEditDirectory, Directory: filepath.Clean(strings.TrimSpace(path))}
}

func CommandGrant(key CommandApprovalKey) PermissionGrant {
	return PermissionGrant{Kind: GrantCommandExact, Command: &key}
}

func ExternalGrant(key string) PermissionGrant {
	return PermissionGrant{Kind: GrantExternalKey, Key: strings.TrimSpace(key)}
}

func (grant PermissionGrant) Valid() bool {
	switch grant.Kind {
	case GrantReadDirectory, GrantEditDirectory:
		return grant.Directory != "" && grant.Directory != "."
	case GrantCommandExact:
		return grant.Command != nil && grant.Command.CWD != "" && grant.Command.Command != ""
	case GrantExternalKey:
		return strings.TrimSpace(grant.Key) != ""
	default:
		return false
	}
}
