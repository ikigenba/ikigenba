package website

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const stubName = "website-stub.test"

// Stub serves fixture websites on two loopback listeners.
type Stub struct {
	plain, secure *http.Server
	client        *http.Client
	closed        atomic.Bool
}

// NewStub starts the plain and TLS fixtures and their verifying client.
func NewStub() (*Stub, error) {
	cert, root, err := stubCertificate()
	if err != nil {
		return nil, err
	}
	plain, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	secure, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = plain.Close()
		return nil, err
	}
	s := &Stub{}
	makeServer := func() *http.Server {
		return &http.Server{Handler: http.HandlerFunc(stubPage), ReadHeaderTimeout: Timeout, ErrorLog: log.New(io.Discard, "", 0)}
	}
	s.plain = makeServer()
	s.secure = makeServer()
	tlsListener := tls.NewListener(secure, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	go func() { _ = s.plain.Serve(plain) }()
	go func() { _ = s.secure.Serve(tlsListener) }()
	roots := x509.NewCertPool()
	roots.AddCert(root)
	s.client = &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: stubName, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, e := net.SplitHostPort(address)
			if e != nil {
				return nil, e
			}
			key := keyword(host)
			if s.closed.Load() || key == "" || key == "refused" || (key == "plain" && port == "443") || (key == "prefixed" && !strings.HasPrefix(host, "www.")) {
				return nil, errors.New("stub connection refused")
			}
			target := plain.Addr().String()
			if port == "443" {
				target = secure.Addr().String()
			} else if port != "80" {
				return nil, errors.New("stub port refused")
			}
			return (&net.Dialer{}).DialContext(ctx, network, target)
		},
	}}
	return s, nil
}

// Client returns the stub's client, which dials only its two listeners.
func (s *Stub) Client() *http.Client { return s.client }

// Close stops both listeners and closes all idle client connections.
func (s *Stub) Close() error {
	s.closed.Store(true)
	s.client.CloseIdleConnections()
	return errors.Join(s.plain.Close(), s.secure.Close())
}

func keyword(host string) string {
	base := strings.TrimPrefix(strings.ToLower(host), "www.")
	if !strings.HasSuffix(base, ".test") {
		return ""
	}
	first, _, _ := strings.Cut(base, ".")
	for _, k := range []string{"secure", "plain", "prefixed", "upgrade", "away", "missing", "empty", "binary", "refused", "slow", "big", "loop"} {
		if first == k || strings.HasPrefix(first, k+"-") {
			return k
		}
	}
	return ""
}

func stubPage(w http.ResponseWriter, r *http.Request) {
	host := strings.ToLower(r.URL.Hostname())
	if host == "" {
		host = strings.ToLower(r.Host)
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
	}
	key := keyword(host)
	switch key {
	case "slow":
		<-r.Context().Done()
		return
	case "away":
		w.Header().Set("Location", "https://secure.test/")
		w.WriteHeader(http.StatusFound)
		return
	case "loop":
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		w.Header().Set("Location", scheme+"://"+host+r.URL.RequestURI())
		w.WriteHeader(http.StatusFound)
		return
	case "binary":
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(make([]byte, 1024))
		return
	case "upgrade":
		if r.TLS == nil {
			w.Header().Set("Location", "https://"+host+"/home")
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		if r.URL.Path != "/home" {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, "<html></html>")
			return
		}
	case "missing":
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "<html></html>")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if key == "empty" {
		return
	}
	body := "<html><head><title>" + html.EscapeString(host) + "</title></head><body></body></html>"
	if key == "big" {
		body += strings.Repeat(" ", 300000-len(body))
	}
	_, _ = io.WriteString(w, body)
}

func stubCertificate() (tls.Certificate, *x509.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: stubName}, DNSNames: []string{stubName}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	root, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("parse stub certificate: %w", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: root}, root, nil
}
