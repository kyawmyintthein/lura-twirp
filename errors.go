package luratwirp

import "errors"

var (
	_errInvalidTwirpClientIdentifier = errors.New("[LuraTwirp]:invalid twirp stub")

	// ErrEmptyValue is the error returned when there is no config under the namespace
	ErrEmptyValue = errors.New("getting the extra config for the martian module")
	// ErrBadValue is the error returned when the config is not a map
	ErrBadValue = errors.New("casting the extra config for the martian module")
	// ErrMarshallingValue is the error returned when the config map can not be marshalled again
	ErrMarshallingValue = errors.New("marshalling the extra config for the martian module")
	// ErrEmptyResponse is the error returned when the modifier receives a nil response
	ErrEmptyResponse = errors.New("getting the http response from the request executor")
)
