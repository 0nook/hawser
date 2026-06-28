package config

import "testing"

func TestIsLoopbackBind(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":   true,
		"127.0.0.5":   true,
		"::1":         true,
		"localhost":   true,
		"":            false, // empty == all interfaces
		"0.0.0.0":     false,
		"::":          false,
		"192.168.1.7": false,
		"example.com": false, // unresolvable hostname -> fail closed
	}
	for addr, want := range cases {
		if got := isLoopbackBind(addr); got != want {
			t.Errorf("isLoopbackBind(%q) = %v, want %v", addr, got, want)
		}
	}
}

// newStandardCfg builds a minimal standard-mode config. DockerHost is set so
// validate() skips the socket-exists check, isolating the auth/bind logic.
func newStandardCfg(bind, token string, allow bool) *Config {
	return &Config{
		Port:                2376,
		BindAddress:         bind,
		Token:               token,
		AllowInsecureNoAuth: allow,
		DockerHost:          "tcp://localhost:2375",
	}
}

func TestValidate_StandardMode_NoAuthBind(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{"non-loopback no token rejected", newStandardCfg("0.0.0.0", "", false), true},
		{"specific LAN ip no token rejected", newStandardCfg("192.168.1.7", "", false), true},
		{"empty bind no token rejected", newStandardCfg("", "", false), true},
		{"non-loopback with token allowed", newStandardCfg("0.0.0.0", "secret", false), false},
		{"loopback no token allowed", newStandardCfg("127.0.0.1", "", false), false},
		{"localhost no token allowed", newStandardCfg("localhost", "", false), false},
		{"non-loopback no token with override allowed", newStandardCfg("0.0.0.0", "", true), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Edge mode must not be affected by the standard-mode bind check.
func TestValidate_EdgeMode_Unaffected(t *testing.T) {
	cfg := &Config{
		DockhandServerURL: "wss://dockhand.example.com/api/hawser/connect",
		Token:             "secret",
		Port:              2376,
		BindAddress:       "0.0.0.0",
		DockerHost:        "tcp://localhost:2375",
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("edge-mode validate() unexpected error: %v", err)
	}
}
