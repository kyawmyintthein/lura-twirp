package luratwirp

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/luraproject/lura/config"
	"github.com/luraproject/lura/logging"
	"github.com/luraproject/lura/proxy"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
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

	registry struct {
		pools sync.Map
	}
)

const (
	TwirpServiceIdentifierConst = "twirp_service_identifier"
)

var (
	once                sync.Once
	_twirpStubRegistery registry
	initOnce            = func() {
		_twirpStubRegistery = registry{
			pools: sync.Map{},
		}
	}
)

func init() {
	once.Do(initOnce)
}

func RegisterTwirpStubs(stubs ...LuraTwirpStub) {
	for _, stub := range stubs {
		_twirpStubRegistery.pools.Store(stub.Identifier(), stub)
	}
}

func NewTwirpProxy(l logging.Logger, f proxy.BackendFactory) proxy.BackendFactory {
	return func(remote *config.Backend) proxy.Proxy {
		bo := getOptions(remote)
		if bo == nil {
			log.Println("twirp: client factory is not used for", remote)
			return f(remote)
		}

		return func(ctx context.Context, request *proxy.Request) (*proxy.Response, error) {
			resp, err := callService(ctx, request, bo)
			request.Body.Close()
			if err != nil {
				l.Warning("gRPC calling the next mw:", err.Error())
				return nil, err
			}
			return resp, err
		}
	}
}

func getOptions(remote *config.Backend) *twirpBackendOptions {
	identifier, _ := remote.ExtraConfig[TwirpServiceIdentifierConst].(string)
	return &twirpBackendOptions{
		method:            remote.Method,
		serviceName:       strings.TrimPrefix(remote.URLPattern, "/"),
		serviceIdentifier: identifier,
	}
}

func callService(ctx context.Context, request *proxy.Request, opts *twirpBackendOptions) (*proxy.Response, error) {
	caller := func(ctx context.Context, req *proxy.Request) (*proxy.Response, error) {
		registredItem, ok := _twirpStubRegistery.pools.Load(opts.serviceIdentifier)
		if !ok {
			return nil, _errInvalidTwirpClientIdentifier
		}

		stub, ok := registredItem.(LuraTwirpStub)
		if !ok {
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
			return nil, err
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
