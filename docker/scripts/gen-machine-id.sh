#!/bin/bash
dbus-uuidgen > /etc/machine-id 2>/dev/null || head -c 32 /dev/urandom | xxd -p | head -c 32 > /etc/machine-id
echo "Generated machine-id: $(cat /etc/machine-id)"
