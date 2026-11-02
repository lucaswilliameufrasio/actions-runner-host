#!/bin/bash
set -eu

script_directory="$(cd "$(dirname "$0")" && pwd)"
project_directory="$(cd "${script_directory}/../.." && pwd)"

if [ -z "${BASE_IMAGE:-}" ]; then
    echo "BASE_IMAGE is required, for example debian:13" >&2
    exit 1
fi
if ! printf '%s' "$BASE_IMAGE" | grep -Eq '^[a-z0-9][a-z0-9._:-]*$'; then
    echo "BASE_IMAGE must be a plain image reference" >&2
    exit 1
fi

image_slug="$(printf '%s' "$BASE_IMAGE" | tr ':/' '--')"
image_name="actions-runner-host-validation:${image_slug}"
container_name="actions-runner-host-validation-${image_slug}"
runtime_directory="$(mktemp -d "${TMPDIR:-/tmp}/actions-runner-host-validation.XXXXXX")"
chmod 700 "$runtime_directory"

ssh_config_path="${HOME}/.ssh/config"
ssh_config_existed=no
ssh_config_stamp=""
if [ -e "$ssh_config_path" ]; then
    ssh_config_existed=yes
    ssh_config_stamp="$(stat -f %m "$ssh_config_path")"
fi

cleanup() {
    docker rm -f "$container_name" >/dev/null 2>&1 || true
    rm -rf "$runtime_directory"
}
trap cleanup EXIT

if ! command -v docker >/dev/null 2>&1; then
    echo "docker CLI is missing" >&2
    exit 1
fi
if ! command -v python3 >/dev/null 2>&1; then
    echo "python3 is missing" >&2
    exit 1
fi
if ! command -v ssh-keygen >/dev/null 2>&1 || ! [ -x /usr/bin/ssh ]; then
    echo "OpenSSH client is missing" >&2
    exit 1
fi

go_binary="$(command -v go || true)"
if [ -z "$go_binary" ] && [ -x "${HOME}/.local/share/mise/installs/go/1.27.3/bin/go" ]; then
    go_binary="${HOME}/.local/share/mise/installs/go/1.27.3/bin/go"
fi
if [ -z "$go_binary" ]; then
    echo "go is missing" >&2
    exit 1
fi

python3 - "$runtime_directory" <<'PY'
import pathlib
import secrets
import sys

runtime = pathlib.Path(sys.argv[1])
password = secrets.token_urlsafe(24)
password_path = runtime / "root-password"
password_path.write_text(password + "\n", encoding="utf-8")
password_path.chmod(0o600)
PY

ssh-keygen -q -t ed25519 -f "${runtime_directory}/id_ed25519" -N "" -C "actions-runner-host-validation"
chmod 600 "${runtime_directory}/id_ed25519"

echo "Building ${image_name} from ${BASE_IMAGE}"
docker build --build-arg "BASE_IMAGE=${BASE_IMAGE}" -t "$image_name" "$script_directory"

start_container() {
    profile_name="$1"
    shift
    docker rm -f "$container_name" >/dev/null 2>&1 || true
    echo "Starting ${BASE_IMAGE} with profile ${profile_name}"
    docker run -d --name "$container_name" \
        -p 127.0.0.1::22 \
        -v "${runtime_directory}/root-password:/run/validation/root-password:ro" \
        -v "${runtime_directory}/id_ed25519.pub:/run/validation/authorized-keys:ro" \
        "$@" \
        "$image_name" >/dev/null
}

container_is_running() {
    state="$(docker inspect -f '{{.State.Running}}' "$container_name" 2>/dev/null || true)"
    [ "$state" = "true" ]
}

wait_for_systemd_ssh() {
    attempt=0
    while [ "$attempt" -lt 40 ]; do
        if container_is_running && docker exec "$container_name" /bin/sh -c 'test -d /run/systemd/system && { pgrep sshd >/dev/null || systemctl is-active --quiet ssh.socket || systemctl is-active --quiet ssh.service; }'; then
            return 0
        fi
        if ! container_is_running; then
            return 1
        fi
        attempt=$((attempt + 1))
        sleep 2
    done
    return 1
}

systemd_ready=no
if start_container "privileged-cgroup-host" --privileged --cgroupns=host -v /sys/fs/cgroup:/sys/fs/cgroup && wait_for_systemd_ssh; then
    systemd_ready=yes
