package luratwirp

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/clbanning/mxj"
	"github.com/google/martian"
	"github.com/google/martian/parse"
	"github.com/luraproject/lura/config"
	"github.com/luraproject/lura/logging"
	"github.com/luraproject/lura/proxy"
	"github.com/luraproject/lura/transport/http/client"
	"github.com/twitchtv/twirp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	_ "github.com/google/martian/body"
	_ "github.com/google/martian/cookie"
	_ "github.com/google/martian/fifo"
	_ "github.com/google/martian/header"
	_ "github.com/google/martian/martianurl"
	_ "github.com/google/martian/port"
	_ "github.com/google/martian/priority"
	_ "github.com/google/martian/stash"
	_ "github.com/google/martian/status"
)

type (
	Service = string
	Method  = string

	HTTPClient interface {
		Do(req *http.Request) (*http.Response, error)
	}

	LuraTwirpStub interface {
		Identifier() string
		Invoke(context.Context, Service, Method, proto.Message) (proto.Message, error)
		Encode(context.Context, Method, []byte) (proto.Message, error)
		Decode(context.Context, Method, proto.Message) ([]byte, error)
	}

	twirpBackendOptions struct {
		serviceName       string
		method            string
		serviceIdentifier string
	}

	result struct {
		Result *parse.Result
		Err    error
	}

	registry struct {
		pools sync.Map
	}
)

const (
	TwirpServiceIdentifierConst = "twirp_service_identifier"

	_contentTypeApplicationXML = "application/xml"
)

var (
	_twirpStubRegistery = registry{
		pools: sync.Map{},
	}
)

func RegisterTwirpStubs(l logging.Logger, stubs ...LuraTwirpStub) {
	for _, stub := range stubs {
		_twirpStubRegistery.pools.Store(stub.Identifier(), stub)
		l.Info("twirp: register new stub", stub.Identifier())
	}
}

func NewTwirpProxy(logger logging.Logger, re client.HTTPRequestExecutor) proxy.BackendFactory {
	return NewConfiguredBackendFactory(logger, func(_ *config.Backend) client.HTTPRequestExecutor { return re })
}

func NewConfiguredBackendFactory(l logging.Logger, ref func(*config.Backend) client.HTTPRequestExecutor) proxy.BackendFactory {
	return func(remote *config.Backend) proxy.Proxy {
		re := ref(remote)
		_, isTwirpCall := remote.ExtraConfig[TwirpServiceIdentifierConst]
		if isTwirpCall {
			start := time.Now()
			defer func(logger logging.Logger) {
				elapsed := time.Since(start)
				logger.Info("NewConfiguredBackendFactory time : %s", elapsed)
			}(l)
			twirpOpt := getTwirpOptions(remote)
			if twirpOpt == nil {
				log.Println("twirp: client factory is not used for", remote)
				return proxy.NewHTTPProxyWithHTTPExecutor(remote, re, remote.Decoder)
			}

			result, ok := getConfig(remote.ExtraConfig).(result)
			if !ok {
				return func(ctx context.Context, request *proxy.Request) (*proxy.Response, error) {
					req, err := convertProxyRequest2HttpRequest(request)
					if err != nil {
						return nil, err
					}
					request.Body.Close()
					resp, err := callService(ctx, req, twirpOpt, l)
					req.Body.Close()
					if err != nil {
						l.Warning("gRPC calling the next mw:", err.Error())
						return nil, err
					}
					return resp, err
				}
			}

			return func(ctx context.Context, request *proxy.Request) (*proxy.Response, error) {
				start := time.Now()
				defer func(logger logging.Logger) {
					elapsed := time.Since(start)
					logger.Info("ProxyFunc time : %s", elapsed)
				}(l)
				req, err := convertProxyRequest2HttpRequest(request)
				if err != nil {
					return nil, err
				}

				var (
					reqMod  martian.RequestModifier
					respMod martian.ResponseModifier
				)

				switch result.Err {
				case nil:
					reqMod = result.Result.RequestModifier()
					respMod = result.Result.ResponseModifier()
				default:
					l.Error(result.Err, "parser.ResultError", result, remote.ExtraConfig)
				}

				if reqMod != nil {
					err = reqMod.ModifyRequest(req)
					if err != nil {
						l.Error(err, "failed to modify request")
						return nil, err
					}
				}

				resp, err := callService(ctx, req, twirpOpt, l)
				request.Body.Close()
				req.Body.Close()
				if err != nil {
					l.Warning("RPC calling the next mw:", err.Error())
					return nil, err
				}

				err = modifyProxyResponse(respMod, req, resp)
				if err != nil {
					l.Error(err, "failed to modify response")
					return resp, err
				}
				return resp, err
			}
		}

		// HTTP call
		result, ok := getConfig(remote.ExtraConfig).(result)
		if !ok {
			return proxy.NewHTTPProxyWithHTTPExecutor(remote, re, remote.Decoder)
		}
		switch result.Err {
		case nil:
			return proxy.NewHTTPProxyWithHTTPExecutor(remote, HTTPRequestExecutor(result.Result, re), remote.Decoder)
		case _errEmptyValue:
			return proxy.NewHTTPProxyWithHTTPExecutor(remote, re, remote.Decoder)
		default:
			l.Error(result, remote.ExtraConfig)
			return proxy.NewHTTPProxyWithHTTPExecutor(remote, re, remote.Decoder)
		}
	}
}

