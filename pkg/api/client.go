package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Client talks to openvpnd over HTTP or a Unix socket.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// ClientOption configures the client.
type ClientOption func(*Client)

// WithToken sets the bearer token.
func WithToken(token string) ClientOption {
	return func(c *Client) { c.token = token }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) { c.httpClient = hc }
}

// WithUnixSocket dials a Unix domain socket.
func WithUnixSocket(socketPath string) ClientOption {
	return func(c *Client) {
		c.httpClient = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socketPath)
				},
			},
		}
		if c.baseURL == "" {
			c.baseURL = "http://localhost"
		}
	}
}

// NewClient creates an API client.
func NewClient(urlOrUnix string, opts ...ClientOption) (*Client, error) {
	c := &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	if strings.HasPrefix(urlOrUnix, "unix://") {
		path := strings.TrimPrefix(urlOrUnix, "unix://")
		c.baseURL = "http://localhost"
		WithUnixSocket(path)(c)
	} else {
		c.baseURL = strings.TrimRight(urlOrUnix, "/")
	}
	for _, o := range opts {
		o(c)
	}
	if c.baseURL == "" {
		return nil, fmt.Errorf("empty base URL")
	}
	return c, nil
}

func (c *Client) do(ctx context.Context, method, path string, in any, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var eb ErrorBody
		if json.Unmarshal(data, &eb) == nil && eb.Error.Message != "" {
			return &APIError{Status: resp.StatusCode, Code: eb.Error.Code, Message: eb.Error.Message}
		}
		return &APIError{Status: resp.StatusCode, Message: string(data)}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// Version returns daemon version.
func (c *Client) Version(ctx context.Context) (VersionInfo, error) {
	var v VersionInfo
	err := c.do(ctx, http.MethodGet, "/v1/version", nil, &v)
	return v, err
}

// ListBinaries lists registered binaries.
func (c *Client) ListBinaries(ctx context.Context) ([]Binary, error) {
	var out []Binary
	err := c.do(ctx, http.MethodGet, "/v1/binaries", nil, &out)
	return out, err
}

// CreateBinary registers a binary.
func (c *Client) CreateBinary(ctx context.Context, req BinaryCreateRequest) (Binary, error) {
	var out Binary
	err := c.do(ctx, http.MethodPost, "/v1/binaries", req, &out)
	return out, err
}

// DeleteBinary removes a binary.
func (c *Client) DeleteBinary(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/v1/binaries/"+name, nil, nil)
}

// ListInstances lists instances.
func (c *Client) ListInstances(ctx context.Context) ([]Instance, error) {
	var out []Instance
	err := c.do(ctx, http.MethodGet, "/v1/instances", nil, &out)
	return out, err
}

// CreateInstance creates an instance (response may include auto_filled).
func (c *Client) CreateInstance(ctx context.Context, req InstanceCreateRequest) (Instance, error) {
	var out InstanceCreateResponse
	err := c.do(ctx, http.MethodPost, "/v1/instances", req, &out)
	return out.Instance, err
}

// GetInstance returns one instance.
func (c *Client) GetInstance(ctx context.Context, name string) (Instance, error) {
	var out Instance
	err := c.do(ctx, http.MethodGet, "/v1/instances/"+name, nil, &out)
	return out, err
}

// DeleteInstance deletes an instance.
func (c *Client) DeleteInstance(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/v1/instances/"+name, nil, nil)
}

// UpdateInstance patches an instance.
func (c *Client) UpdateInstance(ctx context.Context, name string, req InstanceUpdateRequest) (Instance, error) {
	var out Instance
	err := c.do(ctx, http.MethodPatch, "/v1/instances/"+name, req, &out)
	return out, err
}

// InstanceUp enables and starts an instance.
func (c *Client) InstanceUp(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodPost, "/v1/instances/"+name+"/up", nil, nil)
}

// InstanceDown disables and stops an instance.
func (c *Client) InstanceDown(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodPost, "/v1/instances/"+name+"/down", nil, nil)
}

// InstanceRestart restarts an instance process.
func (c *Client) InstanceRestart(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodPost, "/v1/instances/"+name+"/restart", nil, nil)
}

