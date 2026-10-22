package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func postCloudInit(t *testing.T, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/cloud-init", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handleGenerateCloudInit(w, req)
	return w.Result()
}

func TestGenerateCloudInitEmbedsAuthKey(t *testing.T) {
	res := postCloudInit(t, `{"authKey":"tskey-auth-abc123"}`)
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}
	if got := res.Header.Get("Content-Disposition"); got != `attachment; filename="tailboot-cloud-init.sh"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	body, _ := io.ReadAll(res.Body)
	script := string(body)
	if !strings.HasPrefix(script, "#!/bin/sh\n") {
		t.Fatalf("script must start with a shebang so cloud-init runs it, got %q", script[:20])
	}
	if !strings.Contains(script, `printf '%s\n' 'tskey-auth-abc123' > /etc/tailboot/auth.key`) {
		t.Fatalf("auth key not written to key file:\n%s", script)
	}
	if !strings.Contains(script, "--auth-key=file:/etc/tailboot/auth.key --ssh") {
		t.Fatalf("tailscale up must read the key file and enable SSH:\n%s", script)
	}
}

func TestGenerateCloudInitRequiresAuthKey(t *testing.T) {
	for _, body := range []string{`{}`, `{"authKey":""}`, `not json`} {
		if res := postCloudInit(t, body); res.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %q: status %d, want 400", body, res.StatusCode)
		}
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"tskey-auth-abc": `'tskey-auth-abc'`,
		"it's":           `'it'\''s'`,
		"$(rm -rf /)":    `'$(rm -rf /)'`,
		"a b;c":          `'a b;c'`,
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

// A hostile key must reach the key file verbatim and never execute.
func TestCloudInitScriptIsValidShellAndKeepsKeyLiteral(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	key := `k'; touch /tmp/pwned; echo '$(id)`
	script := renderCloudInit(key)

	if out, err := exec.Command(sh, "-n", "-c", script).CombinedOutput(); err != nil {
		t.Fatalf("script has a syntax error: %v\n%s", err, out)
	}

	// Run only the key-writing line against a scratch file.
	var line string
	for _, l := range strings.Split(script, "\n") {
		if strings.HasSuffix(l, " > /etc/tailboot/auth.key") {
			line = strings.TrimSuffix(l, " > /etc/tailboot/auth.key")
		}
	}
	if line == "" {
		t.Fatal("key-writing line not found")
	}
	out, err := exec.Command(sh, "-c", line).Output()
	if err != nil {
		t.Fatalf("run quoted key: %v", err)
	}
	if got := strings.TrimSuffix(string(out), "\n"); got != key {
		t.Fatalf("key round-trip = %q, want %q", got, key)
	}
}

func TestCloudInitInstallsDashboardForCloudPaths(t *testing.T) {
	script := renderCloudInit("tskey-auth-abc123")
	if !strings.Contains(script, "\nTAILBOOT_DASHBOARD_EOF\n") {
		t.Fatal("dashboard heredoc is not terminated on its own line")
	}
	if strings.Contains(script, "/run/tailboot/auth.key") {
		t.Fatal("dashboard still reads the ISO's key path")
	}
	if !strings.Contains(script, "[ -s /etc/tailboot/auth.key ]") {
		t.Fatal("dashboard does not read the cloud key path")
	}
	if !strings.Contains(script, "TAILBOOT_DASHBOARD=1 /usr/local/sbin/tailboot-dashboard") {
		t.Fatal("login hook does not start the dashboard with its re-entry guard")
	}
}

// server/tailboot-dashboard is a copy of the ISO's script so the Docker build
// context stays server/. Fail if they drift (skipped when building from
// server/ alone).
func TestDashboardMatchesISOScript(t *testing.T) {
	iso, err := os.ReadFile("../image/config/includes.chroot/usr/local/sbin/tailboot-dashboard")
	if err != nil {
		t.Skip("ISO dashboard not in this checkout")
	}
	// A Windows checkout with autocrlf turns the ISO's copy into CRLF.
	if strings.ReplaceAll(string(iso), "\r\n", "\n") != dashboardScript {
		t.Fatal("server/tailboot-dashboard differs from the ISO's tailboot-dashboard; copy one over the other")
	}
}

func TestCloudInitScriptHasNoComments(t *testing.T) {
	for i, line := range strings.Split(renderCloudInit("tskey-auth-abc123"), "\n") {
		if s := strings.TrimSpace(line); strings.HasPrefix(s, "#") && !strings.HasPrefix(s, "#!") {
			t.Fatalf("line %d is a comment: %q", i+1, line)
		}
	}
}