fi
report_boot_failure() {
    docker inspect -f 'status={{.State.Status}} exit={{.State.ExitCode}}' "$container_name" >&2 || true
    if container_is_running; then
        docker exec "$container_name" /bin/sh -c 'ps -p 1 -o comm=; if [ -d /run/systemd/system ]; then echo systemd_dir=yes; else echo systemd_dir=no; fi; systemctl is-system-running || true; systemctl is-active ssh.service ssh.socket || true' >&2 || true
    fi
    docker logs "$container_name" 2>&1 | tail -n 40 >&2 || true
}

if [ "$systemd_ready" != "yes" ]; then
    echo "Profile privileged-cgroup-host did not reach systemd on ${BASE_IMAGE}; trying privileged only" >&2
    report_boot_failure
    if start_container "privileged" --privileged && wait_for_systemd_ssh; then
        systemd_ready=yes
    fi
fi
if [ "$systemd_ready" != "yes" ]; then
    echo "${BASE_IMAGE} did not reach a real systemd pid 1. Prepare was not applied." >&2
    report_boot_failure
    exit 1
fi

published_port="$(docker port "$container_name" 22 | awk -F: 'NR == 1 { print $NF }')"
if ! printf '%s' "$published_port" | grep -Eq '^[0-9]+$'; then
    echo "could not read the published ssh port" >&2
    exit 1
fi

ssh-keyscan -p "$published_port" -T 5 127.0.0.1 > "${runtime_directory}/known_hosts" 2>/dev/null
if ! grep -q . "${runtime_directory}/known_hosts"; then
    echo "ssh-keyscan returned no host key" >&2
    exit 1
fi

cat > "${runtime_directory}/ssh_config" <<EOF
Host 127.0.0.1
  HostName 127.0.0.1
  Port ${published_port}
  User hostadmin
  IdentityFile ${runtime_directory}/id_ed25519
  IdentitiesOnly yes
  StrictHostKeyChecking yes
  UserKnownHostsFile ${runtime_directory}/known_hosts
  GlobalKnownHostsFile /dev/null
EOF

cat > "${runtime_directory}/ssh" <<EOF
#!/bin/bash
exec /usr/bin/ssh -F ${runtime_directory}/ssh_config "\$@"
EOF
chmod 755 "${runtime_directory}/ssh"

export PATH="${runtime_directory}:${PATH}"
preflight_output="$("${runtime_directory}/ssh" -n 127.0.0.1 'id -u; id -un; command -v su; command -v sudo || true; test -d /run/systemd/system && echo systemd_dir=yes' </dev/null)"
printf '%s\n' "$preflight_output"
operator_uid="$(printf '%s\n' "$preflight_output" | awk 'NR == 1 { print }')"
operator_name="$(printf '%s\n' "$preflight_output" | awk 'NR == 2 { print }')"
if [ "$operator_uid" = "0" ] || [ "$operator_name" != "hostadmin" ]; then
    echo "SSH session is not the unprivileged hostadmin user" >&2
    exit 1
fi
if ! printf '%s\n' "$preflight_output" | grep -q '/su$'; then
    echo "su is not available to the SSH user" >&2
    exit 1
fi
if printf '%s\n' "$preflight_output" | grep -q '/sudo$'; then
    echo "sudo is installed; the su path would not be exercised" >&2
    exit 1
fi
if ! printf '%s\n' "$preflight_output" | grep -qx 'systemd_dir=yes'; then
    echo "inspection would not see systemd" >&2
    exit 1
fi

export VALIDATION_PROJECT="$project_directory"
export VALIDATION_GO="$go_binary"
export VALIDATION_PASSWORD_FILE="${runtime_directory}/root-password"
export VALIDATION_TRANSCRIPT="${runtime_directory}/transcript.txt"

echo "Running prepare for a new runner user on ${BASE_IMAGE}"
VALIDATION_EXPECT_USER=absent python3 "${script_directory}/drive_prepare.py"
docker exec -i "$container_name" bash -s < "${script_directory}/assert_host.sh"

echo "Running prepare again for the same runner user on ${BASE_IMAGE}"
VALIDATION_EXPECT_USER=ready python3 "${script_directory}/drive_prepare.py"
docker exec -i "$container_name" bash -s < "${script_directory}/assert_host.sh"

if [ "$ssh_config_existed" = "yes" ]; then
    if [ "$(stat -f %m "$ssh_config_path")" != "$ssh_config_stamp" ]; then
        echo "user ssh config changed" >&2
        exit 1
    fi
elif [ -e "$ssh_config_path" ]; then
    echo "user ssh config was created" >&2
    exit 1
fi

echo "Validation passed for ${BASE_IMAGE} on 127.0.0.1 port ${published_port}"