// ExportInstance returns rendered conf text.
func (c *Client) ExportInstance(ctx context.Context, name string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/instances/"+name+"/export", nil)
	if err != nil {
		return "", err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		var eb ErrorBody
		if json.Unmarshal(data, &eb) == nil && eb.Error.Message != "" {
			return "", &APIError{Status: resp.StatusCode, Code: eb.Error.Code, Message: eb.Error.Message}
		}
		return "", &APIError{Status: resp.StatusCode, Message: string(data)}
	}
	return string(data), nil
}

// ListClients lists clients on a server instance.
func (c *Client) ListClients(ctx context.Context, instance string) ([]ServerClient, error) {
	var out []ServerClient
	err := c.do(ctx, http.MethodGet, "/v1/instances/"+instance+"/clients", nil, &out)
	return out, err
}

// CreateClient creates a client on a server instance.
func (c *Client) CreateClient(ctx context.Context, instance string, req ClientCreateRequest) (ServerClient, error) {
	var out ServerClient
	err := c.do(ctx, http.MethodPost, "/v1/instances/"+instance+"/clients", req, &out)
	return out, err
}

// DeleteClient deletes a client.
func (c *Client) DeleteClient(ctx context.Context, instance, cn string) error {
	return c.do(ctx, http.MethodDelete, "/v1/instances/"+instance+"/clients/"+cn, nil, nil)
}

// UpdateClient patches a client.
func (c *Client) UpdateClient(ctx context.Context, instance, cn string, req ClientUpdateRequest) (ServerClient, error) {
	var out ServerClient
	err := c.do(ctx, http.MethodPatch, "/v1/instances/"+instance+"/clients/"+cn, req, &out)
	return out, err
}

// SuspendClient suspends a server client.
func (c *Client) SuspendClient(ctx context.Context, instance, cn string) error {
	return c.do(ctx, http.MethodPost, "/v1/instances/"+instance+"/clients/"+cn+"/suspend", nil, nil)
}

// ResumeClient resumes a server client.
func (c *Client) ResumeClient(ctx context.Context, instance, cn string) error {
	return c.do(ctx, http.MethodPost, "/v1/instances/"+instance+"/clients/"+cn+"/resume", nil, nil)
}

// ResetClientTraffic soft-resets traffic counters.
func (c *Client) ResetClientTraffic(ctx context.Context, instance, cn string) error {
	return c.do(ctx, http.MethodPost, "/v1/instances/"+instance+"/clients/"+cn+"/reset-traffic", nil, nil)
}

// ListAllClients loads clients for every instance (servers only; client roles return empty).
func (c *Client) ListAllClients(ctx context.Context) ([]ServerClient, error) {
	insts, err := c.ListInstances(ctx)
	if err != nil {
		return nil, err
	}
	var out []ServerClient
	for _, inst := range insts {
		if inst.Role != "server" {
			continue
		}
		list, err := c.ListClients(ctx, inst.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, list...)
	}
	return out, nil
}

// ListEvents returns recent audit events.
func (c *Client) ListEvents(ctx context.Context) ([]Event, error) {
	var out []Event
	err := c.do(ctx, http.MethodGet, "/v1/events", nil, &out)
	return out, err
}

// CreateProfileLink mints a presigned .ovpn download / OpenVPN Connect import URL.
func (c *Client) CreateProfileLink(ctx context.Context, instance, cn string, req ProfileLinkRequest) (ProfileLink, error) {
	var out ProfileLink
	err := c.do(ctx, http.MethodPost, "/v1/instances/"+instance+"/clients/"+cn+"/profile-link", req, &out)
	return out, err
}

// ListProfileLinks lists share tokens for a client.
func (c *Client) ListProfileLinks(ctx context.Context, instance, cn string) ([]ProfileLink, error) {
	var out []ProfileLink
	err := c.do(ctx, http.MethodGet, "/v1/instances/"+instance+"/clients/"+cn+"/profile-links", nil, &out)
	return out, err
}

// RevokeProfileLink revokes a token.
func (c *Client) RevokeProfileLink(ctx context.Context, token string) error {
	return c.do(ctx, http.MethodDelete, "/v1/profile-tokens/"+token, nil, nil)
}

