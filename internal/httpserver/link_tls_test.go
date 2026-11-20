package httpserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/tlsfiles/tlstest"
)

// linkServer is the server section of a process of a link: its pair and the internal CA, reloaded every 50 ms.
func linkServer(certFile, keyFile, caFile string) config.Server {
	return config.Server{
		TLS:         config.ServerTLS{Certificates: []config.TLSCertificate{{CertFile: certFile, KeyFile: keyFile}}, MinVersion: "1.2", ReloadInterval: 50 * time.Millisecond},
		InternalTLS: config.ServerInternalTLS{CAFile: caFile},
	}
}

// handshake runs a TLS handshake of client against a listener with the listener's TLS and returns the client's error.
func handshake(t *testing.T, l *ListenerTLS, client *tls.Config) error {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		sc := tls.Server(c, l.base)
		_ = sc.SetDeadline(time.Now().Add(5 * time.Second))
		_ = sc.HandshakeContext(context.Background())
		// TLS 1.3 sends the server's refusal of a client certificate after the client's handshake ended: the echo of one byte
		// shows it to the client.
		b := make([]byte, 1)
		if _, err := sc.Read(b); err == nil {
			_, _ = sc.Write(b)
		}
		_ = sc.Close()
	}()
	c, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(t.Context(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	cc := tls.Client(c, client)
	_ = cc.SetDeadline(time.Now().Add(5 * time.Second))
	err = cc.HandshakeContext(t.Context())
	if err == nil {
		_, err = cc.Write([]byte("x"))
		if err == nil {
			_, err = cc.Read(make([]byte, 1))
		}
	}
	_ = cc.Close()
	<-done
	return err
}

func TestR_ARCH_038_TheListenerOfALinkRequiresAClientCertificateOfTheInternalCA(t *testing.T) {
	dir := t.TempDir()
	ca := tlstest.NewCA(t, "internal")
	caFile := ca.WriteCA(t, dir, "ca.crt")
	srvCert, srvKey := ca.Issue(t, tlstest.Options{}).Files(t, dir, "server")
	l, err := NewLinkListenerTLS(LinkScan, linkServer(srvCert, srvKey, caFile))
	if err != nil || l == nil {
		t.Fatalf("%v %v", l, err)
	}
	if err := l.Loaded(); err != nil {
		t.Fatal(err)
	}
	client := func(certs ...tls.Certificate) *tls.Config {
		return &tls.Config{RootCAs: ca.Pool(), ServerName: "localhost", MinVersion: tls.VersionTLS12, Certificates: certs}
	}
	good := ca.Issue(t, tlstest.Options{})
	if err := handshake(t, l, client(good.TLS)); err != nil {
		t.Fatalf("a client of the internal CA: %v", err)
	}
	other := tlstest.NewCA(t, "other").Issue(t, tlstest.Options{})
	expired := ca.Issue(t, tlstest.Options{NotAfter: time.Now().Add(-time.Minute)})
	for name, cfg := range map[string]*tls.Config{
		"another CA's": client(other.TLS), "none": client(), "an expired one": client(expired.TLS),
	} {
		if err := handshake(t, l, cfg); err == nil {
			t.Errorf("a client with %s certificate was admitted", name)
		}
	}

	t.Run("a rotated CA admits the new client from the next handshake, with no restart", func(t *testing.T) {
		next := tlstest.NewCA(t, "internal-2")
		fresh := next.Issue(t, tlstest.Options{})
		if err := handshake(t, l, client(fresh.TLS)); err == nil {
			t.Fatal("a client of the next CA was admitted before the rotation")
		}
		// The bundle holds both CAs during a rotation, as cert-manager's ca.crt does.
		tlstest.WriteFile(t, caFile, append(append([]byte{}, ca.PEM...), next.PEM...))
		if err := l.material.Reload(); err != nil {
			t.Fatal(err)
		}
		if err := handshake(t, l, client(fresh.TLS)); err != nil {
			t.Fatalf("after the rotation: %v", err)
		}
	})
	t.Run("without TLS the link listener is plain, and with TLS it needs the internal CA", func(t *testing.T) {
		if l, err := NewLinkListenerTLS(LinkScan, config.Server{}); l != nil || err != nil {
			t.Fatalf("%v %v", l, err)
		}
		s := linkServer(srvCert, srvKey, "")
		if _, err := NewLinkListenerTLS(LinkScan, s); !errors.Is(err, ErrLinkNeedsCA) {
			t.Fatalf("%v", err)
		}
	})
}

func TestR_ARCH_038_ThePairOfALinkMustAllowClientAuthentication(t *testing.T) {
	dir := t.TempDir()
	ca := tlstest.NewCA(t, "internal")
	caFile := ca.WriteCA(t, dir, "ca.crt")
	serverOnly := ca.Issue(t, tlstest.Options{ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	if err := ClientAuthFit(serverOnly.Cert); err == nil {
		t.Fatal("a pair with serverAuth alone is fit for a link")
	}
	for name, eku := range map[string][]x509.ExtKeyUsage{"both": nil, "none": {}, "clientAuth": {x509.ExtKeyUsageClientAuth}} {
		leaf := ca.Issue(t, tlstest.Options{ExtKeyUsage: eku})
		if err := ClientAuthFit(leaf.Cert); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	certFile, keyFile := serverOnly.Files(t, dir, "server-only")
	link, err := NewLinkTLS(LinkScanner, linkServer(certFile, keyFile, caFile), "localhost", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := link.Fit(); err == nil {
		t.Fatal("the link reports an unfit pair as fit")
	}
	srv, err := NewLinkListenerTLS(LinkScanner, linkServer(certFile, keyFile, caFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.ClientAuthFit(); err == nil {
		t.Fatal("the scanner's listener reports an unfit pair as fit")
	}

	t.Run("the client side of a link presents its pair and trusts only the internal CA", func(t *testing.T) {
		goodCert, goodKey := ca.Issue(t, tlstest.Options{}).Files(t, dir, "good")
		s := linkServer(goodCert, goodKey, caFile)
		client, err := NewLinkTLS(LinkScanner, s, "localhost", nil)
		if err != nil {
			t.Fatal(err)
		}
		listener, err := NewLinkListenerTLS(LinkScanner, s)
		if err != nil {
			t.Fatal(err)
		}
		if err := handshake(t, listener, client.Config()); err != nil {
			t.Fatalf("the link: %v", err)
		}
		foreign := tlstest.NewCA(t, "foreign")
		fc, fk := foreign.Issue(t, tlstest.Options{}).Files(t, dir, "foreign")
		stranger, err := NewLinkListenerTLS(LinkScanner, linkServer(fc, fk, foreign.WriteCA(t, dir, "foreign.crt")))
		if err != nil {
			t.Fatal(err)
		}
		if err := handshake(t, stranger, client.Config()); err == nil {
			t.Fatal("the client trusted a server of another CA")
		}
	})
}