func getConfig(e config.ExtraConfig) interface{} {
	cfg, ok := e[Namespace]
	if !ok {
		return result{nil, _errEmptyValue}
	}

	data, ok := cfg.(map[string]interface{})
	if !ok {
		return result{nil, _errBadValue}
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return result{nil, _errMarshallingValue}
	}

	r, err := parse.FromJSON(raw)

	return result{r, err}
}

func getTwirpOptions(remote *config.Backend) *twirpBackendOptions {
	identifier, _ := remote.ExtraConfig[TwirpServiceIdentifierConst].(string)
	return &twirpBackendOptions{
		method:            remote.Method,
		serviceName:       strings.TrimPrefix(remote.URLPattern, "/"),
		serviceIdentifier: identifier,
	}
}

func callService(ctx context.Context, request *http.Request, opts *twirpBackendOptions, l logging.Logger) (*proxy.Response, error) {
	caller := func(ctx context.Context, req *http.Request) (*proxy.Response, error) {
		registredItem, ok := _twirpStubRegistery.pools.Load(opts.serviceIdentifier)
		if !ok {
			l.Warning("twirp: stub not found for service", opts.serviceIdentifier)
			return nil, _errInvalidTwirpClientIdentifier
		}

		stub, ok := registredItem.(LuraTwirpStub)
		if !ok {
			l.Warning("twirp: stub is not implemeted LuraTwirpStub interface", opts.serviceIdentifier)
			return nil, _errInvalidTwirpClientIdentifier
		}

		var in proto.Message
		if req.Body != nil {
			payload, err := ioutil.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}

			in, err = stub.Encode(ctx, opts.method, payload)
			if err != nil {
				return nil, err
			}
		}

		resp, err := stub.Invoke(ctx, opts.serviceName, opts.method, in)
		if err != nil {
			l.Error(err, "function invocation failed")
			twirpErrorCode := twirp.Internal
			header := http.Header{}
			twirpErr, ok := err.(twirp.Error)
			if ok {
				errData := make(map[string]interface{})
				errData["code"] = twirpErr.Code()
				errData["msg"] = twirpErr.Msg()
				errData["meta"] = twirpErr.MetaMap()
				twirpErrorCode = twirpErr.Code()

				statusCode := http.StatusInternalServerError
				statusCode = twirp.ServerHTTPStatusFromErrorCode(twirpErrorCode)

				return &proxy.Response{
					Data:       errData,
					IsComplete: true,
					Metadata: proxy.Metadata{
						Headers:    header,
						StatusCode: statusCode,
					},
				}, nil
			}

			errData := make(map[string]interface{})
			errData["code"] = twirpErrorCode
			errData["msg"] = err.Error()

			return &proxy.Response{
				Data:       errData,
				IsComplete: true,
				Metadata: proxy.Metadata{
					Headers:    header,
					StatusCode: http.StatusInternalServerError,
				},
			}, nil
		}

		str := protojson.Format(resp)
		var data map[string]interface{}
		err = json.Unmarshal([]byte(str), &data)
		if err != nil {
			return nil, err
		}

		return &proxy.Response{
			Data:       data,
			IsComplete: true,
			Metadata: proxy.Metadata{
				Headers: make(map[string][]string),
			},
		}, err
	}
	return caller(ctx, request)
}

