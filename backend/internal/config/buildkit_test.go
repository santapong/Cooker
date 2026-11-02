package config

import "testing"

func TestBuildKitContractConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		env    Env
		bk     BuildKitConfig
		pusher string
		ok     bool
	}{
		{"missing endpoint", EnvDev, BuildKitConfig{}, "crane", false},
		{"unsupported transfer", EnvDev, BuildKitConfig{Address: "unix:///run/buildkit/buildkitd.sock"}, "docker", false},
		{"socket", EnvProduction, BuildKitConfig{Address: "unix:///run/buildkit/buildkitd.sock"}, "crane", true},
		{"insecure tcp dev", EnvDev, BuildKitConfig{Address: "tcp://127.0.0.1:1234"}, "crane", true},
		{"insecure tcp prod", EnvProduction, BuildKitConfig{Address: "tcp://buildkit:1234"}, "crane", false},
		{"mtls", EnvProduction, BuildKitConfig{Address: "tcp://buildkit:1234", TLSCA: "ca.pem", TLSCert: "cert.pem", TLSKey: "key.pem"}, "crane", true},
		{"partial tls", EnvDev, BuildKitConfig{Address: "tcp://buildkit:1234", TLSCert: "cert.pem"}, "crane", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{Env: tc.env, BuilderBackend: "buildkit", PusherBackend: tc.pusher, BuildKit: tc.bk}
			err := c.validateBuildKit()
			if (err == nil) != tc.ok {
				t.Fatalf("err=%v allowed=%v", err, tc.ok)
			}
		})
	}
}
func TestLoadBuildKitConfiguration(t *testing.T) {
	t.Setenv("COOKER_BUILDKIT_ADDR", "unix:///fixture/buildkit.sock")
	c := Load()
	if c.BuildKit.Address != "unix:///fixture/buildkit.sock" {
		t.Fatalf("endpoint missing: %+v", c.BuildKit)
	}
}
