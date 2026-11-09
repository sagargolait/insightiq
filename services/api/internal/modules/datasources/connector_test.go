package datasources

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestClassifyConnErrorBuckets(t *testing.T) {
	cases := []struct {
		name string
		in   error
		want error
	}{
		{"deadline", context.DeadlineExceeded, ErrConnTimeout},
		{"timeout text", errors.New("dial tcp: i/o timeout"), ErrConnTimeout},
		{"tls", errors.New("tls: handshake failure"), ErrConnTLS},
		{"x509", errors.New("x509: certificate signed by unknown authority"), ErrConnTLS},
		{"refused", errors.New("dial tcp 10.0.0.1:5432: connect: connection refused"), ErrConnUnreachable},
		{"no host", errors.New("lookup nope: no such host"), ErrConnUnreachable},
		{"unknown", errors.New("something weird"), ErrConnRejected},
		{"nil", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifyConnError(c.in)
			if c.want == nil {
				if got != nil {
					t.Errorf("classifyConnError(%v) = %v, want nil", c.in, got)
				}
				return
			}
			if !errors.Is(got, c.want) {
				t.Errorf("classifyConnError(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// The sanitized error must not echo the host, credentials, or raw driver text.
func TestClassifyConnErrorLeaksNothing(t *testing.T) {
	raw := errors.New("dial tcp secret-host.internal:5432: password=hunter2 connection refused")
	got := classifyConnError(raw)
	msg := got.Error()
	for _, leak := range []string{"secret-host", "hunter2", "5432", "password"} {
		if strings.Contains(msg, leak) {
			t.Errorf("sanitized error %q leaked %q", msg, leak)
		}
	}
}

// Ping against an unroutable address returns a sanitized bucket quickly,
// without leaking the target. No external DB required.
func TestPingUnreachableIsSanitized(t *testing.T) {
	c := NewConnector()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 198.51.100.0/24 is TEST-NET-2 (RFC 5737): guaranteed non-routable.
	err := c.Ping(ctx, ConnectionConfig{
		Host: "198.51.100.1", Port: 5432, Database: "x", Username: "u", Password: "p-secret",
	})
	if err == nil {
		t.Skip("unexpected connectivity to TEST-NET address; skipping")
	}
	if strings.Contains(err.Error(), "p-secret") || strings.Contains(err.Error(), "198.51.100.1") {
		t.Errorf("ping error leaked target/credentials: %v", err)
	}
	// Must be one of the known buckets.
	if !(errors.Is(err, ErrConnTimeout) || errors.Is(err, ErrConnUnreachable) ||
		errors.Is(err, ErrConnRejected) || errors.Is(err, ErrConnTLS)) {
		t.Errorf("ping error %v is not a sanitized bucket", err)
	}
}
