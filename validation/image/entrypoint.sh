#!/bin/sh
set -eu

password_file=/run/validation/root-password
authorized_keys=/run/validation/authorized-keys

if [ ! -f "$password_file" ]; then
    echo "validation root password file is missing" >&2
    exit 1
fi
if [ ! -f "$authorized_keys" ]; then
    echo "validation authorized keys file is missing" >&2
    exit 1
fi

root_password="$(cat "$password_file")"
printf 'root:%s\n' "$root_password" | chpasswd
unset root_password

install -d -m 700 -o hostadmin -g hostadmin /home/hostadmin/.ssh
install -m 600 -o hostadmin -g hostadmin "$authorized_keys" /home/hostadmin/.ssh/authorized_keys

if [ ! -s /etc/machine-id ]; then
    tr -d '-' < /proc/sys/kernel/random/uuid > /etc/machine-id
fi

exec /sbin/init
