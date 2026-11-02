package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const scriptPreamble = `set -eu
set -o pipefail
umask 077

if [ "$#" -ne 1 ]; then
  echo "expected exactly one username argument" >&2
  exit 2
fi
username="$1"
if ! printf '%s' "$username" | grep -Eq '^[a-z_][a-z0-9_-]{0,31}$'; then
  echo "invalid username" >&2
  exit 2
fi
case "$username" in
  {{RESERVED_USERNAMES}})
    echo "reserved username" >&2
    exit 2
    ;;
esac
home="/var/lib/${username}"
runner_directory="${home}/actions-runner"

privileged_group() {
  group_list="$(id -nG "$username")"
  for group_name in sudo admin wheel docker; do
    for current_group in $group_list; do
      if [ "$current_group" = "$group_name" ]; then
        return 0
      fi
    done
  done
  return 1
}
`

const inspectBody = `
sanitize_token() {
  printf '%s' "$1" | tr -cd 'A-Za-z0-9_.,:-'
}

os_id=unknown
version_id=unknown
if [ -r /etc/os-release ]; then
  # shellcheck disable=SC1091
  . /etc/os-release
  os_id="$(sanitize_token "${ID:-unknown}")"
  version_id="$(sanitize_token "${VERSION_ID:-unknown}")"
fi
if [ -z "$os_id" ]; then
  os_id=unknown
fi
if [ -z "$version_id" ]; then
  version_id=unknown
fi
arch="$(sanitize_token "$(uname -m)")"
if [ -z "$arch" ]; then
  arch=unknown
fi

systemd=no
if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
  systemd=yes
fi

supported=no
if [ -f /etc/debian_version ]; then
  supported=yes
fi

privilege=none
if [ "$(id -u)" -eq 0 ]; then
  privilege=root
elif command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then
  privilege=sudo
fi

su_available=no
if command -v su >/dev/null 2>&1; then
  su_available=yes
fi

tailscale_ipv4=
if command -v tailscale >/dev/null 2>&1; then
  tailscale_candidate="$(tailscale ip -4 2>/dev/null | head -n 1 || true)"
  if printf '%s' "$tailscale_candidate" | grep -Eq '^([0-9]{1,3}\.){3}[0-9]{1,3}$'; then
    tailscale_ipv4="$tailscale_candidate"
  fi
fi

github_reachable=unchecked
if command -v curl >/dev/null 2>&1; then
  github_reachable=no
  if curl --proto '=https' --tlsv1.2 -fsS -o /dev/null --max-time 15 https://api.github.com/zen; then
    github_reachable=yes
  fi
fi

user_state=absent
user_detail=
if id "$username" >/dev/null 2>&1; then
  reasons=""
  actual_home="$(getent passwd "$username" | cut -d: -f6)"
  shell_path="$(getent passwd "$username" | cut -d: -f7)"
  if [ "$actual_home" != "$home" ]; then
    reasons="${reasons}home,"
  fi
  case "$shell_path" in
    /usr/sbin/nologin|/bin/false|/usr/bin/nologin|/sbin/nologin) ;;
    *) reasons="${reasons}shell," ;;
  esac
  if [ "$(id -u "$username")" -eq 0 ]; then
    reasons="${reasons}root,"
  fi
  if privileged_group; then
    reasons="${reasons}group,"
  fi
  reasons="${reasons%,}"
  if [ -z "$reasons" ]; then
    user_state=ready
  else
    user_state=conflict
    user_detail="$reasons"
  fi
fi

printf 'os_id=%s\n' "$os_id"
printf 'version_id=%s\n' "$version_id"
printf 'arch=%s\n' "$arch"
printf 'systemd=%s\n' "$systemd"
printf 'supported=%s\n' "$supported"
printf 'privilege=%s\n' "$privilege"
printf 'su=%s\n' "$su_available"
printf 'tailscale_ipv4=%s\n' "$tailscale_ipv4"
printf 'github_reachable=%s\n' "$github_reachable"
printf 'user_state=%s\n' "$user_state"
printf 'user_detail=%s\n' "$user_detail"
`

