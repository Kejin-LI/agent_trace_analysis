package jwtbundle

import (
	"code.byted.org/security/go-spiffe-v2/spiffeid"
)

// Source represents a source of JWT bundles keyed by trust domain.
type Source interface {
	// GetJWTBundleForTrustDomain returns the JWT bundle for the given trust
	// domain.
	GetJWTBundleForTrustDomain(trustDomain spiffeid.TrustDomain) (*Bundle, error)
}
