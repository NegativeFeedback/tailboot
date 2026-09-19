#!/bin/sh

set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "Run this script as root (for example: sudo ./image/scripts/build-iso.sh)." >&2
  exit 1
fi

output_name=${1:-tailboot-amd64.iso}
case "${output_name}" in
  *.iso) ;;
  *)
    echo "The output name must be an .iso file name without directories." >&2
    exit 1
    ;;
esac
case "${output_name}" in
  */*)
    echo "The output name must not contain directories." >&2
    exit 1
    ;;
esac

# "full" (default) keeps all wireless chipset firmware; "no-wifi" strips it
# entirely for Ethernet-only deployments. See strip-wifi-firmware.hook.chroot.
variant=${2:-full}
case "${variant}" in
  full|no-wifi) ;;
  *)
    echo "The variant must be 'full' or 'no-wifi'." >&2
    exit 1
    ;;
esac

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
image_dir=$(dirname -- "${script_dir}")

cd "${image_dir}"

# Materialize (or remove) the wifi-stripping hook before each build. It lives
# outside config/hooks/live/ normally so the default build never runs it;
# config/hooks/live/* other than the tracked hooks is git-ignored, so adding
# it here for a no-wifi build leaves the working tree clean either way.
wifi_hook="config/hooks/live/0950-strip-wifi-firmware.hook.chroot"
rm -f "${wifi_hook}"
if [ "${variant}" = "no-wifi" ]; then
  install -m 0755 "${script_dir}/strip-wifi-firmware.hook.chroot" "${wifi_hook}"
fi

lb clean --purge

# live-build consumes this key while installing the package from Tailscale's
# official APT repository. It is fetched fresh so key rotation does not require
# a source change.
curl --fail --silent --show-error --location \
  https://pkgs.tailscale.com/stable/debian/trixie.asc \
  --output config/archives/tailscale.key.chroot

lb config
lb build

mkdir -p "${image_dir}/dist"
install -m 0644 live-image-amd64.hybrid.iso \
  "${image_dir}/dist/${output_name}"

rm -f "${wifi_hook}"
