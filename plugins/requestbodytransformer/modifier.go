package requestbodytransformer

import (
	"github.com/google/martian/parse"
	"github.com/kyawmyintthein/lura-twirp/plugins/requestbodytransformer/modifier"
)

func init() {
	parse.Register("body.FromRequestBody", FromJSON)
	parse.Register("body.ToResponseBody", FromRespJSON)
}

func FromJSON(b []byte) (*parse.Result, error) {
	msg, err := modifier.FromJSON(b)
	if err != nil {
		return nil, err
	}

	return parse.NewResult(msg, []parse.ModifierType{parse.Request})
}

func FromRespJSON(b []byte) (*parse.Result, error) {
	msg, err := modifier.FromResponseJSON(b)
	if err != nil {
		return nil, err
	}

	return parse.NewResult(msg, []parse.ModifierType{parse.Request})
}
