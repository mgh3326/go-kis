package ws

import (
	"context"
	"errors"
	"sync"

	"github.com/mgh3326/go-kis/kis"
)

// ApprovalKeyProvider supplies the approval key used in every subscribe and
// unsubscribe header.
//
// Injecting a provider is how a process hands an existing approval key to a
// successor without a REST round trip: return the cached key from ApprovalKey
// and the connection issues no approval request at all.
type ApprovalKeyProvider interface {
	// ApprovalKey returns the key to use. It is called on connect and on each
	// reconnect, so a caching implementation controls the request rate.
	ApprovalKey(ctx context.Context) (string, error)

	// Reissue discards the current key and obtains a new one. It is called
	// only after a rejection whose msg_cd is in the reissuable set, and never
	// for ErrSessionOccupied, where a new key cannot help.
	Reissue(ctx context.Context) (string, error)
}

// ErrApprovalUnavailable reports that no approval key could be obtained. It is
// an operational failure of the provider, not a KIS protocol verdict, and so
// is distinct from the protocol vocabulary in errors.go.
var errApprovalUnavailable = errors.New("ws: approval key unavailable")

// NewClientApprovalProvider returns the default provider, which obtains keys
// through the REST approval endpoint of the given client and caches the result
// until Reissue is called.
func NewClientApprovalProvider(client *kis.Client) ApprovalKeyProvider {
	return &clientProvider{client: client}
}

type clientProvider struct {
	client *kis.Client
	mu     sync.Mutex
	cached string
}

func (p *clientProvider) ApprovalKey(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cached != "" {
		return p.cached, nil
	}
	return p.issue(ctx)
}

func (p *clientProvider) Reissue(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cached = ""
	return p.issue(ctx)
}

// issue runs under p.mu.
func (p *clientProvider) issue(ctx context.Context) (string, error) {
	if p.client == nil {
		return "", errApprovalUnavailable
	}
	issued, err := p.client.IssueApprovalKey(ctx)
	if err != nil || issued.ApprovalKey == "" {
		// The upstream error can quote the request; do not propagate it.
		return "", errApprovalUnavailable
	}
	p.cached = issued.ApprovalKey
	return p.cached, nil
}
