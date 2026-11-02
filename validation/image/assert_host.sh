#!/bin/bash
set -eu

runner_user=github-runner
runner_home="/var/lib/${runner_user}"
runner_directory="${runner_home}/actions-runner"

operator_uid="$(id -u hostadmin)"
if [ "$operator_uid" -eq 0 ]; then
    echo "hostadmin is root" >&2
    exit 1
fi

if ! id "$runner_user" >/dev/null 2>&1; then
    echo "runner user is missing" >&2
    exit 1
fi

runner_uid="$(id -u "$runner_user")"
if [ "$runner_uid" -eq 0 ]; then
    echo "runner user is root" >&2
    exit 1
fi

actual_home="$(getent passwd "$runner_user" | cut -d: -f6)"
actual_shell="$(getent passwd "$runner_user" | cut -d: -f7)"
if [ "$actual_home" != "$runner_home" ]; then
    echo "home mismatch: ${actual_home}" >&2
    exit 1
fi
case "$actual_shell" in
    /usr/sbin/nologin|/bin/false|/usr/bin/nologin|/sbin/nologin) ;;
    *)
        echo "shell mismatch: ${actual_shell}" >&2
        exit 1
        ;;
esac

home_mode="$(stat -c %a "$runner_home")"
home_owner="$(stat -c %U "$runner_home")"
runner_mode="$(stat -c %a "$runner_directory")"
runner_owner="$(stat -c %U "$runner_directory")"
marker_path="${runner_home}/.host-prepared"
if [ ! -f "$marker_path" ]; then
    echo "marker is missing" >&2
    exit 1
fi
marker_mode="$(stat -c %a "$marker_path")"
marker_owner="$(stat -c %U:%G "$marker_path")"

if [ "$home_mode" != "700" ] || [ "$home_owner" != "$runner_user" ]; then
    echo "home ownership mismatch" >&2
    exit 1
fi
if [ "$runner_mode" != "750" ] || [ "$runner_owner" != "$runner_user" ]; then
    echo "runner directory mismatch" >&2
    exit 1
fi
if [ "$marker_mode" != "640" ] || [ "$marker_owner" != "root:${runner_user}" ]; then
    echo "marker ownership mismatch" >&2
    exit 1
fi

group_list="$(id -nG "$runner_user")"
for group_name in sudo admin wheel docker; do
    for current_group in $group_list; do
        if [ "$current_group" = "$group_name" ]; then
            echo "runner is in ${group_name}" >&2
            exit 1
        fi
    done
done

if command -v sudo >/dev/null 2>&1; then
    echo "sudo is installed" >&2
    exit 1
fi

os_release_rpm_like() {
    if [ ! -r /etc/os-release ]; then
        return 1
    fi
    family_token="$(
        # shellcheck disable=SC1091
        . /etc/os-release
        printf '%s %s' "${ID:-}" "${ID_LIKE:-}" | tr '[:upper:]' '[:lower:]'
    )"
    case " ${family_token} " in
        *" amzn "*|*" amazon "*|*" fedora "*|*" rhel "*|*" centos "*|*" rocky "*|*" almalinux "*|*" ol "*|*" oracle "*)
            return 0
            ;;
    esac
    return 1
}

if [ -f /etc/debian_version ]; then
    for package_name in ca-certificates curl tar git libkrb5-3 zlib1g; do
        package_status="$(dpkg-query -W -f '${Status}' "$package_name")"
        if [ "$package_status" != "install ok installed" ]; then
            echo "package missing: ${package_name}" >&2
            exit 1
        fi
    done
    zlib_package=zlib1g
    installed_package() {
        package_name="$1"
        package_status="$(dpkg-query -W -f '${Status}' "$package_name" 2>/dev/null || true)"
        [ "$package_status" = "install ok installed" ]
    }
    libssl_package=""
    for candidate_name in libssl3t64 libssl3 libssl1.1 libssl1.0.2 libssl1.0.0; do
        if installed_package "$candidate_name"; then
            libssl_package="$candidate_name"
            break
        fi
    done
    liblttng_package=""
    for candidate_name in liblttng-ust1t64 liblttng-ust1 liblttng-ust0; do
        if installed_package "$candidate_name"; then
            liblttng_package="$candidate_name"
            break
        fi
    done
    libicu_package="$(dpkg-query -W -f '${Package} ${Status}\n' 'libicu[0-9]*' | awk '$2 == "install" && $3 == "ok" && $4 == "installed" { print $1; exit }')"
elif [ -f /etc/redhat-release ] || os_release_rpm_like; then
    for package_name in ca-certificates tar git krb5-libs openssl-libs lttng-ust libicu; do
        if ! rpm -q "$package_name" >/dev/null 2>&1; then
            echo "package missing: ${package_name}" >&2
            exit 1
        fi
    done
    zlib_package="$(rpm -q --qf '%{NAME}\n' --whatprovides zlib 2>/dev/null | head -n 1 || true)"
    if [ -z "$zlib_package" ]; then
        echo "package missing: zlib" >&2
        exit 1
    fi
    if ! rpm -q curl >/dev/null 2>&1 && ! rpm -q curl-minimal >/dev/null 2>&1; then
        echo "package missing: curl" >&2
        exit 1
    fi
    libssl_package=openssl-libs
    liblttng_package=lttng-ust
    libicu_package=libicu
