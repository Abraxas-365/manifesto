package kernel

import (
	"encoding/json"
	"errors"
)

// Actor identifies exactly one request principal: a user or an API key.
// Its zero value is invalid and must be rejected at authentication boundaries.
type Actor struct {
	userID   UserID
	apiKeyID APIKeyID
}

func NewUserActor(id UserID) Actor {
	return Actor{userID: id}
}

func NewAPIKeyActor(id APIKeyID) Actor {
	return Actor{apiKeyID: id}
}

func (a Actor) UserID() (UserID, bool) {
	return a.userID, !a.userID.IsEmpty() && a.apiKeyID.IsEmpty()
}

func (a Actor) APIKeyID() (APIKeyID, bool) {
	return a.apiKeyID, !a.apiKeyID.IsEmpty() && a.userID.IsEmpty()
}

func (a Actor) IsUser() bool {
	_, ok := a.UserID()
	return ok
}

func (a Actor) IsAPIKey() bool {
	_, ok := a.APIKeyID()
	return ok
}

func (a Actor) IsValid() bool {
	return a.IsUser() || a.IsAPIKey()
}

// MarshalJSON provides audit attribution without exposing the internal fields.
// Invalid actors fail serialization rather than producing misleading identity.
func (a Actor) MarshalJSON() ([]byte, error) {
	var kind, id string
	if userID, ok := a.UserID(); ok {
		kind, id = "user", userID.String()
	} else if keyID, ok := a.APIKeyID(); ok {
		kind, id = "api_key", keyID.String()
	} else {
		return nil, errors.New("cannot marshal invalid actor")
	}
	return json.Marshal(struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}{Type: kind, ID: id})
}
