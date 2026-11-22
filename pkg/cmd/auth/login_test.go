package auth

import (
	"errors"
	"testing"

	"github.com/mwcli/mwcli/api"
	"github.com/stretchr/testify/assert"
)

func TestOAuth1Hint(t *testing.T) {
	reject := func(reason string) error {
		return &api.APIError{Code: "mwoauth-invalid-authorization", Info: "The authorization headers in your request are not valid: " + reason}
	}
	assert.Contains(t, oauth1Hint(reject("Invalid consumer")), "consumer token")
	assert.Contains(t, oauth1Hint(reject("Invalid consumer key")), "consumer token")
	assert.Contains(t, oauth1Hint(reject("Invalid signature")), "The wiki could not verify the signature. Check the consumer secret and the access secret.")
	assert.Contains(t, oauth1Hint(reject("No approved grant was found for that authorization token.")), "access token")
	assert.Empty(t, oauth1Hint(reject("Nonce already used: x")))
	assert.Empty(t, oauth1Hint(reject("Expired timestamp, yours 1, ours 2")))
	assert.Empty(t, oauth1Hint(&api.APIError{Code: "mwoauth-invalid-authorization-blocked-user"}))
	assert.Empty(t, oauth1Hint(errors.New("network down")))
}
