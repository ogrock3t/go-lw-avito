package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
)

type Gateway struct {
	endpoint  string
	client    *http.Client
	transport *http.Transport
}

type positionRequest struct {
	RequestID   string            `json:"request_id"`
	RecipientID uuid.UUID         `json:"recipient_id"`
	Kind        string            `json:"kind"`
	Data        map[string]string `json:"data"`
}

type HTTPError struct {
	StatusCode int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("push service returned HTTP: %d", e.StatusCode)
}

func NewGateway(baseURL string, timeout time.Duration) (*Gateway, error) {
	if timeout <= 0 {
		return nil, fmt.Errorf("push timeout must be positive, got %s", timeout)
	}

	serviceURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse push service URL: %w", err)
	}

	if serviceURL.Host == "" || (serviceURL.Scheme != "http" && serviceURL.Scheme != "https") {
		return nil, fmt.Errorf("push service URL must be an absolute HTTP or HTTPS URL")
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
	}

	client := &http.Client{
		Transport: otelhttp.NewTransport(
			transport,
			otelhttp.WithMeterProvider(metricnoop.NewMeterProvider()),
		),
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &Gateway{
		endpoint:  serviceURL.JoinPath("api", "v1", "push").String(),
		client:    client,
		transport: transport,
	}, nil
}

func (g *Gateway) RequestPosition(ctx context.Context, driverID, tripID uuid.UUID) error {
	payload := positionRequest{
		RequestID:   uuid.NewString(),
		RecipientID: driverID,
		Kind:        "REQUEST_POSITION",
		Data: map[string]string{
			"trip_id": tripID.String(),
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode position push request: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		g.endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create position push request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")

	response, err := g.client.Do(request)
	if err != nil {
		return fmt.Errorf("send position push request: %w", err)
	}
	defer response.Body.Close()

	_, readErr := io.Copy(io.Discard, response.Body)

	if response.StatusCode != http.StatusAccepted {
		return &HTTPError{StatusCode: response.StatusCode}
	}

	if readErr != nil {
		return fmt.Errorf("read position push response: %w", readErr)
	}

	return nil
}

func (g *Gateway) CloseIdleConnections() {
	g.transport.CloseIdleConnections()
}
