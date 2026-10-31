// Package xmail is a small client for xmail's REST API (/api/v1). Only the
// XAuth server uses it; browsers never talk to xmail and never see the key.
package xmail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MaxResponse caps how much of an xmail response is read. Mail bodies with
// inline HTML are the largest responses.
const MaxResponse = 4 << 20

// Timeout bounds one call to xmail.
const Timeout = 15 * time.Second

var (
	// ErrTooLarge is returned when xmail sends more than MaxResponse bytes.
	ErrTooLarge = errors.New("xmail response too large")
	// ErrBadResponse is returned when the body is not xmail's JSON envelope.
	ErrBadResponse = errors.New("xmail returned an unrecognised response")
)

// Client calls one xmail deployment with one API key.
type Client struct {
	base *url.URL
	key  string
	http *http.Client
}

// New returns a client for baseURL (scheme://host, without /api/v1).
func New(baseURL, apiKey string) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("xmail: invalid base URL %q", baseURL)
	}
	if apiKey == "" {
		return nil, errors.New("xmail: empty API key")
	}
	return &Client{base: u, key: apiKey, http: &http.Client{
		Timeout: Timeout,
		// Never follow redirects: the Authorization header must only go
		// to the configured host.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// Result is xmail's response: the HTTP status and the envelope fields.
type Result struct {
	Status  int
	Success bool
	Data    json.RawMessage
	Error   string
}

// OK reports whether xmail accepted the request.
func (r *Result) OK() bool { return r.Success && r.Status >= 200 && r.Status < 300 }

// Decode unmarshals the data field into v.
func (r *Result) Decode(v any) error {
	if len(r.Data) == 0 {
		return ErrBadResponse
	}
	return json.Unmarshal(r.Data, v)
}

// URL builds the request URL. Each segment is escaped on its own, so a
// segment can never add path levels, change the host or climb with "..".
func (c *Client) URL(segments []string, query url.Values) string {
	var b strings.Builder
	b.WriteString(c.base.Scheme + "://" + c.base.Host)
	b.WriteString(strings.TrimRight(c.base.EscapedPath(), "/"))
	b.WriteString("/api/v1")
	for _, s := range segments {
		b.WriteByte('/')
		b.WriteString(url.PathEscape(s))
	}
	if len(query) > 0 {
		b.WriteByte('?')
		b.WriteString(query.Encode())
	}
	return b.String()
}

// Do calls xmail. body is sent as JSON when non-nil. A non-2xx status is not
// an error: the Result carries it with xmail's message.
func (c *Client) Do(ctx context.Context, method string, segments []string, query url.Values, body []byte) (*Result, error) {
	for _, s := range segments {
		if s == "" || s == "." || s == ".." {
			return nil, fmt.Errorf("xmail: invalid path segment %q", s)
		}
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.URL(segments, query), rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		// url.Error includes the URL, which is not secret; the key is in a
		// header and never part of the message.
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, MaxResponse+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxResponse {
		return nil, ErrTooLarge
	}
	var env struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   string          `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("%w (HTTP %d)", ErrBadResponse, res.StatusCode)
	}
	return &Result{Status: res.StatusCode, Success: env.Success, Data: env.Data, Error: env.Error}, nil
}

// Get is Do with GET and no body.
func (c *Client) Get(ctx context.Context, segments []string, query url.Values) (*Result, error) {
	return c.Do(ctx, http.MethodGet, segments, query, nil)
}
