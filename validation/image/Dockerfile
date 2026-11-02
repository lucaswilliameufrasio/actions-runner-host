ARG BASE_IMAGE=debian:13
FROM ${BASE_IMAGE}

ENV container=docker

# systemd creates /run/systemd/system when it becomes pid 1.
# Do not create that directory in the image.
RUN set -eu; \
    if command -v apt-get >/dev/null 2>&1; then \
        export DEBIAN_FRONTEND=noninteractive; \
        apt-get update; \
        apt-get install -y --no-install-recommends \
            systemd \
            systemd-sysv \
            dbus \
            openssh-server \
            ca-certificates \
            passwd \
            login \
            procps \
            findutils; \
        apt-get clean; \
        rm -rf /var/lib/apt/lists/*; \
    elif command -v dnf >/dev/null 2>&1; then \
        dnf install -y --setopt=install_weak_deps=False \
            systemd \
            dbus \
            openssh-server \
            ca-certificates \
            passwd \
            shadow-utils \
            procps-ng \
            util-linux \
            findutils; \
        dnf clean all; \
    elif command -v yum >/dev/null 2>&1; then \
        yum install -y \
            systemd \
            dbus \
            openssh-server \
            ca-certificates \
            passwd \
            shadow-utils \
            procps-ng \
            util-linux \
            findutils; \
        yum clean all; \
    elif command -v zypper >/dev/null 2>&1; then \
        zypper --non-interactive --gpg-auto-import-keys refresh; \
        zypper --non-interactive --gpg-auto-import-keys install --no-recommends \
            systemd \
            dbus-1 \
            openssh \
            ca-certificates \
            shadow \
            procps \
            util-linux \
            findutils; \
        if zypper --non-interactive info --type package openssh-server >/dev/null 2>&1; then \
            zypper --non-interactive --gpg-auto-import-keys install --no-recommends openssh-server; \
        fi; \
        for sysv_package in systemd-sysvinit systemd-sysvcompat; do \
            if zypper --non-interactive info --type package "$$sysv_package" >/dev/null 2>&1; then \
                zypper --non-interactive --gpg-auto-import-keys install --no-recommends "$$sysv_package"; \
                break; \
            fi; \
        done; \
        zypper clean --all; \
    else \
        echo "no supported package manager in the base image" >&2; \
        exit 1; \
    fi

# The suite logs in as a normal user and becomes root with su -.
# Fedora ships sudo as a protected package, which would hide that path.
RUN set -eu; \
    if command -v dnf >/dev/null 2>&1; then \
        if rpm -q sudo >/dev/null 2>&1; then \
            dnf remove -y --setopt=protected_packages= sudo; \
        fi; \
    elif command -v yum >/dev/null 2>&1; then \
        if rpm -q sudo >/dev/null 2>&1; then \
            yum remove -y sudo; \
        fi; \
    elif command -v zypper >/dev/null 2>&1; then \
        if rpm -q sudo >/dev/null 2>&1; then \
            zypper --non-interactive remove sudo; \
        fi; \
    elif command -v apt-get >/dev/null 2>&1; then \
        if dpkg-query -W -f '${Status}' sudo 2>/dev/null | grep -q 'install ok installed'; then \
            export DEBIAN_FRONTEND=noninteractive; \
            apt-get remove -y sudo; \
        fi; \
    fi

RUN set -eu; \
    if [ ! -s /etc/machine-id ]; then \
        tr -d '-' < /proc/sys/kernel/random/uuid > /etc/machine-id; \
    fi; \
    mkdir -p /var/lib/dbus /etc/systemd/system; \
    if [ -f /etc/machine-id ]; then \
        ln -sf /etc/machine-id /var/lib/dbus/machine-id; \
    fi; \
    for unit_root in /usr/lib/systemd/system /lib/systemd/system; do \
        if [ -d "$unit_root" ]; then \
            find "$unit_root" /etc/systemd/system -path '*.wants/*' \
                \( -name '*udev*' -o -name '*getty*' -o -name '*systemd-networkd*' \
                   -o -name '*systemd-resolved*' -o -name '*systemd-timesyncd*' \
                   -o -name '*systemd-update-utmp*' \) -delete; \
        fi; \
    done; \
    for unit_name in systemd-udevd.service systemd-udevd-kernel.socket systemd-udevd-control.socket console-getty.service getty@tty1.service systemd-networkd.service systemd-resolved.service; do \
        ln -sf /dev/null "/etc/systemd/system/${unit_name}"; \
    done

RUN set -eu; \
    useradd --create-home --shell /bin/bash --user-group hostadmin; \
    mkdir -p /var/run/sshd /etc/ssh/sshd_config.d /etc/systemd/system/multi-user.target.wants; \
    ssh-keygen -A; \
    if [ -f /etc/ssh/sshd_config ]; then \
        sed -i \
            -e 's/^#\?[[:space:]]*PermitRootLogin.*/PermitRootLogin no/' \
            -e 's/^#\?[[:space:]]*PasswordAuthentication.*/PasswordAuthentication no/' \
            -e 's/^#\?[[:space:]]*KbdInteractiveAuthentication.*/KbdInteractiveAuthentication no/' \
            -e 's/^#\?[[:space:]]*ChallengeResponseAuthentication.*/ChallengeResponseAuthentication no/' \
            -e 's/^#\?[[:space:]]*PubkeyAuthentication.*/PubkeyAuthentication yes/' \
            /etc/ssh/sshd_config; \
        if ! grep -Eq '^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config\.d/\*\.conf' /etc/ssh/sshd_config; then \
            printf '\nInclude /etc/ssh/sshd_config.d/*.conf\n' >> /etc/ssh/sshd_config; \
        fi; \
    elif [ ! -f /usr/etc/ssh/sshd_config ]; then \
        printf '%s\n' 'Include /etc/ssh/sshd_config.d/*.conf' > /etc/ssh/sshd_config; \
    fi; \
    printf '%s\n' \
        'PermitRootLogin no' \
        'PasswordAuthentication no' \
        'KbdInteractiveAuthentication no' \
        'PubkeyAuthentication yes' \
        'UsePAM yes' \
        > /etc/ssh/sshd_config.d/validation.conf; \
    for pam_file in /etc/pam.d/sshd /etc/pam.d/login /etc/pam.d/su; do \
        if [ -f "$pam_file" ]; then \
            sed -i \
                's/^session[[:space:]]\+required[[:space:]]\+pam_loginuid\.so/session optional pam_loginuid.so/' \
                "$pam_file"; \
        fi; \
    done; \
    ssh_unit=""; \
    for candidate in \
        /usr/lib/systemd/system/ssh.service \
        /lib/systemd/system/ssh.service \
        /usr/lib/systemd/system/sshd.service \
        /lib/systemd/system/sshd.service; do \
        if [ -f "$candidate" ]; then \
            ssh_unit="$candidate"; \
            break; \
        fi; \
    done; \
    if [ -z "$ssh_unit" ]; then \
        echo "ssh service unit is missing" >&2; \
        exit 1; \
    fi; \
    ssh_unit_name="$(basename "$ssh_unit")"; \
    ln -sf "$ssh_unit" "/etc/systemd/system/multi-user.target.wants/${ssh_unit_name}"

COPY entrypoint.sh /usr/local/sbin/validation-entrypoint
RUN chmod 755 /usr/local/sbin/validation-entrypoint

STOPSIGNAL SIGRTMIN+3
ENTRYPOINT ["/usr/local/sbin/validation-entrypoint"]
