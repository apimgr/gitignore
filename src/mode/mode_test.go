package mode

import (
	"strings"
	"testing"
)

// resetState restores the package-level mode/debug globals to their zero
// state after a test so tests don't leak state into each other (these
// helpers manipulate shared package globals guarded by mu).
func resetState(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		mu.Lock()
		current = Production
		debug = false
		mu.Unlock()
	})
}

func TestParseMode(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    Mode
		wantErr bool
	}{
		{"dev short", "dev", Development, false},
		{"development full", "development", Development, false},
		{"debug alias", "debug", Development, false},
		{"prod short", "prod", Production, false},
		{"production full", "production", Production, false},
		{"case insensitive", "PRODUCTION", Production, false},
		{"whitespace trimmed", "  dev  ", Development, false},
		{"invalid", "bogus", "", true},
		{"empty", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseMode(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseMode(%q) expected error, got nil", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMode(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseMode(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestSetAndGet(t *testing.T) {
	resetState(t)

	if err := Set("development"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if Get() != Development {
		t.Errorf("Get() = %v, want Development", Get())
	}
	if !IsAppModeDev() {
		t.Error("IsAppModeDev() = false, want true")
	}
	if IsAppModeProd() {
		t.Error("IsAppModeProd() = true, want false")
	}

	if err := Set("production"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !IsAppModeProd() {
		t.Error("IsAppModeProd() = false, want true after Set(production)")
	}
}

func TestSetInvalidModeReturnsErrorAndLeavesCurrentUnchanged(t *testing.T) {
	resetState(t)

	if err := Set("development"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := Set("bogus"); err == nil {
		t.Fatal("Set(bogus) expected error, got nil")
	}
	if Get() != Development {
		t.Errorf("Get() = %v after failed Set, want unchanged Development", Get())
	}
}

func TestSetDebugAliasEnablesDebug(t *testing.T) {
	resetState(t)

	if IsDebugEnabled() {
		t.Fatal("debug should start disabled")
	}
	if err := Set("debug"); err != nil {
		t.Fatalf("Set(debug): %v", err)
	}
	if Get() != Development {
		t.Errorf("Get() = %v, want Development for debug alias", Get())
	}
	if !IsDebugEnabled() {
		t.Error("IsDebugEnabled() = false, want true after Set(debug)")
	}
}

func TestSetDebugIndependentOfMode(t *testing.T) {
	resetState(t)

	SetDebug(true)
	if !IsDebugEnabled() {
		t.Fatal("SetDebug(true) did not enable debug")
	}
	if IsAppModeDev() {
		t.Error("SetDebug should not change mode")
	}

	SetDebug(false)
	if IsDebugEnabled() {
		t.Error("SetDebug(false) did not disable debug")
	}
}

func TestGetErrorDetail(t *testing.T) {
	resetState(t)

	if got := GetErrorDetail(nil); got != "" {
		t.Errorf("GetErrorDetail(nil) = %q, want empty", got)
	}

	err := &testError{"boom"}

	if setErr := Set("production"); setErr != nil {
		t.Fatalf("Set: %v", setErr)
	}
	prodMsg := GetErrorDetail(err)
	if prodMsg == "" || strings.Contains(prodMsg, "boom") {
		t.Errorf("production error detail = %q, want generic message without internals", prodMsg)
	}

	if setErr := Set("development"); setErr != nil {
		t.Fatalf("Set: %v", setErr)
	}
	devMsg := GetErrorDetail(err)
	if !strings.Contains(devMsg, "boom") {
		t.Errorf("development error detail = %q, want to contain internal message", devMsg)
	}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

func TestModeGatedHelpers(t *testing.T) {
	resetState(t)

	if err := Set("production"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !ShouldCacheTemplates() {
		t.Error("ShouldCacheTemplates() = false in production, want true")
	}
	if !ShouldCacheStaticFiles() {
		t.Error("ShouldCacheStaticFiles() = false in production, want true")
	}
	if ShouldEnableAutoReload() {
		t.Error("ShouldEnableAutoReload() = true in production, want false")
	}
	if GetPanicRecoveryMode() != "graceful" {
		t.Errorf("GetPanicRecoveryMode() = %q, want graceful", GetPanicRecoveryMode())
	}

	if err := Set("development"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if ShouldCacheTemplates() {
		t.Error("ShouldCacheTemplates() = true in development, want false")
	}
	if ShouldCacheStaticFiles() {
		t.Error("ShouldCacheStaticFiles() = true in development, want false")
	}
	if !ShouldEnableAutoReload() {
		t.Error("ShouldEnableAutoReload() = false in development, want true")
	}
	if GetPanicRecoveryMode() != "verbose" {
		t.Errorf("GetPanicRecoveryMode() = %q, want verbose", GetPanicRecoveryMode())
	}
}

func TestDebugGatedHelpersIndependentOfMode(t *testing.T) {
	resetState(t)

	if err := Set("development"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if ShouldShowDebugEndpoints() {
		t.Error("ShouldShowDebugEndpoints() = true without explicit debug, want false")
	}
	if ShouldEnableProfiling() {
		t.Error("ShouldEnableProfiling() = true without explicit debug, want false")
	}

	SetDebug(true)
	if !ShouldShowDebugEndpoints() {
		t.Error("ShouldShowDebugEndpoints() = false after SetDebug(true), want true")
	}
	if !ShouldEnableProfiling() {
		t.Error("ShouldEnableProfiling() = false after SetDebug(true), want true")
	}
}

func TestInitFromEnv(t *testing.T) {
	resetState(t)

	t.Run("valid mode env", func(t *testing.T) {
		resetState(t)
		t.Setenv("MODE", "development")
		Init()
		if !IsAppModeDev() {
			t.Error("Init() with MODE=development did not set development mode")
		}
	})

	t.Run("invalid mode env keeps default", func(t *testing.T) {
		resetState(t)
		t.Setenv("MODE", "bogus")
		Init()
		if !IsAppModeProd() {
			t.Error("Init() with invalid MODE should keep default production")
		}
	})

	t.Run("empty mode env is no-op", func(t *testing.T) {
		resetState(t)
		t.Setenv("MODE", "")
		Init()
		if !IsAppModeProd() {
			t.Error("Init() with empty MODE should keep default production")
		}
	})
}

func TestInitDebugFromEnv(t *testing.T) {
	t.Run("unset env leaves debug untouched", func(t *testing.T) {
		resetState(t)
		SetDebug(true)
		InitDebug()
		if !IsDebugEnabled() {
			t.Error("InitDebug() with unset DEBUG should not change debug state")
		}
	})

	t.Run("DEBUG=true enables debug", func(t *testing.T) {
		resetState(t)
		t.Setenv("DEBUG", "true")
		InitDebug()
		if !IsDebugEnabled() {
			t.Error("InitDebug() with DEBUG=true should enable debug")
		}
	})

	t.Run("DEBUG=false overrides debug mode alias", func(t *testing.T) {
		resetState(t)
		if err := Set("debug"); err != nil {
			t.Fatalf("Set(debug): %v", err)
		}
		t.Setenv("DEBUG", "false")
		InitDebug()
		if IsDebugEnabled() {
			t.Error("InitDebug() with DEBUG=false should disable debug even after debug alias")
		}
		if !IsAppModeDev() {
			t.Error("mode should remain development from the debug alias")
		}
	})

	t.Run("invalid DEBUG value ignored", func(t *testing.T) {
		resetState(t)
		SetDebug(true)
		t.Setenv("DEBUG", "not-a-bool")
		InitDebug()
		if !IsDebugEnabled() {
			t.Error("InitDebug() with invalid DEBUG value should leave existing debug state")
		}
	})
}

func TestModeStringer(t *testing.T) {
	if Production.String() != "production" {
		t.Errorf("Production.String() = %q", Production.String())
	}
	if Development.String() != "development" {
		t.Errorf("Development.String() = %q", Development.String())
	}
}
