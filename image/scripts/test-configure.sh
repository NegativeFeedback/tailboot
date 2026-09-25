#!/bin/sh

# Run only inside the disposable build chroot, after the ISO has been built:
# sudo chroot image/chroot /bin/sh < image/scripts/test-configure.sh
set -eux

config=/run/live/medium/TAILBOOT.JSON
key=/run/tailboot/auth.key
profile=/run/NetworkManager/system-connections/tailboot-wifi.nmconnection
static_profile=/run/NetworkManager/system-connections/tailboot-static-ip.nmconnection
work_dir=$(mktemp -d)
trap 'rm -f "${config}" "${key}" "${profile}" "${static_profile}" /run/tailboot/wifi.nmconnection /run/tailboot/static-ip.nmconnection /etc/hostname /root/scripts/engagement.txt /root/scripts/*.csv; rm -rf "${work_dir}"' EXIT HUP INT TERM
mkdir -p /run/live/medium /run/tailboot

printf '%s\n' '{"authKey":"tskey-auth-test"}' > "${config}"
/usr/local/sbin/tailboot-configure
test "$(cat "${key}")" = tskey-auth-test
test "$(stat -c %a "${key}")" = 600
test ! -e "${profile}"
test ! -e "${static_profile}"

cat > "${config}" <<'JSON'
{"authKey":"tskey-auth-test","wifi":{"ssid":"Café \"网络\"","password":"quotes\"and\\backslash"}}
JSON
/usr/local/sbin/tailboot-configure
test "$(stat -c %a "${profile}")" = 600
# nmcli serializes Unicode SSIDs as their UTF-8 bytes and escapes backslashes.
grep -Fxq 'ssid=67;97;102;195;169;32;34;231;189;145;231;187;156;34;' "${profile}"
grep -Fxq 'psk=quotes"and\\backslash' "${profile}"
grep -Fxq 'key-mgmt=wpa-psk' "${profile}"
test "$(grep -c '^method=auto$' "${profile}")" = 2
# Re-import through NetworkManager's parser to check the complete profile.
nmcli --offline connection modify connection.id tailboot-wifi < "${profile}" > /dev/null
/usr/local/sbin/tailboot-configure
test "$(find /run/NetworkManager/system-connections -name 'tailboot-wifi*' | wc -l)" -eq 1
rm "${profile}"

cp "${config}" "${work_dir}/valid-wifi.json"

# Static IP, no Wi-Fi.
printf '%s\n' \
  '{"authKey":"tskey-auth-test","staticIp":{"address":"192.168.1.50/24","gateway":"192.168.1.1","dns":["1.1.1.1","8.8.8.8"]}}' \
  > "${config}"
/usr/local/sbin/tailboot-configure
test "$(stat -c %a "${static_profile}")" = 600
grep -Fq 'method=manual' "${static_profile}"
grep -Fq '192.168.1.50/24' "${static_profile}"
grep -Fq '192.168.1.1' "${static_profile}"
grep -Fq '1.1.1.1' "${static_profile}"
grep -Fq '8.8.8.8' "${static_profile}"
# Re-import through NetworkManager's parser to check the complete profile.
nmcli --offline connection modify connection.id tailboot-static-ip < "${static_profile}" > /dev/null
/usr/local/sbin/tailboot-configure
test "$(find /run/NetworkManager/system-connections -name 'tailboot-static-ip*' | wc -l)" -eq 1
cp "${config}" "${work_dir}/valid-static-ip.json"

# Static IP without DNS is optional.
rm "${static_profile}"
printf '%s\n' \
  '{"authKey":"tskey-auth-test","staticIp":{"address":"192.168.1.50/24","gateway":"192.168.1.1"}}' \
  > "${config}"
/usr/local/sbin/tailboot-configure
test -e "${static_profile}"
rm "${static_profile}"

# Invalid static IP settings must not fail the required auth-key service.
printf '%s\n' \
  '{"authKey":"tskey-auth-test","staticIp":{"address":"not-an-address","gateway":"192.168.1.1"}}' \
  > "${config}"
/usr/local/sbin/tailboot-configure 2> "${work_dir}/error"
grep -Fq 'continuing with DHCP' "${work_dir}/error"
test "$(cat "${key}")" = tskey-auth-test
test ! -e "${static_profile}"

# Inject a failed and a stuck profile writer without requiring real hardware.
cp "${work_dir}/valid-static-ip.json" "${config}"
mkdir -p "${work_dir}/bin"
cat > "${work_dir}/bin/nmcli" <<'SH'
#!/bin/sh
echo '[partial profile]'
exit 1
SH
chmod 755 "${work_dir}/bin/nmcli"
PATH="${work_dir}/bin:${PATH}" /usr/local/sbin/tailboot-configure 2> "${work_dir}/error"
grep -Fq 'continuing with DHCP' "${work_dir}/error"
test "$(cat "${key}")" = tskey-auth-test
test ! -e "${static_profile}"

