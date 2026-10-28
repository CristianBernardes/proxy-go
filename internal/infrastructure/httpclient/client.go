package httpclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"net/http"
	"time"

	"edenred-ai-proxy/internal/domain/port"
)

// Client adapta net/http para o port.HTTPClient do domínio.
type Client struct {
	http *http.Client
}

// New cria um cliente HTTP com Transport tunado para streaming, reuso de conexões
// e, opcionalmente, InsecureSkipVerify (upstream https com CA interno).
func New(insecureTLS bool) *Client {
	transport := &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: insecureTLS},
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}
	return &Client{http: &http.Client{Transport: transport}}
}

// Do executa a requisição HTTP propagando ctx — cancelamento do cliente encerra
// imediatamente a conexão com o upstream.
func (c *Client) Do(ctx context.Context, req port.Request) (*port.Response, error) {
	var body *bytes.Reader
	if req.Body != nil {
		body = bytes.NewReader(req.Body)
	} else {
		body = bytes.NewReader(nil)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, body)
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}

	return &port.Response{
		Status: resp.StatusCode,
		Header: resp.Header,
		Body:   resp.Body, // o chamador sempre deve fechar
	}, nil
}
