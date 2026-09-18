<h1 align="center">
  <img src="assets/logo.svg" alt="Tailboot" width="420">
</h1>

<p align="center">
  <strong>Use Tailboot to start Debian and connect to a computer with Tailscale SSH.</strong>
</p>

Tailboot is a Debian 13 live image. It connects the computer to your tailnet.
It then starts Tailscale SSH.

You do not have to install an operating system. Tailboot runs from a USB drive.

Tailboot does not install Debian on the computer. To use the usual operating
system, first shut down Tailboot. Remove the USB drive. Then, start the computer.

## What Tailboot does

You can use Tailboot to:

- Prepare a new server before you install an operating system.
- Use Debian for a short time. Tailboot does not replace the installed operating
  system.
- Make an SSH connection without an open port on the public internet.

Tailboot contains only the software that is necessary to do these tasks:

- Start the computer.
- Connect to a network.
- Join Tailscale.
- Accept SSH connections.

After you connect, use APT to install other software.

## Prepare to use Tailboot

Make sure that you have these items:

- A Tailscale account
- Docker, to run the Tailboot image generator
- A USB drive
- A computer with an x86-64 processor that can start from a USB drive

### 1. Create a Tailscale auth key

In the Tailscale admin console, open
[Settings > Keys](https://console.tailscale.com/admin/settings/keys). Select
**Generate auth key**. Use these settings:

| Setting | Value |
| --- | --- |
| Reusable | On |
| Expiration | 90 days |
| Ephemeral | On |
| Pre-approved | On, if available |
| Tags | An isolated tag, such as `tag:isolated` |

Use an isolated tag. Configure your
[tailnet policy](https://console.tailscale.com/admin/acls). Do not let Tailboot
machines connect to other devices. Make sure that your devices can connect to
the Tailboot machines with Tailscale SSH.

A tag does not limit access by itself. Make sure that broad allow rules do not
apply to the tag.

### 2. Create the ISO

Tailboot ships as a self-hosted Docker image. It bundles a pre-built base ISO
and serves both a web form and a JSON API for customizing it -- there is no
public website or third-party service involved.

```sh
docker run -p 8080:8080 ghcr.io/negativefeedback/tailboot:latest
```

Then either:

- Open <http://localhost:8080> in a browser, enter the auth key and any
  optional Wi-Fi or static IP settings, and select **Create ISO**.
- Or call the API directly:

  ```sh
  curl -X POST http://localhost:8080/api/iso \
    -H 'Content-Type: application/json' \
    -d '{
      "authKey": "tskey-auth-...",
      "wifi": { "ssid": "your-network", "password": "your-password" },
      "staticIp": { "address": "192.168.1.50/24", "gateway": "192.168.1.1", "dns": ["1.1.1.1"] }
    }' \
    -o tailboot.iso
  ```

  `wifi` and `staticIp` are both optional. `staticIp.dns` is optional within
  `staticIp`. The static IP applies to whichever Ethernet adapter the machine
  presents, regardless of its interface name.

Your customized ISO is generated and downloaded from your own container. Your
credentials are never sent to a third party.

### 3. Write the ISO to a USB drive

1. Use [Etcher](https://etcher.balena.io/), `dd`, or an equivalent ISO tool.
2. Write the customized ISO to a USB drive.
3. Connect the USB drive to the target computer.
4. Start the computer from the USB drive.

Tailboot supports BIOS and UEFI systems.

Tailboot uses Ethernet as the primary connection. If Ethernet has no default
route, Tailboot uses the Wi-Fi network that you added to the ISO.

### 4. Connect with SSH

The computer has the name `tailboot` in your tailnet. If that name is in use,
Tailscale adds a number. For example, the name can be `tailboot-1`.

Use these commands:

```sh
ssh tailboot@tailboot
sudo -i
```

Tailscale authenticates the SSH session. You do not need an SSH password. Your
tailnet policy must permit traffic on port 22. It must also include a Tailscale
SSH rule for the `tailboot` user.

The `autogroup:nonroot` group includes the `tailboot` user. It does not include
the `root` user. Refer to the
[Tailscale SSH documentation](https://tailscale.com/kb/1193/tailscale-ssh) for
policy examples.

## Security

- The customized ISO contains the Tailscale auth key as plain text. It also
  contains the Wi-Fi password as plain text if you supply one.
- Keep the customized ISO and the USB drive in a secure location. Do not give
  them to other persons.
- Your own Docker container writes the credentials to the ISO. They are never
  sent to a third party -- run the image on infrastructure you control.
- Each time Tailboot starts, it creates a new ephemeral Tailscale machine
  identity. Tailboot does not keep or restore Tailscale state.
- The auth key expires after 90 days. When the key expires, create a new key and
  a new ISO.
- Each [GitHub release](https://github.com/NegativeFeedback/tailboot/releases)
  contains the base ISO and its SHA-256 checksum. The checksum applies only to
  the base ISO. It does not apply to a customized ISO.

## Product limits

Tailboot connects a computer to Tailscale and gives you a shell. It is not a
general rescue system, an operating system installer, or a persistent
workstation.

- The computer must have an internet connection to join Tailscale.
- Tailboot supports one WPA2/WPA3 Personal Wi-Fi network.
- A restart removes changes to the live system.
- The Debian kernel and firmware control hardware support.
- Tailboot automatically logs in to the local console as `tailboot`. If a login
  screen appears, use `tailboot` as the user name and `live` as the password.
  This account can use `sudo` without a password.

## Development

Tailboot uses the [MIT License](LICENSE).

Use these commands to test the shared ISO-patching logic:

```sh
pnpm install
pnpm test
```

Use a Debian 13 computer to build the ISO. Install `live-build` and `curl`.
Then, run this command:

```sh
sudo ./image/scripts/build-iso.sh tailboot-local-amd64.iso
```

The script writes the ISO to `image/dist/`.

### Server (`server/`)

The Docker image's web UI and API live in `server/`, a standalone Go module
(no dependency on the pnpm workspace):

```sh
cd server
go test ./...
# Needs a base ISO at /data/base.iso; the offset is baked in at build time,
# same as ISO_NAME/RELEASE_TAG (see the Dockerfile).
go run -ldflags "-X main.configOffsetStr=<offset from image/scripts/config-offset.sh>" .
```

### Docker image

```sh
docker build \
  --build-arg ISO_NAME=tailboot-local-amd64.iso \
  --build-arg RELEASE_TAG=dev \
  --build-arg CONFIG_OFFSET=<offset from image/scripts/config-offset.sh> \
  -t tailboot .
```

The build expects a `base.iso` file at the repository root (the output of
`build-iso.sh`, renamed). In CI, the `build-docker` job in
`.github/workflows/release.yml` stages this automatically from the same
`release-iso` build and pushes to `ghcr.io/negativefeedback/tailboot`.