// ClientConfig downloads the .ovpn text (authenticated).
func (c *Client) ClientConfig(ctx context.Context, instance, cn string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/instances/"+instance+"/clients/"+cn+"/client-config", nil)
	if err != nil {
		return "", err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		var eb ErrorBody
		if json.Unmarshal(data, &eb) == nil && eb.Error.Message != "" {
			return "", &APIError{Status: resp.StatusCode, Code: eb.Error.Code, Message: eb.Error.Message}
		}
		return "", &APIError{Status: resp.StatusCode, Message: string(data)}
	}
	return string(data), nil
}

// Stats returns global stats.
func (c *Client) Stats(ctx context.Context) (Stats, error) {
	var out Stats
	err := c.do(ctx, http.MethodGet, "/v1/stats", nil, &out)
	return out, err
}

// Reconcile forces a reconcile cycle.
func (c *Client) Reconcile(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/reconcile", nil, nil)
}

// ListCAs returns managed CAs.
func (c *Client) ListCAs(ctx context.Context) ([]CA, error) {
	var out []CA
	err := c.do(ctx, http.MethodGet, "/v1/pki/cas", nil, &out)
	return out, err
}

// CreateCA creates a new CA.
func (c *Client) CreateCA(ctx context.Context, req CreateCARequest) (CA, error) {
	var out CA
	err := c.do(ctx, http.MethodPost, "/v1/pki/cas", req, &out)
	return out, err
}

// GetCA returns one CA.
func (c *Client) GetCA(ctx context.Context, name string) (CA, error) {
	var out CA
	err := c.do(ctx, http.MethodGet, "/v1/pki/cas/"+name, nil, &out)
	return out, err
}

// DeleteCA removes CA metadata.
func (c *Client) DeleteCA(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/v1/pki/cas/"+name, nil, nil)
}

// ListCertificates lists certs; ca optional filter.
func (c *Client) ListCertificates(ctx context.Context, ca string) ([]Certificate, error) {
	path := "/v1/pki/certs"
	if ca != "" {
		path += "?ca=" + ca
	}
	var out []Certificate
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// IssueCert issues a leaf cert.
func (c *Client) IssueCert(ctx context.Context, req IssueCertRequest) (Certificate, error) {
	var out Certificate
	err := c.do(ctx, http.MethodPost, "/v1/pki/certs", req, &out)
	return out, err
}

// GenerateTLSCrypt creates a tls-crypt key.
func (c *Client) GenerateTLSCrypt(ctx context.Context, name string) (TLSCryptKey, error) {
	var out TLSCryptKey
	err := c.do(ctx, http.MethodPost, "/v1/pki/tls-crypt", TLSCryptRequest{Name: name}, &out)
	return out, err
}

// ListTLSCrypt lists tls-crypt keys.
func (c *Client) ListTLSCrypt(ctx context.Context) ([]TLSCryptKey, error) {
	var out []TLSCryptKey
	err := c.do(ctx, http.MethodGet, "/v1/pki/tls-crypt", nil, &out)
	return out, err
}

// IssueServerCert issues server cert and wires instance PKI paths.
func (c *Client) IssueServerCert(ctx context.Context, instance string, req IssueServerCertRequest) error {
	return c.do(ctx, http.MethodPost, "/v1/instances/"+instance+"/issue-server-cert", req, nil)
}

// IssueClientCert issues client cert and wires client paths.
func (c *Client) IssueClientCert(ctx context.Context, instance, cn string, req IssueClientCertRequest) error {
	return c.do(ctx, http.MethodPost, "/v1/instances/"+instance+"/clients/"+cn+"/issue-cert", req, nil)
}

// ListFeatures returns builtin + custom feature presets.
func (c *Client) ListFeatures(ctx context.Context) ([]FeaturePreset, error) {
	var out []FeaturePreset
	err := c.do(ctx, http.MethodGet, "/v1/features", nil, &out)
	return out, err
}

// UpsertFeature creates or updates a custom feature preset.
func (c *Client) UpsertFeature(ctx context.Context, p FeaturePreset) (FeaturePreset, error) {
	var out FeaturePreset
	err := c.do(ctx, http.MethodPost, "/v1/features", p, &out)
	return out, err
}

// DeleteFeature removes a custom preset.
func (c *Client) DeleteFeature(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/features/"+id, nil, nil)
}
