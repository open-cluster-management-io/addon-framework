package assets

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func generateTestCertPEM(t *testing.T, commonName string, notBefore, notAfter time.Time) []byte {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatalf("failed to encode certificate: %v", err)
	}

	return buf.Bytes()
}

func TestNotAfter(t *testing.T) {
	notAfterTime := time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)
	certPEM := generateTestCertPEM(t, "test", time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC), notAfterTime)

	cases := []struct {
		name      string
		certBytes []byte
		expected  string
	}{
		{
			name:      "empty bytes",
			certBytes: nil,
			expected:  "",
		},
		{
			name:      "valid cert",
			certBytes: certPEM,
			expected:  notAfterTime.Format(time.RFC3339),
		},
	}

	for _, c := range cases {
		r := notAfter(c.certBytes)
		if r != c.expected {
			t.Errorf("Name %s : expected %s, but got %s", c.name, c.expected, r)
		}
	}
}

func TestNotBefore(t *testing.T) {
	notBeforeTime := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	certPEM := generateTestCertPEM(t, "test", notBeforeTime, time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC))

	cases := []struct {
		name      string
		certBytes []byte
		expected  string
	}{
		{
			name:      "empty bytes",
			certBytes: nil,
			expected:  "",
		},
		{
			name:      "valid cert",
			certBytes: certPEM,
			expected:  notBeforeTime.Format(time.RFC3339),
		},
	}

	for _, c := range cases {
		r := notBefore(c.certBytes)
		if r != c.expected {
			t.Errorf("Name %s : expected %s, but got %s", c.name, c.expected, r)
		}
	}
}

func TestIssuer(t *testing.T) {
	certPEM := generateTestCertPEM(t, "my-issuer", time.Now(), time.Now().Add(time.Hour))

	cases := []struct {
		name      string
		certBytes []byte
		expected  string
	}{
		{
			name:      "empty bytes",
			certBytes: nil,
			expected:  "",
		},
		{
			name:      "valid cert",
			certBytes: certPEM,
			expected:  "my-issuer",
		},
	}

	for _, c := range cases {
		r := issuer(c.certBytes)
		if r != c.expected {
			t.Errorf("Name %s : expected %s, but got %s", c.name, c.expected, r)
		}
	}
}

func TestBase64encode(t *testing.T) {
	cases := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "empty",
			input:    []byte{},
			expected: "",
		},
		{
			name:     "simple string",
			input:    []byte("hello world"),
			expected: "aGVsbG8gd29ybGQ=",
		},
	}

	for _, c := range cases {
		r := base64encode(c.input)
		if r != c.expected {
			t.Errorf("Name %s : expected %s, but got %s", c.name, c.expected, r)
		}
	}
}

func TestIndent(t *testing.T) {
	cases := []struct {
		name      string
		indention int
		input     []byte
		expected  string
	}{
		{
			name:      "no newlines",
			indention: 2,
			input:     []byte("single line"),
			expected:  "single line",
		},
		{
			name:      "multiple lines",
			indention: 2,
			input:     []byte("line1\nline2\nline3"),
			expected:  "line1\n  line2\n  line3",
		},
		{
			name:      "zero indention",
			indention: 0,
			input:     []byte("a\nb"),
			expected:  "a\nb",
		},
	}

	for _, c := range cases {
		r := indent(c.indention, c.input)
		if r != c.expected {
			t.Errorf("Name %s : expected %q, but got %q", c.name, c.expected, r)
		}
	}
}

func TestLoad(t *testing.T) {
	assets := map[string][]byte{
		"manifests/deployment.yaml": []byte("kind: Deployment"),
	}

	cases := []struct {
		name     string
		key      string
		assets   map[string][]byte
		expected []byte
	}{
		{
			name:     "existing key",
			key:      "manifests/deployment.yaml",
			assets:   assets,
			expected: []byte("kind: Deployment"),
		},
		{
			name:     "missing key",
			key:      "does/not/exist.yaml",
			assets:   assets,
			expected: nil,
		},
	}

	for _, c := range cases {
		r := load(c.key, c.assets)
		if !bytes.Equal(r, c.expected) {
			t.Errorf("Name %s : expected %q, but got %q", c.name, c.expected, r)
		}
	}
}

func TestRegexMatch(t *testing.T) {
	cases := []struct {
		name     string
		pattern  string
		input    string
		expected bool
	}{
		{
			name:     "match",
			pattern:  "^addon-.*$",
			input:    "addon-manager",
			expected: true,
		},
		{
			name:     "no match",
			pattern:  "^addon-.*$",
			input:    "cluster-manager",
			expected: false,
		},
		{
			name:     "invalid pattern",
			pattern:  "[",
			input:    "anything",
			expected: false,
		},
	}

	for _, c := range cases {
		r := regexMatch(c.pattern, c.input)
		if r != c.expected {
			t.Errorf("Name %s : expected %t, but got %t", c.name, c.expected, r)
		}
	}
}

func TestRenderFile(t *testing.T) {
	cases := []struct {
		name        string
		template    string
		data        interface{}
		expected    string
		expectError bool
	}{
		{
			name:     "simple substitution",
			template: "hello {{ .Name }}",
			data:     struct{ Name string }{Name: "world"},
			expected: "hello world",
		},
		{
			name:     "uses base64 func from funcMap",
			template: "encoded: {{ base64 .Data }}",
			data:     struct{ Data []byte }{Data: []byte("hi")},
			expected: "encoded: aGk=",
		},
		{
			name:        "malformed template",
			template:    "{{ .Name",
			data:        struct{ Name string }{Name: "world"},
			expectError: true,
		},
		{
			name:        "missing field",
			template:    "{{ .Missing }}",
			data:        struct{ Name string }{Name: "world"},
			expectError: true,
		},
	}

	for _, c := range cases {
		r, err := renderFile("test", []byte(c.template), c.data)
		if c.expectError {
			if err == nil {
				t.Errorf("Name %s : expected error, got none", c.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("Name %s : unexpected error: %v", c.name, err)
			continue
		}
		if string(r) != c.expected {
			t.Errorf("Name %s : expected %q, but got %q", c.name, c.expected, string(r))
		}
	}
}
