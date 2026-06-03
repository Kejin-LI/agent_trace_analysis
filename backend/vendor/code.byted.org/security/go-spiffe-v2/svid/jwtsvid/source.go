package jwtsvid

import (
	"context"

	"code.byted.org/security/go-spiffe-v2/spiffeid"
)

// Params are JWT-SVID parameters used when fetching a new JWT-SVID.
type Params struct {
	Audience       string
	ExtraAudiences []string
	Subject        spiffeid.ID
}

// Source represents a source of JWT-SVIDs.
type Source interface {
	// FetchJWTSVID fetches a JWT-SVID from the source with the given
	// parameters.
	FetchJWTSVID(ctx context.Context, params Params) (*SVID, error)
}
