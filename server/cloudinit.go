package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// cloudInitScript is a plain shell script rather than #cloud-config YAML so
// the same artifact works as cloud-init user data (AWS, Azure, DigitalOcean,
// Hetzner, Vultr, Linode, OCI, ...) and as a startup script on providers
// that don't run cloud-init (GCP). It does what the ISO does at boot, using
// Tailscale's own installer instead of pinning a repo, so there is nothing
// here to keep in step with Debian or Tailscale.
//
// __AUTH_KEY__ is replaced with the shell-quoted auth key.
const cloudInitScript = `#!/bin/sh
set -eu
umask 077

mkdir -p /etc/tailboot
printf '%s\n' __AUTH_KEY__ > /etc/tailboot/auth.key

export DEBIAN_FRONTEND=noninteractive
if ! command -v curl >/dev/null 2>&1 || ! command -v jq >/dev/null 2>&1; then
  apt-get update
  apt-get install --yes --no-install-recommends ca-certificates curl jq
fi

if ! command -v tailscale >/dev/null 2>&1; then
  curl --fail --silent --show-error --location https://tailscale.com/install.sh | sh
fi

mkdir -p /etc/systemd/system/tailscaled.service.d
cat > /etc/systemd/system/tailscaled.service.d/tailboot.conf <<'EOF'
[Service]
ExecStartPre=-/bin/rm -f /var/lib/tailscale/tailscaled.state
EOF

cat > /etc/systemd/system/tailboot.service <<'EOF'
[Unit]
Description=Join Tailscale and enable Tailscale SSH
Wants=network-online.target
After=network-online.target tailscaled.service
Requires=tailscaled.service

[Service]
Type=oneshot
ExecStart=/usr/bin/tailscale up --auth-key=file:/etc/tailboot/auth.key --ssh --timeout=2m
RemainAfterExit=yes
Restart=on-failure
RestartSec=15s

[Install]
WantedBy=multi-user.target
EOF

cat > /usr/local/sbin/tailboot-dashboard <<'TAILBOOT_DASHBOARD_EOF'
__DASHBOARD__TAILBOOT_DASHBOARD_EOF
chmod 0755 /usr/local/sbin/tailboot-dashboard

cat > /etc/profile.d/tailboot-dashboard.sh <<'EOF'
case $- in
  *i*) [ -t 1 ] && [ -z "${TAILBOOT_DASHBOARD:-}" ] && TAILBOOT_DASHBOARD=1 /usr/local/sbin/tailboot-dashboard ;;
esac
EOF

systemctl daemon-reload
systemctl restart tailscaled.service
systemctl enable --now tailboot.service
`

// shellQuote single-quotes s for POSIX sh, so no auth key can break out of
// the script.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// The dashboard is the ISO's own script. On a cloud VM the auth key lives in
// /etc/tailboot rather than the ISO's /run/tailboot.
//
//go:embed tailboot-dashboard
var dashboardScript string

// stripComments drops whole-line comments, keeping the shebang, and collapses
// the blank-line runs they leave behind, so the generated script is just code.
func stripComments(script string) string {
	var out []string
	for i, line := range strings.Split(script, "\n") {
		if i > 0 && strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if line == "" && len(out) > 0 && out[len(out)-1] == "" {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func renderCloudInit(authKey string) string {
	dashboard := stripComments(strings.ReplaceAll(dashboardScript, "/run/tailboot/auth.key", "/etc/tailboot/auth.key"))
	script := strings.Replace(cloudInitScript, "__DASHBOARD__", dashboard, 1)
	return strings.Replace(script, "__AUTH_KEY__", shellQuote(authKey), 1)
}

// handleGenerateCloudInit serves the same auth-key-only request as the ISO
// endpoint, for machines that boot a provider's own Debian image instead of
// an ISO.
func handleGenerateCloudInit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)

	var cfg struct {
		AuthKey string `json:"authKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if cfg.AuthKey == "" {
		http.Error(w, "authKey is required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="tailboot-cloud-init.sh"`)
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, renderCloudInit(cfg.AuthKey))
}