elif [ -r /etc/os-release ]; then
    family_token="$(
        # shellcheck disable=SC1091
        . /etc/os-release
        printf '%s %s' "${ID:-}" "${ID_LIKE:-}" | tr '[:upper:]' '[:lower:]'
    )"
    case " ${family_token} " in
        *" suse "*) ;;
        *)
            echo "unsupported package family" >&2
            exit 1
            ;;
    esac
    for package_name in ca-certificates curl tar git krb5; do
        if ! rpm -q "$package_name" >/dev/null 2>&1; then
            echo "package missing: ${package_name}" >&2
            exit 1
        fi
    done
    libssl_package=""
    for candidate_name in libopenssl3 libopenssl1_1 libopenssl1_0_0; do
        if rpm -q "$candidate_name" >/dev/null 2>&1; then
            libssl_package="$candidate_name"
            break
        fi
    done
    zlib_package=""
    for candidate_name in libz1 zlib; do
        if rpm -q "$candidate_name" >/dev/null 2>&1; then
            zlib_package="$candidate_name"
            break
        fi
    done
    libicu_package="$(rpm -qa --qf '%{NAME}\n' 'libicu[0-9]*' | while IFS= read -r candidate_name; do
        case "$candidate_name" in
            libicu[0-9]|libicu[0-9][0-9]|libicu[0-9][0-9][0-9]|libicu[0-9]_[0-9]|libicu[0-9][0-9]_[0-9]|libicu[0-9][0-9][0-9]_[0-9])
                printf '%s\n' "$candidate_name"
                ;;
        esac
    done | sort -V | tail -n 1)"
    liblttng_package=""
    for candidate_name in lttng-ust liblttng-ust1 liblttng-ust0; do
        if rpm -q "$candidate_name" >/dev/null 2>&1; then
            liblttng_package="$candidate_name"
            break
        fi
    done
    if [ -z "$zlib_package" ]; then
        echo "zlib package missing" >&2
        exit 1
    fi
else
    echo "unsupported package family" >&2
    exit 1
fi
if [ -z "$libssl_package" ] || [ -z "$libicu_package" ] || [ -z "$liblttng_package" ]; then
    echo "runtime library package missing" >&2
    exit 1
fi

registration_count="$(find "$runner_directory" -mindepth 1 | wc -l | tr -d ' ')"
if [ "$registration_count" != "0" ]; then
    echo "runner directory is not empty" >&2
    exit 1
fi

staged_count=0
state_directory=/home/hostadmin/.local/state/actions-runner-host
if [ -d "$state_directory" ]; then
    staged_count="$(find "$state_directory" -type f | wc -l | tr -d ' ')"
fi
if [ "$staged_count" != "0" ]; then
    echo "staged su files remain" >&2
    exit 1
fi

if [ ! -d /run/systemd/system ]; then
    echo "systemd runtime directory is missing" >&2
    exit 1
fi
read -r pid1_name < /proc/1/comm
case "$pid1_name" in
    systemd|init) ;;
    *)
        echo "pid 1 is ${pid1_name}" >&2
        exit 1
        ;;
esac
if ! command -v systemctl >/dev/null 2>&1; then
    echo "systemctl is missing" >&2
    exit 1
fi

systemd_state="$(systemctl is-system-running || true)"
case "$systemd_state" in
    running|degraded) ;;
    *)
        echo "systemd state is ${systemd_state}" >&2
        exit 1
        ;;
esac

printf 'operator_uid=%s\n' "$operator_uid"
printf 'runner_uid=%s\n' "$runner_uid"
printf 'shell=%s\n' "$actual_shell"
printf 'home_mode=%s\n' "$home_mode"
printf 'runner_mode=%s\n' "$runner_mode"
printf 'marker_mode=%s\n' "$marker_mode"
printf 'groups=%s\n' "$group_list"
printf 'zlib=%s\n' "${zlib_package:-}"
printf 'libssl=%s\n' "$libssl_package"
printf 'libicu=%s\n' "$libicu_package"
printf 'liblttng=%s\n' "$liblttng_package"
printf 'registration_entries=%s\n' "$registration_count"
printf 'staged_files=%s\n' "$staged_count"
printf 'pid1=%s\n' "$pid1_name"
printf 'systemd_state=%s\n' "$systemd_state"
github_reachable=no
if curl --proto '=https' --tlsv1.2 -fsS -o /dev/null --max-time 15 https://api.github.com/zen; then
    github_reachable=yes
fi
printf 'github_reachable=%s\n' "$github_reachable"
