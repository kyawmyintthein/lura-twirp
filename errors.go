package luratwirp

import "errors"

var (
	_errInvalidTwirpClientIdentifier = errors.New("[LuraTwirp]:invalid twirp stub")

	// ErrEmptyValue is the error returned when there is no config under the namespace
	_errEmptyValue = errors.New("getting the extra config for the martian module")
	// ErrBadValue is the error returned when the config is not a map
	_errBadValue = errors.New("casting the extra config for the martian module")
	// ErrMarshallingValue is the error returned when the config map can not be marshalled again
	_errMarshallingValue = errors.New("marshalling the extra config for the martian module")
	// ErrEmptyResponse is the error returned when the modifier receives a nil response
	_errEmptyResponse = errors.New("getting the http response from the request executor")
)