func convertProxyRequest2HttpRequest(request *proxy.Request) (*http.Request, error) {

	requestToBakend, err := http.NewRequest(strings.ToTitle(request.Method), request.URL.String(), request.Body)
	if err != nil {
		return nil, err
	}

	requestToBakend.Header = make(map[string][]string, len(request.Headers))
	for k, vs := range request.Headers {
		tmp := make([]string, len(vs))
		copy(tmp, vs)
		requestToBakend.Header[k] = tmp
	}
	if request.Body != nil {
		if v, ok := request.Headers["Content-Length"]; ok && len(v) == 1 && v[0] != "chunked" {
			if size, err := strconv.Atoi(v[0]); err == nil {
				requestToBakend.ContentLength = int64(size)
			}
		}
	}

	return requestToBakend, nil
}

// HTTPRequestExecutor creates a wrapper over the received request executor, so the martian modifiers can be
// executed before and after the execution of the request
func HTTPRequestExecutor(result *parse.Result, re client.HTTPRequestExecutor) client.HTTPRequestExecutor {
	return func(ctx context.Context, req *http.Request) (resp *http.Response, err error) {
		if err = modifyRequest(result.RequestModifier(), req); err != nil {
			return
		}

		mctx, ok := req.Context().(*Context)
		if !ok || !mctx.SkippingRoundTrip() {
			resp, err = re(ctx, req)
			if err != nil {
				return
			}
			if resp == nil {
				err = _errEmptyResponse
				return
			}
		} else if resp == nil {
			resp = &http.Response{
				Request:    req,
				Header:     http.Header{},
				StatusCode: http.StatusOK,
				Body:       ioutil.NopCloser(bytes.NewBufferString("")),
			}
		}

		err = modifyResponse(result.ResponseModifier(), resp)
		return
	}
}

func modifyRequest(mod martian.RequestModifier, req *http.Request) error {
	if req.Body == nil {
		req.Body = ioutil.NopCloser(bytes.NewBufferString(""))
	}
	if req.Header == nil {
		req.Header = http.Header{}
	}

	if mod == nil {
		return nil
	}
	return mod.ModifyRequest(req)
}

func modifyResponse(mod martian.ResponseModifier, resp *http.Response) error {
	if resp.Body == nil {
		resp.Body = ioutil.NopCloser(bytes.NewBufferString(""))
	}

	if resp.Header == nil {
		resp.Header = http.Header{}
	}

	if resp.StatusCode == 0 {
		resp.StatusCode = http.StatusOK
	}

	if mod == nil {
		return nil
	}

	return mod.ModifyResponse(resp)
}

func modifyProxyResponse(mod martian.ResponseModifier, request *http.Request, resp *proxy.Response) error {
	bodyBytes, err := json.Marshal(resp.Data)
	if err != nil {
		return err
	}

	httpResponse := http.Response{
		Request:    request,
		Body:       ioutil.NopCloser(bytes.NewBuffer(bodyBytes)),
		StatusCode: resp.Metadata.StatusCode,
		Header:     resp.Metadata.Headers,
	}

	if mod == nil {
		return nil
	}

	err = mod.ModifyResponse(&httpResponse)
	if err != nil {
		return err
	}

	modifiedResponseBytes, err := ioutil.ReadAll(httpResponse.Body)
	if err != nil {
		return err
	}

	var data map[string]interface{}
	switch request.Header.Get("Content-Type") {
	case _contentTypeApplicationXML:
		mv, err := mxj.NewMapXml(modifiedResponseBytes)
		if err != nil {
			return err
		}
		data = mv
	default:
		if len(modifiedResponseBytes) <= 0 {
			modifiedResponseBytes = []byte(`{}`)
		}
		err = json.Unmarshal(modifiedResponseBytes, &data)
		if err != nil {
			return err
		}
	}

	resp.Data = data
	resp.Metadata.Headers = httpResponse.Header
	resp.Metadata.StatusCode = httpResponse.StatusCode
	resp.Io = proxy.NewReadCloserWrapper(request.Context(), ioutil.NopCloser(bytes.NewBuffer(modifiedResponseBytes)))
	resp.IsComplete = true
	return nil
}
