ARG BASE_IMAGE=debian:13
FROM ${BASE_IMAGE}

ENV DEBIAN_FRONTEND=noninteractive \
    container=docker \
    LANG=C.UTF-8 \
    LC_ALL=C.UTF-8

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        systemd \
        systemd-sysv \
        dbus \
        openssh-server \
        ca-certificates \
        passwd \
        login \
        procps \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# systemd creates /run/systemd/system when it becomes pid 1.
# Do not create that directory in the image.
RUN set -eu; \
    if [ ! -s /etc/machine-id ]; then \
        tr -d '-' < /proc/sys/kernel/random/uuid > /etc/machine-id; \
    fi; \
    mkdir -p /var/lib/dbus /etc/systemd/system; \
    ln -sf /etc/machine-id /var/lib/dbus/machine-id; \
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
    sed -i \
        -e 's/^#\?[[:space:]]*PermitRootLogin.*/PermitRootLogin no/' \
        -e 's/^#\?[[:space:]]*PasswordAuthentication.*/PasswordAuthentication no/' \
        -e 's/^#\?[[:space:]]*KbdInteractiveAuthentication.*/KbdInteractiveAuthentication no/' \
        -e 's/^#\?[[:space:]]*ChallengeResponseAuthentication.*/ChallengeResponseAuthentication no/' \
        -e 's/^#\?[[:space:]]*PubkeyAuthentication.*/PubkeyAuthentication yes/' \
        /etc/ssh/sshd_config; \
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
    for candidate in /usr/lib/systemd/system/ssh.service /lib/systemd/system/ssh.service; do \
        if [ -f "$candidate" ]; then \
            ssh_unit="$candidate"; \
            break; \
        fi; \
    done; \
    if [ -z "$ssh_unit" ]; then \
        echo "ssh service unit is missing" >&2; \
        exit 1; \
    fi; \
    ln -sf "$ssh_unit" /etc/systemd/system/multi-user.target.wants/ssh.service

COPY entrypoint.sh /usr/local/sbin/validation-entrypoint
RUN chmod 755 /usr/local/sbin/validation-entrypoint

STOPSIGNAL SIGRTMIN+3
ENTRYPOINT ["/usr/local/sbin/validation-entrypoint"]