const applyBody = `
if [ ! -f /etc/debian_version ]; then
  echo "only Debian and Ubuntu are supported" >&2
  exit 3
fi
if [ ! -d /run/systemd/system ] || ! command -v systemctl >/dev/null 2>&1; then
  echo "systemd is required" >&2
  exit 3
fi
if [ "$(id -u)" -ne 0 ]; then
  echo "apply must run as root" >&2
  exit 3
fi
nologin_shell=""
for candidate_shell in /usr/sbin/nologin /usr/bin/nologin /sbin/nologin /bin/false; do
  if [ -x "$candidate_shell" ]; then
    nologin_shell="$candidate_shell"
    break
  fi
done
if [ -z "$nologin_shell" ]; then
  echo "nologin shell is missing" >&2
  exit 3
fi

runner_contract_ok() {
  actual_home="$(getent passwd "$username" | cut -d: -f6)"
  shell_path="$(getent passwd "$username" | cut -d: -f7)"
  if [ "$actual_home" != "$home" ]; then
    return 1
  fi
  case "$shell_path" in
    /usr/sbin/nologin|/bin/false|/usr/bin/nologin|/sbin/nologin) ;;
    *) return 1 ;;
  esac
  if [ "$(id -u "$username")" -eq 0 ]; then
    return 1
  fi
  if privileged_group; then
    return 1
  fi
  return 0
}

if id "$username" >/dev/null 2>&1; then
  if ! runner_contract_ok; then
    echo "existing user does not match the runner contract" >&2
    exit 4
  fi
else
  if [ -e "$home" ]; then
    echo "home path already exists" >&2
    exit 4
  fi
  useradd --system --create-home --home-dir "$home" --shell "$nologin_shell" --user-group "$username"
fi

install -d -m 0700 -o "$username" -g "$username" "$home"
install -d -m 0750 -o "$username" -g "$username" "$runner_directory"
chmod 0700 "$home"

if privileged_group; then
  echo "runner user is in a privileged group" >&2
  exit 4
fi
if command -v sudo >/dev/null 2>&1 && sudo -n -l -U "$username" >/dev/null 2>&1; then
  echo "runner user can invoke sudo" >&2
  exit 4
fi

export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=l
apt_get_install() {
  apt-get install -y --no-install-recommends \
    -o Dpkg::Options::=--force-confdef \
    -o Dpkg::Options::=--force-confold \
    "$@"
}
install_first_available() {
  for package_name in "$@"; do
    if apt-cache show "$package_name" >/dev/null 2>&1; then
      apt_get_install "$package_name"
      return 0
    fi
  done
  echo "none of the candidate packages are available: $*" >&2
  return 1
}

apt-get update
apt_get_install {{BASE_PACKAGES}}
install_first_available libssl3t64 libssl3 libssl1.1 libssl1.0.2 libssl1.0.0
install_first_available liblttng-ust1t64 liblttng-ust1 liblttng-ust0
libicu_package="$(apt-cache search --names-only '^libicu[0-9]+$' | awk '{print $1}' | sort -V | tail -n 1)"
if [ -z "$libicu_package" ]; then
  echo "libicu runtime package not found" >&2
  exit 1
fi
apt_get_install "$libicu_package"

if privileged_group; then
  echo "runner user is in a privileged group" >&2
  exit 4
fi
if command -v sudo >/dev/null 2>&1 && sudo -n -l -U "$username" >/dev/null 2>&1; then
  echo "runner user can invoke sudo" >&2
  exit 4
fi

marker="${home}/{{MARKER_FILE}}"
printf 'version=1\nusername=%s\nhome=%s\nrunner_directory=%s\n' "$username" "$home" "$runner_directory" > "$marker"
chown "root:${username}" "$marker"
chmod 0640 "$marker"
`

