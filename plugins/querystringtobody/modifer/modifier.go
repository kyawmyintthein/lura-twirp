// Package modifier exposes a request modifier for generating bodies
// from the querystring params
package modifier

import (
	"bytes"
	b64 "encoding/base64"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"

	kazaam "gopkg.in/qntfy/kazaam.v3"
)

type Config struct {
	URLPattern  string `json:"url_pattern"`
	Template    string `json:"template"`
	Method      string `json:"method"`
	ContentType string `json:"content_type"`
}

type Query2BodyModifier struct {
	template    string
	method      string
	contentType string
}

func (m *Query2BodyModifier) ModifyRequest(req *http.Request) error {
	if req.Body == nil {
		return nil
	}
	payloadBytes, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return err
	}
	req.Body.Close()
	log.Println("Payload", string(payloadBytes))
	log.Println("Template", string(m.template))
	k, err := kazaam.NewKazaam(m.template)
	if err != nil {
		return err
	}

	tranformedDataBytes, err := k.TransformJSONString(string(payloadBytes))
	if err != nil {
		return err
	}

	log.Println("Transform Data Bytes", string(tranformedDataBytes))

	buf := new(bytes.Buffer)
	buf.Read(tranformedDataBytes)
	if m.method != "" {
		req.Method = m.method
	}
	if m.contentType != "" {
		req.Header.Set("Content-Type", m.contentType)
	} else {
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	}
	req.ContentLength = int64(len(tranformedDataBytes))
	req.Body = ioutil.NopCloser(bytes.NewReader(tranformedDataBytes))

	return nil
}

func FromJSON(b []byte) (*Query2BodyModifier, error) {
	cfg := &Config{}
	if err := json.Unmarshal(b, cfg); err != nil {
		return nil, err
	}

	bytes, err := b64.StdEncoding.DecodeString(cfg.Template) // Converting data
	if err != nil {
		return nil, err
	}

	return &Query2BodyModifier{
		template: string(bytes),
		method:   cfg.Method,
	}, nil
}

func FromResponseJSON(b []byte) (*Query2BodyModifier, error) {
	cfg := &Config{}
	if err := json.Unmarshal(b, cfg); err != nil {
		return nil, err
	}

	bytes, err := b64.StdEncoding.DecodeString(cfg.Template) // Converting data
	if err != nil {
		return nil, err
	}

	return &Query2BodyModifier{
		template: string(bytes),
		method:   cfg.Method,
	}, nil
}

// ModifyResponse sets the Content-Type header and overrides the response body.
func (m *Query2BodyModifier) ModifyResponse(res *http.Response) error {
	log.Println("body.ModifyResponse: request: %s", res.Request.URL)

	if res.Body == nil {
		return nil
	}
	responseBodyBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return err
	}
	// Replace the existing body, close it first.
	res.Body.Close()

	res.Header.Set("Content-Type", m.contentType)

	// Reset the Content-Encoding since we know that the new body isn't encoded.
	res.Header.Del("Content-Encoding")

	log.Println("Response", string(responseBodyBytes))
	log.Println("Template", string(m.template))
	k, err := kazaam.NewKazaam(m.template)
	if err != nil {
		return err
	}

	tranformedDataBytes, err := k.TransformJSONString(string(responseBodyBytes))
	if err != nil {
		return err
	}

	log.Println("Transform Data Bytes", string(tranformedDataBytes))

	buf := new(bytes.Buffer)
	buf.Read(tranformedDataBytes)
	res.ContentLength = int64(len(tranformedDataBytes))
	res.Body = ioutil.NopCloser(bytes.NewReader(tranformedDataBytes))

	return nil
}
