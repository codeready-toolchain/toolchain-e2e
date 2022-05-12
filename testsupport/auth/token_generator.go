package auth

import (
	"time"

	commonauth "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
)

func NewToken(claims ...Claim) (*commonauth.Identity, string, error) {
	identity := commonauth.NewIdentity()
	claims = append(claims, commonauth.WithSubClaim(identity.ID.String()))
	token, err := commonauth.GenerateSignedE2ETestToken(*identity, claims...)
	return identity, token, err
}

func NewTokenFromIdentity(identity *commonauth.Identity, claims ...Claim) (string, error) {
	claims = append(claims, commonauth.WithSubClaim(identity.ID.String()))
	token, err := commonauth.GenerateSignedE2ETestToken(*identity, claims...)
	return token, err
}

type Claim = commonauth.ExtraClaim

func WithEmail(email string) Claim {
	return commonauth.WithEmailClaim(email)
}

func WithExp(exp time.Time) Claim {
	return commonauth.WithExpClaim(exp)
}

func WithIAT(iat time.Time) Claim {
	return commonauth.WithIATClaim(iat)
}

func WithPreferredUsername(username string) Claim {
	return commonauth.WithPreferredUsernameClaim(username)
}