cat > "${work_dir}/bin/nmcli" <<'SH'
#!/bin/sh
trap '' TERM
sleep 30
SH
PATH="${work_dir}/bin:${PATH}" /usr/local/sbin/tailboot-configure 2> "${work_dir}/error"
grep -Fq 'continuing with DHCP' "${work_dir}/error"
test "$(cat "${key}")" = tskey-auth-test
test ! -e "${static_profile}"

hostname_file=/etc/hostname
engagement=/root/scripts/engagement.txt
internal_csv=/root/scripts/internal.csv
cde_csv=/root/scripts/cde.csv
rm -f "${hostname_file}" "${engagement}" /root/scripts/*.csv

# Engagement metadata: hostname, engagement.txt, and per-name scope CSVs.
# Two "Internal"/"internal" entries must merge into one file; "CDE" gets its
# own, arbitrarily-named file -- there's no fixed internal/external split.
printf '%s\n' \
  '{"authKey":"tskey-auth-test","hostname":"dropbox-acme-corp","clientName":"Acme Corp","date":"2026-09-24","killDate":"2026-10-24","scope":[{"name":"Internal","value":"10.0.0.0/8"},{"name":"internal","value":"172.16.0.0/12"},{"name":"CDE","value":"203.0.113.0/24"}]}' \
  > "${config}"
/usr/local/sbin/tailboot-configure
test "$(cat "${hostname_file}")" = dropbox-acme-corp
grep -Fxq 'Client: Acme Corp' "${engagement}"
grep -Fxq 'Start date: 2026-09-24' "${engagement}"
grep -Fxq 'Kill date: 2026-10-24' "${engagement}"
test "$(cat "${internal_csv}")" = "$(printf '10.0.0.0/8\n172.16.0.0/12')"
test "$(cat "${cde_csv}")" = "203.0.113.0/24"

# Absent engagement fields write nothing.
rm -f "${hostname_file}" "${engagement}" /root/scripts/*.csv
printf '%s\n' '{"authKey":"tskey-auth-test"}' > "${config}"
/usr/local/sbin/tailboot-configure
test ! -e "${hostname_file}"
test ! -e "${engagement}"
test ! -e "${internal_csv}"

# A malformed scope (not an array) must not fail the required auth-key
# service, and must not prevent hostname/engagement.txt from being written.
printf '%s\n' \
  '{"authKey":"tskey-auth-test","clientName":"Acme Corp","hostname":"dropbox-acme-corp","scope":"not-an-array"}' \
  > "${config}"
/usr/local/sbin/tailboot-configure 2> "${work_dir}/error"
grep -Fq 'scope CSV generation failed' "${work_dir}/error"
test "$(cat "${key}")" = tskey-auth-test
test "$(cat "${hostname_file}")" = dropbox-acme-corp
grep -Fxq 'Client: Acme Corp' "${engagement}"
test ! -e "${internal_csv}"
rm -f "${hostname_file}" "${engagement}"

cp "${work_dir}/valid-wifi.json" "${config}"

# Invalid Wi-Fi settings must not fail the required auth-key service.
jq '.wifi.ssid = ("x" * 33)' "${config}" > "${work_dir}/invalid-wifi.json"
cp "${config}" "${work_dir}/valid.json"
cp "${work_dir}/invalid-wifi.json" "${config}"
/usr/local/sbin/tailboot-configure 2> "${work_dir}/error"
grep -Fq 'continuing with Ethernet available' "${work_dir}/error"
test "$(cat "${key}")" = tskey-auth-test
test ! -e "${profile}"

# Inject a failed and a stuck profile writer without requiring Wi-Fi hardware.
cp "${work_dir}/valid.json" "${config}"
mkdir -p "${work_dir}/bin"
cat > "${work_dir}/bin/nmcli" <<'SH'
#!/bin/sh
echo '[partial profile]'
exit 1
SH
chmod 755 "${work_dir}/bin/nmcli"
PATH="${work_dir}/bin:${PATH}" /usr/local/sbin/tailboot-configure 2> "${work_dir}/error"
grep -Fq 'continuing with Ethernet available' "${work_dir}/error"
test "$(cat "${key}")" = tskey-auth-test
test ! -e "${profile}"

cat > "${work_dir}/bin/nmcli" <<'SH'
#!/bin/sh
trap '' TERM
sleep 30
SH
PATH="${work_dir}/bin:${PATH}" /usr/local/sbin/tailboot-configure 2> "${work_dir}/error"
grep -Fq 'continuing with Ethernet available' "${work_dir}/error"
test "$(cat "${key}")" = tskey-auth-test
test ! -e "${profile}"

printf '%s\n' 'invalid JSON' > "${config}"
if /usr/local/sbin/tailboot-configure 2>/dev/null; then
  echo 'Invalid JSON unexpectedly succeeded.' >&2
  exit 1
fi

echo 'Verified auth-key extraction, Wi-Fi/static IP profiles, engagement metadata, permissions, and continuation after errors and timeouts.'
