package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/repoinfo"
)

type HostedRole string

const (
	HostedRoleViewer HostedRole = "viewer"
	HostedRoleEditor HostedRole = "editor"
	HostedRoleOwner  HostedRole = "owner"
)

var (
	ErrHostedSessionExists       = errors.New("hosted session already exists")
	ErrHostedSessionMissing      = errors.New("hosted session does not exist")
	ErrHostedSessionNotCommunity = errors.New("hosted session does not admit new community members")
	ErrHostedMemberExists        = errors.New("hosted session member already exists")
	ErrHostedMemberDisabled      = errors.New("hosted session member is disabled")
	ErrHostedIdentityMismatch    = errors.New("hosted identity does not match the existing member")
	ErrHostedInvitationInvalid   = errors.New("hosted invitation is invalid, expired, or redeemed")
)

const (
	HostedMetadataTextMaxBytes = 128
	HostedPageDefaultLimit     = 50
	HostedPageMaxLimit         = 100
	HostedActiveIDMaxCount     = 1000
)

type HostedVisibility string

const (
	HostedVisibilityPrivate   HostedVisibility = "private"
	HostedVisibilityCommunity HostedVisibility = "community"
)

type HostedSessionMetadata struct {
	Visibility       HostedVisibility `json:"visibility"`
	Title            string           `json:"title"`
	MapLabel         string           `json:"map_label"`
	EnvironmentLabel string           `json:"environment_label"`
}

type HostedSessionPageRequest struct {
	Cursor string
	Limit  int
}

type HostedSessionPage struct {
	Sessions   []HostedSession
	NextCursor string
}

type HostedSession struct {
	SessionID        string
	DocumentID       model.DocumentID
	CreatedAt        time.Time
	Visibility       HostedVisibility
	Title            string
	MapLabel         string
	EnvironmentLabel string
	OwnerDisplayName string
	// Repository is the optional host-published repository descriptor. Nil means
	// unknown. Stores validate it on write and treat an invalid stored value as
	// unknown on read.
	Repository *repoinfo.Descriptor
}

// Notification summaries expose bounded Community metadata and Private totals.
type HostedNotificationSummary struct {
	Community          []HostedSession
	CommunityCount     int
	PrivateCount       int
	ActivePrivateCount int
}

type HostedNotificationStore interface {
	HostedNotificationSummary(context.Context, []string) (HostedNotificationSummary, error)
}

type HostedIdentity struct {
	Issuer      string
	Subject     string
	ActorID     model.ActorID
	DisplayName string
}

type HostedMember struct {
	SessionID   string
	Issuer      string
	Subject     string
	ActorID     model.ActorID
	DisplayName string
	Role        HostedRole
	Disabled    bool
}

type HostedInvitation struct {
	TokenHash        [sha256.Size]byte
	SessionID        string
	Role             HostedRole
	CreatedByActorID model.ActorID
	ExpiresAt        time.Time
}

type HostedRegistry interface {
	CreateHostedSession(context.Context, HostedSession, HostedMember) error
	ListHostedSessions(context.Context) ([]HostedSession, error)
	ListHostedMembers(context.Context, string) ([]HostedMember, error)
	ResolveHostedMember(context.Context, string, string, string) (HostedMember, bool, error)
	UpdateHostedMemberDisplayName(context.Context, string, model.ActorID, string) error
	CreateHostedInvitation(context.Context, HostedInvitation) error
	RedeemHostedInvitation(context.Context, string, [sha256.Size]byte, HostedIdentity, time.Time) (HostedMember, error)
}

// HostedSessionLifecycleStore ends discovery and admission while retaining the
// document and its acknowledged operation history for recovery.
type HostedSessionLifecycleStore interface {
	EndHostedSession(context.Context, string) error
}

// HostedSessionBrowserStore contains the bounded, hosted-only operations used by
// session browsing and Community admission. Keeping it separate preserves the
// smaller HostedRegistry contract used by existing server fakes.
type HostedSessionBrowserStore interface {
	ListCommunityHostedSessions(context.Context, model.ActorID, []string, HostedSessionPageRequest) (HostedSessionPage, error)
	ListMyHostedSessions(context.Context, model.ActorID, HostedSessionPageRequest) (HostedSessionPage, error)
	UpdateHostedSessionMetadata(context.Context, string, model.ActorID, HostedSessionMetadata) (HostedSession, error)
	JoinHostedSession(context.Context, string, HostedIdentity) (HostedMember, bool, error)
}

// NormalizeHostedSessionMetadata applies the same bounded text rules at HTTP
// request and storage boundaries. Empty visibility preserves old-client behavior
// by defaulting to Private; filesystem paths and URLs are rejected in the labels.
func NormalizeHostedSessionMetadata(metadata HostedSessionMetadata) (HostedSessionMetadata, error) {
	if metadata.Visibility == "" {
		metadata.Visibility = HostedVisibilityPrivate
	}
	if metadata.Visibility != HostedVisibilityPrivate && metadata.Visibility != HostedVisibilityCommunity {
		return HostedSessionMetadata{}, fmt.Errorf("hosted session visibility is invalid")
	}
	var err error
	if metadata.Title, err = normalizeHostedMetadataText(metadata.Title, false); err != nil {
		return HostedSessionMetadata{}, fmt.Errorf("hosted session title: %w", err)
	}
	if metadata.MapLabel, err = normalizeHostedMetadataText(metadata.MapLabel, true); err != nil {
		return HostedSessionMetadata{}, fmt.Errorf("hosted map label: %w", err)
	}
	if metadata.EnvironmentLabel, err = normalizeHostedMetadataText(metadata.EnvironmentLabel, true); err != nil {
		return HostedSessionMetadata{}, fmt.Errorf("hosted environment label: %w", err)
	}
	return metadata, nil
}

func normalizeHostedMetadataText(value string, basenameOnly bool) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("text is not valid UTF-8")
	}
	value = strings.TrimSpace(value)
	if len(value) > HostedMetadataTextMaxBytes {
		return "", fmt.Errorf("text exceeds %d UTF-8 bytes", HostedMetadataTextMaxBytes)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", fmt.Errorf("text contains a control character")
		}
	}
	if strings.Contains(value, "://") || isAbsoluteHostedMetadataPath(value) || (basenameOnly && strings.ContainsAny(value, `/\\`)) {
		return "", fmt.Errorf("text must be a label, not a path or URL")
	}
	return value, nil
}

func isAbsoluteHostedMetadataPath(value string) bool {
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, `\\`) {
		return true
	}
	return len(value) >= 3 && ((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')) && value[1] == ':' && (value[2] == '/' || value[2] == '\\')
}

func ValidHostedRole(role HostedRole) bool {
	return role == HostedRoleViewer || role == HostedRoleEditor || role == HostedRoleOwner
}
