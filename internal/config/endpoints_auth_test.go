package config

import (
	"slices"
	"strings"
	"testing"
)

func TestR_EP_010_CredentialRuleConfiguration(t *testing.T) {
	t.Run("R-EP-010 an empty list of token types stops start-up instead of accepting every type", func(t *testing.T) {
		_, err := loadEndpoints(t, customersYAML+"      auth: {token_types: []}\n")
		if err == nil || !strings.Contains(err.Error(), "server.endpoints.customers.auth.token_types is empty") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("R-EP-010 Q-EP-10 the defaults: SATs bound to the endpoint, no anonymous; the default endpoint unchanged", func(t *testing.T) {
		l, err := loadEndpoints(t, customersYAML)
		if err != nil {
			t.Fatal(err)
		}
		eps := l.Config.Server.ResolveEndpoints()
		def, cust := eps[0], eps[1]
		if !slices.Equal(cust.TokenTypes, []string{"sat"}) || !cust.BoundTokensOnly || cust.Anonymous || cust.Exposure != ExposureRules || cust.RateLimitClass != "external" {
			t.Fatalf("customers: %+v", cust)
		}
		if !slices.Equal(def.TokenTypes, []string{"pat", "sat", "cit"}) || def.BoundTokensOnly || !def.Anonymous || def.Exposure != ExposureAll || def.RateLimitClass != "" {
			t.Fatalf("default: %+v", def)
		}
	})
}

// TestR_CLAMAV_010_ClientCertificatesNeedAFurtherEndpointThatVerifiesThem: auth.client_certificates is a credential only on a further
// endpoint with tls.client_ca_file and without auth.anonymous (R-EP-001, R-EP-010, formats/clamav.md R-CLAMAV-010); each violation stops
// startup and names the key.
func TestR_CLAMAV_010_ClientCertificatesNeedAFurtherEndpointThatVerifiesThem(t *testing.T) {
	const tlsBlock = "      tls: {certificates: [{cert_file: /tls/c.crt, key_file: /tls/c.key}], client_ca_file: /tls/ca.crt}\n"
	t.Run("R-CLAMAV-010 with the client CA it loads and resolves the key", func(t *testing.T) {
		l, err := loadEndpoints(t, customersYAML+tlsBlock+"      auth: {client_certificates: true}\n")
		if err != nil {
			t.Fatal(err)
		}
		eps := l.Config.Server.ResolveEndpoints()
		if eps[0].ClientCertificates || !eps[1].ClientCertificates || eps[1].TLS.ClientAuth != ClientAuthRequire {
			t.Fatalf("default %+v, customers %+v", eps[0], eps[1])
		}
	})
	const noCA = "      tls: {certificates: [{cert_file: /tls/c.crt, key_file: /tls/c.key}]}\n"
	for _, tc := range []struct{ name, yaml, want string }{
		{
			name: "without tls.client_ca_file", yaml: customersYAML + noCA + "      auth: {client_certificates: true}\n",
			want: "server.endpoints.customers.auth.client_certificates is true without",
		},
		{
			name: "on the default endpoint", yaml: customersYAML + "    default:\n      auth: {client_certificates: true}\n",
			want: "server.endpoints.default.auth.client_certificates is true on the default endpoint",
		},
		{
			name: "with auth.anonymous", yaml: customersYAML + tlsBlock + "      auth: {client_certificates: true, anonymous: true}\n",
			want: "server.endpoints.customers.auth.client_certificates is true while",
		},
	} {
		t.Run("R-CLAMAV-010 "+tc.name+" stops startup", func(t *testing.T) {
			_, err := loadEndpoints(t, tc.yaml)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