const verifyBody = `
if [ "$(id -u)" -ne 0 ]; then
  echo "verify must run as root" >&2
  exit 3
fi
if ! id "$username" >/dev/null 2>&1; then
  echo "runner user is missing" >&2
  exit 4
fi
actual_home="$(getent passwd "$username" | cut -d: -f6)"
if [ "$actual_home" != "$home" ]; then
  echo "home mismatch" >&2
  exit 4
fi

home_mode="$(stat -c %a "$home")"
home_owner="$(stat -c %U "$home")"
runner_owner="$(stat -c %U "$runner_directory")"
marker=no
if [ -f "${home}/{{MARKER_FILE}}" ]; then
  marker=yes
fi
sudo_allowed=no
if command -v sudo >/dev/null 2>&1 && sudo -n -l -U "$username" >/dev/null 2>&1; then
  sudo_allowed=yes
fi
privileged=no
if privileged_group; then
  privileged=yes
fi
shell_ok=no
case "$(getent passwd "$username" | cut -d: -f7)" in
  /usr/sbin/nologin|/bin/false|/usr/bin/nologin|/sbin/nologin)
    shell_ok=yes
    ;;
esac

github_reachable=unchecked
if command -v curl >/dev/null 2>&1; then
  github_reachable=no
  if curl --proto '=https' --tlsv1.2 -fsS -o /dev/null --max-time 15 https://api.github.com/zen; then
    github_reachable=yes
  fi
fi

printf 'home_mode=%s\n' "$home_mode"
printf 'home_owner=%s\n' "$home_owner"
printf 'runner_owner=%s\n' "$runner_owner"
printf 'marker=%s\n' "$marker"
printf 'sudo_allowed=%s\n' "$sudo_allowed"
printf 'privileged_group=%s\n' "$privileged"
printf 'shell_ok=%s\n' "$shell_ok"
printf 'github_reachable=%s\n' "$github_reachable"
`

var packageNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*$`)

const stageFilesScript = `set -eu
umask 077
state="${HOME}/.local/state/actions-runner-host"
mkdir -p "$state"
chmod 700 "$state"
prepare_path="$(mktemp "${state}/prepare.XXXXXX")"
verify_path="$(mktemp "${state}/verify.XXXXXX")"
result_path="$(mktemp "${state}/result.XXXXXX")"
printf 'prepare=%s\n' "$prepare_path"
printf 'verify=%s\n' "$verify_path"
printf 'result=%s\n' "$result_path"
`

func inspectScript() (string, error) {
	return renderScript(inspectBody)
}

func applyScript() (string, error) {
	return renderScript(applyBody)
}

func verifyScript() (string, error) {
	return renderScript(verifyBody)
}

func renderScript(body string) (string, error) {
	for _, packageName := range basePackages {
		if !packageNamePattern.MatchString(packageName) {
			return "", fmt.Errorf("invalid package name %q", packageName)
		}
	}
	alternation, err := reservedUsernameAlternation()
	if err != nil {
		return "", err
	}
	if strings.Contains(markerFileName, "/") || strings.Contains(markerFileName, "{{") {
		return "", errors.New("invalid marker file name")
	}
	script := scriptPreamble + body
	script = strings.ReplaceAll(script, "{{RESERVED_USERNAMES}}", alternation)
	script = strings.ReplaceAll(script, "{{BASE_PACKAGES}}", strings.Join(basePackages, " "))
	script = strings.ReplaceAll(script, "{{MARKER_FILE}}", markerFileName)
	if strings.Contains(script, "{{") {
		return "", errors.New("unresolved script placeholder")
	}
	return script, nil
}

func reservedUsernameAlternation() (string, error) {
	names := reservedRunnerUsernames()
	if len(names) == 0 {
		return "", errors.New("reserved username list is empty")
	}
	for _, name := range names {
		if !runnerUsernamePattern.MatchString(name) {
			return "", fmt.Errorf("reserved username %q is invalid", name)
		}
	}
	return strings.Join(names, "|"), nil
}
