package port

import (
	"context"
	"io"
)

// Request representa uma chamada HTTP ao upstream.
type Request struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    []byte
}

// Response é sempre um io.ReadCloser — streaming e não-streaming tratados
// de forma uniforme, eliminando o "if (isStream)" espalhado pelo código.
type Response struct {
	Status int
	Header map[string][]string
	Body   io.ReadCloser
}

// HTTPClient abstrai a chamada HTTP ao upstream (Adapter sobre net/http).
type HTTPClient interface {
	Do(ctx context.Context, req Request) (*Response, error)
}
